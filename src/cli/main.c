#include <errno.h>
#include <getopt.h>
#include <limits.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <stdint.h>
#include <sys/stat.h>
#include <sys/types.h>
#include <unistd.h>

#include "config.h"
#include "dimsim/version.h"

enum {
    VERBOSE_QUIET = 0,
    VERBOSE_INFO = 1,
    VERBOSE_DEBUG = 2
};

typedef struct {
    char *name;
    char *url;
    int priority;
    int enabled;
} repo_t;

typedef struct {
    repo_t *items;
    size_t len;
    size_t cap;
} repo_list_t;

typedef struct {
    int verbose;
    const char *root;
} app_opts_t;

static void usage(FILE *out) {
    fprintf(out,
            "dimsim %s\\n"
            "BlueyOS package manager (C rewrite)\\n\\n"
            "Usage:\\n"
            "  dimsim [--root PATH] [-v|-vv] <command> [args]\\n"
            "  dimsim --version\\n\\n"
            "Global options:\\n"
            "  -v, --verbose           Increase verbosity (use -vv for debug)\\n"
            "      --verbose-level N   Set verbosity level explicitly (0..2)\\n"
            "      --root PATH         Operate on a target root filesystem\\n"
            "  -V, --version           Print version/build metadata\\n"
            "  -h, --help              Show this help\\n\\n"
            "Commands:\\n"
            "  repo add <name> <url> [--priority N]\\n"
            "  repo list\\n"
            "  repo remove <name>\\n"
            "  update\\n"
            "  search <query>\\n"
            "  info <package>\\n"
            "  install <package...|file.dpk...>\\n"
            "  remove <package...>\\n"
            "  upgrade [package...]\\n"
            "  verify\\n"
            "  doctor\\n",
            DIMSIM_VERSION);
}

static void print_version(void) {
    printf("dimsim %s\\n", DIMSIM_VERSION);
    printf("build-number: %s\\n", DIMSIM_BUILD_NUMBER);
    printf("build-commit: %s\\n", DIMSIM_BUILD_COMMIT);
    printf("build-date:   %s %s UTC\\n", DIMSIM_BUILD_DATE, DIMSIM_BUILD_TIME);
    printf("build-host:   %s\\n", DIMSIM_BUILD_HOST);
    printf("build-user:   %s\\n", DIMSIM_BUILD_USER);
    printf("static:       %s\\n", DIMSIM_STATIC_BINARY ? "yes" : "no");
}

static void log_msg(const app_opts_t *opts, int level, const char *msg) {
    if (opts->verbose >= level) {
        fputs(msg, stderr);
        fputc('\n', stderr);
    }
}

static int parse_verbose_env(void) {
    const char *env = getenv("VERBOSE");
    char *end = NULL;
    long value;

    if (env == NULL || *env == '\0') {
        return VERBOSE_QUIET;
    }

    value = strtol(env, &end, 10);
    if (*end != '\0') {
        return VERBOSE_QUIET;
    }
    if (value < VERBOSE_QUIET) {
        return VERBOSE_QUIET;
    }
    if (value > VERBOSE_DEBUG) {
        return VERBOSE_DEBUG;
    }
    return (int)value;
}

static int ensure_parent_dirs(const char *file_path) {
    char path[PATH_MAX];
    char *slash;

    if (strlen(file_path) >= sizeof(path)) {
        return -1;
    }
    strcpy(path, file_path);

    slash = strrchr(path, '/');
    if (slash == NULL) {
        return 0;
    }
    *slash = '\0';

    for (char *p = path + 1; *p; p++) {
        if (*p == '/') {
            *p = '\0';
            if (mkdir(path, 0755) < 0 && errno != EEXIST) {
                return -1;
            }
            *p = '/';
        }
    }
    if (mkdir(path, 0755) < 0 && errno != EEXIST) {
        return -1;
    }
    return 0;
}

static int repos_db_path(char *out, size_t out_sz, const app_opts_t *opts) {
    const char *root = opts->root;

    if (root == NULL || *root == '\0') {
        root = "";
    }
    if (snprintf(out, out_sz, "%s/var/lib/dimsim/repos.db", root) >= (int)out_sz) {
        return -1;
    }
    return 0;
}

static void repo_list_free(repo_list_t *list) {
    for (size_t i = 0; i < list->len; i++) {
        free(list->items[i].name);
        free(list->items[i].url);
    }
    free(list->items);
    memset(list, 0, sizeof(*list));
}

static int repo_list_push(repo_list_t *list, const repo_t *repo) {
    repo_t *next;
    size_t new_cap;

    if (list->len == list->cap) {
        new_cap = list->cap == 0 ? 8 : list->cap * 2;
        next = realloc(list->items, new_cap * sizeof(*next));
        if (next == NULL) {
            return -1;
        }
        list->items = next;
        list->cap = new_cap;
    }

    list->items[list->len++] = *repo;
    return 0;
}

static int load_repos(repo_list_t *list, const app_opts_t *opts) {
    FILE *fp;
    char path[PATH_MAX];
    char line[4096];

    if (repos_db_path(path, sizeof(path), opts) < 0) {
        return -1;
    }

    fp = fopen(path, "r");
    if (fp == NULL) {
        if (errno == ENOENT) {
            return 0;
        }
        return -1;
    }

    while (fgets(line, sizeof(line), fp) != NULL) {
        char *name;
        char *url;
        char *priority;
        char *enabled;
        char *save = NULL;
        repo_t repo = {0};

        line[strcspn(line, "\r\n")] = '\0';
        if (line[0] == '\0') {
            continue;
        }

        name = strtok_r(line, "\t", &save);
        url = strtok_r(NULL, "\t", &save);
        priority = strtok_r(NULL, "\t", &save);
        enabled = strtok_r(NULL, "\t", &save);
        if (name == NULL || url == NULL || priority == NULL || enabled == NULL) {
            continue;
        }

        repo.name = strdup(name);
        repo.url = strdup(url);
        if (repo.name == NULL || repo.url == NULL) {
            free(repo.name);
            free(repo.url);
            fclose(fp);
            return -1;
        }
        repo.priority = atoi(priority);
        repo.enabled = atoi(enabled) != 0;

        if (repo_list_push(list, &repo) < 0) {
            free(repo.name);
            free(repo.url);
            fclose(fp);
            return -1;
        }
    }

    fclose(fp);
    return 0;
}

static int save_repos(const repo_list_t *list, const app_opts_t *opts) {
    FILE *fp;
    char path[PATH_MAX];

    if (repos_db_path(path, sizeof(path), opts) < 0) {
        return -1;
    }
    if (ensure_parent_dirs(path) < 0) {
        return -1;
    }

    fp = fopen(path, "w");
    if (fp == NULL) {
        return -1;
    }

    for (size_t i = 0; i < list->len; i++) {
        const repo_t *r = &list->items[i];
        fprintf(fp, "%s\t%s\t%d\t%d\n", r->name, r->url, r->priority, r->enabled ? 1 : 0);
    }

    fclose(fp);
    return 0;
}

static int cmd_repo_list(const app_opts_t *opts) {
    repo_list_t list = {0};

    if (load_repos(&list, opts) < 0) {
        perror("dimsim: failed to load repositories");
        return 1;
    }

    if (list.len == 0) {
        puts("No repositories configured. Use 'dimsim repo add <name> <url>'.");
        return 0;
    }

    printf("%-20s %-8s %-8s %s\n", "NAME", "ENABLED", "PRIORITY", "URL");
    printf("%-20s %-8s %-8s %s\n", "----", "-------", "--------", "---");
    for (size_t i = 0; i < list.len; i++) {
        const repo_t *r = &list.items[i];
        printf("%-20s %-8s %-8d %s\n", r->name, r->enabled ? "yes" : "no", r->priority, r->url);
    }

    repo_list_free(&list);
    return 0;
}

static int cmd_repo_add(const app_opts_t *opts, int argc, char **argv) {
    repo_list_t list = {0};
    repo_t repo = {0};
    int priority = 100;

    if (argc < 2) {
        fputs("dimsim: repo add requires <name> <url>\n", stderr);
        return 2;
    }

    if (load_repos(&list, opts) < 0) {
        perror("dimsim: failed to load repositories");
        return 1;
    }

    for (int i = 2; i < argc; i++) {
        if ((strcmp(argv[i], "--priority") == 0 || strcmp(argv[i], "-p") == 0) && i + 1 < argc) {
            priority = atoi(argv[++i]);
        }
    }

    for (size_t i = 0; i < list.len; i++) {
        if (strcmp(list.items[i].name, argv[0]) == 0) {
            fprintf(stderr, "dimsim: repository \"%s\" already exists\n", argv[0]);
            repo_list_free(&list);
            return 1;
        }
    }

    repo.name = strdup(argv[0]);
    repo.url = strdup(argv[1]);
    repo.priority = priority;
    repo.enabled = 1;

    if (repo.name == NULL || repo.url == NULL || repo_list_push(&list, &repo) < 0) {
        fputs("dimsim: out of memory\n", stderr);
        free(repo.name);
        free(repo.url);
        repo_list_free(&list);
        return 1;
    }

    if (save_repos(&list, opts) < 0) {
        perror("dimsim: failed to save repositories");
        repo_list_free(&list);
        return 1;
    }

    printf("Added repository '%s' -> %s (priority=%d)\n", argv[0], argv[1], priority);
    repo_list_free(&list);
    return 0;
}

static int cmd_repo_remove(const app_opts_t *opts, int argc, char **argv) {
    repo_list_t list = {0};
    size_t pos = SIZE_MAX;

    if (argc < 1) {
        fputs("dimsim: repo remove requires <name>\n", stderr);
        return 2;
    }

    if (load_repos(&list, opts) < 0) {
        perror("dimsim: failed to load repositories");
        return 1;
    }

    for (size_t i = 0; i < list.len; i++) {
        if (strcmp(list.items[i].name, argv[0]) == 0) {
            pos = i;
            break;
        }
    }

    if (pos == SIZE_MAX) {
        fprintf(stderr, "dimsim: repository '%s' not found\n", argv[0]);
        repo_list_free(&list);
        return 1;
    }

    free(list.items[pos].name);
    free(list.items[pos].url);
    for (size_t i = pos + 1; i < list.len; i++) {
        list.items[i - 1] = list.items[i];
    }
    list.len--;

    if (save_repos(&list, opts) < 0) {
        perror("dimsim: failed to save repositories");
        repo_list_free(&list);
        return 1;
    }

    printf("Removed repository '%s'\n", argv[0]);
    repo_list_free(&list);
    return 0;
}

static int cmd_repo(const app_opts_t *opts, int argc, char **argv) {
    if (argc == 0) {
        fputs("dimsim: repo requires subcommand add|list|remove\n", stderr);
        return 2;
    }

    if (strcmp(argv[0], "list") == 0) {
        return cmd_repo_list(opts);
    }
    if (strcmp(argv[0], "add") == 0) {
        return cmd_repo_add(opts, argc - 1, argv + 1);
    }
    if (strcmp(argv[0], "remove") == 0 || strcmp(argv[0], "rm") == 0 || strcmp(argv[0], "del") == 0) {
        return cmd_repo_remove(opts, argc - 1, argv + 1);
    }

    fprintf(stderr, "dimsim: unknown repo subcommand '%s'\n", argv[0]);
    return 2;
}

static int cmd_stub(const char *name) {
    fprintf(stderr,
            "dimsim: command '%s' is not implemented yet in the C rewrite.\\n"
            "Use the Go branch/release if you need full package operations today.\\n",
            name);
    return 2;
}

int main(int argc, char **argv) {
    app_opts_t opts;
    int c;
    int option_index = 0;

    static const struct option long_options[] = {
        {"help", no_argument, 0, 'h'},
        {"version", no_argument, 0, 'V'},
        {"verbose", no_argument, 0, 'v'},
        {"verbose-level", required_argument, 0, 1000},
        {"root", required_argument, 0, 1001},
        {0, 0, 0, 0},
    };

    opts.verbose = parse_verbose_env();
    opts.root = "";

    while ((c = getopt_long(argc, argv, "hVv", long_options, &option_index)) != -1) {
        switch (c) {
        case 'h':
            usage(stdout);
            return 0;
        case 'V':
            print_version();
            return 0;
        case 'v':
            if (opts.verbose < VERBOSE_DEBUG) {
                opts.verbose++;
            }
            break;
        case 1000:
            opts.verbose = atoi(optarg);
            if (opts.verbose < VERBOSE_QUIET) {
                opts.verbose = VERBOSE_QUIET;
            }
            if (opts.verbose > VERBOSE_DEBUG) {
                opts.verbose = VERBOSE_DEBUG;
            }
            break;
        case 1001:
            opts.root = optarg;
            break;
        default:
            usage(stderr);
            return 2;
        }
    }

    log_msg(&opts, VERBOSE_DEBUG, "[debug] dimsim starting");

    if (optind >= argc) {
        usage(stderr);
        return 2;
    }

    if (strcmp(argv[optind], "repo") == 0) {
        return cmd_repo(&opts, argc - optind - 1, argv + optind + 1);
    }
    if (strcmp(argv[optind], "update") == 0 ||
        strcmp(argv[optind], "search") == 0 ||
        strcmp(argv[optind], "info") == 0 ||
        strcmp(argv[optind], "install") == 0 ||
        strcmp(argv[optind], "remove") == 0 ||
        strcmp(argv[optind], "upgrade") == 0 ||
        strcmp(argv[optind], "verify") == 0 ||
        strcmp(argv[optind], "pin") == 0 ||
        strcmp(argv[optind], "unpin") == 0 ||
        strcmp(argv[optind], "doctor") == 0 ||
        strcmp(argv[optind], "autoremove") == 0) {
        return cmd_stub(argv[optind]);
    }

    fprintf(stderr, "dimsim: unknown command '%s'\\n", argv[optind]);
    usage(stderr);
    return 2;
}
