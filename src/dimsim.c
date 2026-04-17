#define _XOPEN_SOURCE 700
#include "common.h"
#include "manifest.h"
#include "tar.h"

#include <errno.h>
#include <fcntl.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/stat.h>
#include <unistd.h>

static const char *default_state_dir = "/var/lib/dimsim";

typedef struct {
    const char *root;
    char state_dir[PATHBUF];
    char pkg_dir[PATHBUF];
    char script_dir[PATHBUF];
    char staging_dir[PATHBUF];
} Ctx;

static int init_ctx(Ctx *ctx, const char *root) {
    memset(ctx, 0, sizeof(*ctx));
    ctx->root = root ? root : "";
    if (ctx->root[0]) snprintf(ctx->state_dir, sizeof(ctx->state_dir), "%s%s", ctx->root, default_state_dir);
    else snprintf(ctx->state_dir, sizeof(ctx->state_dir), "%s", default_state_dir);
    snprintf(ctx->pkg_dir, sizeof(ctx->pkg_dir), "%s/packages", ctx->state_dir);
    snprintf(ctx->script_dir, sizeof(ctx->script_dir), "%s/scripts", ctx->state_dir);
    snprintf(ctx->staging_dir, sizeof(ctx->staging_dir), "%s/staging", ctx->state_dir);
    if (mkdir_p(ctx->pkg_dir, 0755) != 0) return -1;
    if (mkdir_p(ctx->script_dir, 0755) != 0) return -1;
    if (mkdir_p(ctx->staging_dir, 0755) != 0) return -1;
    return 0;
}

static void full_target_path(const Ctx *ctx, const char *pkg_path, char out[PATHBUF]) {
    if (ctx->root[0]) snprintf(out, PATHBUF, "%s%s", ctx->root, pkg_path);
    else snprintf(out, PATHBUF, "%s", pkg_path);
}

static int write_script(const char *dir, const char *name, const char *content) {
    char path[PATHBUF];
    if (!content || !*content) return 0;
    snprintf(path, sizeof(path), "%s/%s", dir, name);
    return write_file(path, (const unsigned char *)content, strlen(content), 0755);
}

static int run_script(const char *content, const char *arg, const Ctx *ctx) {
    if (!content || !*content) return 0;
    char script[PATHBUF];
    snprintf(script, sizeof(script), "%s/run-script-%d.sh", ctx->staging_dir, getpid());
    if (write_file(script, (const unsigned char *)content, strlen(content), 0755) != 0) return -1;
    setenv("DIMSIM_ROOT", ctx->root, 1);
    char cmd[PATHBUF * 2];
    if (arg && *arg) snprintf(cmd, sizeof(cmd), "/bin/bash '%s' '%s'", script, arg);
    else snprintf(cmd, sizeof(cmd), "/bin/bash '%s'", script);
    int rc = system(cmd);
    unlink(script);
    return rc == 0 ? 0 : -1;
}

static int save_pkg_state(const Ctx *ctx, const Manifest *m) {
    char path[PATHBUF], spath[PATHBUF];
    snprintf(path, sizeof(path), "%s/%s.state", ctx->pkg_dir, m->name);
    FILE *f = fopen(path, "wb");
    if (!f) return -1;
    fprintf(f, "name=%s\nversion=%s\narch=%s\ndescription=%s\nfiles=%zu\n",
        m->name, m->version, m->arch, m->description ? m->description : "", m->file_count);
    for (size_t i = 0; i < m->file_count; ++i) {
        ManifestFile *x = &m->files[i];
        fprintf(f, "file\t%s\t%s\t%lld\t%s\t%s\t%s\n",
            x->path ? x->path : "",
            x->hash ? x->hash : "",
            x->size,
            x->mode ? x->mode : "0644",
            x->type ? x->type : "file",
            x->target ? x->target : "");
    }
    fclose(f);

    snprintf(spath, sizeof(spath), "%s/%s", ctx->script_dir, m->name);
    if (mkdir_p(spath, 0755) != 0) return -1;
    if (write_script(spath, "prerm", m->prerm) != 0) return -1;
    if (write_script(spath, "postrm", m->postrm) != 0) return -1;
    return 0;
}

static int load_pkg_state(const Ctx *ctx, const char *pkg, Manifest *m) {
    char path[PATHBUF];
    snprintf(path, sizeof(path), "%s/%s.state", ctx->pkg_dir, pkg);
    unsigned char *data;
    size_t len;
    if (read_file(path, &data, &len) != 0) return -1;

    char *save, *line = strtok_r((char *)data, "\n", &save);
    manifest_init(m);
    while (line) {
        if (strncmp(line, "name=", 5) == 0) m->name = strdup(line + 5);
        else if (strncmp(line, "version=", 8) == 0) m->version = strdup(line + 8);
        else if (strncmp(line, "arch=", 5) == 0) m->arch = strdup(line + 5);
        else if (strncmp(line, "description=", 12) == 0) m->description = strdup(line + 12);
        else if (strncmp(line, "file\t", 5) == 0) {
            ManifestFile f = {0};
            char *toksave, *p = line + 5;
            f.path = strdup(strtok_r(p, "\t", &toksave));
            f.hash = strdup(strtok_r(NULL, "\t", &toksave));
            f.size = atoll(strtok_r(NULL, "\t", &toksave));
            f.mode = strdup(strtok_r(NULL, "\t", &toksave));
            f.type = strdup(strtok_r(NULL, "\t", &toksave));
            char *target = strtok_r(NULL, "\t", &toksave);
            f.target = strdup(target ? target : "");
            ManifestFile *n = realloc(m->files, (m->file_count + 1) * sizeof(*m->files));
            if (!n) { free(data); return -1; }
            m->files = n;
            m->files[m->file_count++] = f;
        }
        line = strtok_r(NULL, "\n", &save);
    }
    free(data);
    return 0;
}

static int install_single(const Ctx *ctx, const char *dpk_path) {
    char stage[PATHBUF], manifest_path[PATHBUF], payload_root[PATHBUF];
    unsigned char *manifest_data = NULL;
    size_t manifest_len = 0;
    Manifest m;

    snprintf(stage, sizeof(stage), "%s/install-%d", ctx->staging_dir, getpid());
    rm_rf(stage);
    if (mkdir_p(stage, 0755) != 0) return -1;
    if (tar_extract(dpk_path, stage) != 0) {
        fprintf(stderr, "failed to extract %s\n", dpk_path);
        rm_rf(stage);
        return -1;
    }

    snprintf(manifest_path, sizeof(manifest_path), "%s/meta/manifest.json", stage);
    if (read_file(manifest_path, &manifest_data, &manifest_len) != 0) {
        fprintf(stderr, "manifest missing in package\n");
        rm_rf(stage);
        return -1;
    }

    manifest_init(&m);
    if (manifest_parse_full((const char *)manifest_data, &m) != 0) {
        fprintf(stderr, "manifest parse failed\n");
        free(manifest_data); rm_rf(stage); return -1;
    }

    snprintf(payload_root, sizeof(payload_root), "%s/payload", stage);
    if (run_script(m.preinst, NULL, ctx) != 0) {
        fprintf(stderr, "preinst failed for %s\n", m.name);
        manifest_free(&m); free(manifest_data); rm_rf(stage); return -1;
    }

    for (size_t i = 0; i < m.file_count; ++i) {
        ManifestFile *f = &m.files[i];
        char src[PATHBUF], dst[PATHBUF], got[65];
        snprintf(src, sizeof(src), "%s%s", payload_root, f->path);
        full_target_path(ctx, f->path, dst);
        if (strcmp(f->type ? f->type : "file", "symlink") == 0) {
            if (ensure_parent_dir(dst, 0755) != 0) goto fail;
            unlink(dst);
            if (symlink(f->target, dst) != 0) goto fail;
            sha256_hex_bytes((const unsigned char *)f->target, strlen(f->target), got);
        } else {
            mode_t mode = (mode_t)strtol(f->mode ? f->mode : "0644", NULL, 8);
            if (copy_file(src, dst, mode) != 0) goto fail;
            if (sha256_hex_file(dst, got) != 0) goto fail;
        }
        if (f->hash && *f->hash && strcmp(got, f->hash) != 0) {
            fprintf(stderr, "hash mismatch for %s\n", f->path);
            goto fail;
        }
    }

    if (run_script(m.postinst, NULL, ctx) != 0) {
        fprintf(stderr, "postinst failed for %s\n", m.name);
        goto fail;
    }

    if (save_pkg_state(ctx, &m) != 0) goto fail;
    printf("✓ Installed %s %s\n", m.name, m.version);

    manifest_free(&m);
    free(manifest_data);
    rm_rf(stage);
    return 0;

fail:
    fprintf(stderr, "install failed for %s\n", m.name ? m.name : dpk_path);
    manifest_free(&m);
    free(manifest_data);
    rm_rf(stage);
    return -1;
}

static int cmd_install(const Ctx *ctx, int argc, char **argv) {
    for (int i = 0; i < argc; ++i) {
        if (install_single(ctx, argv[i]) != 0) return 1;
    }
    return 0;
}

static int cmd_remove(const Ctx *ctx, const char *pkg, int purge) {
    Manifest m;
    char spath[PATHBUF], script[PATHBUF], fpath[PATHBUF];
    if (load_pkg_state(ctx, pkg, &m) != 0) {
        fprintf(stderr, "package not installed: %s\n", pkg);
        return 1;
    }

    snprintf(spath, sizeof(spath), "%s/%s", ctx->script_dir, pkg);
    snprintf(script, sizeof(script), "%s/prerm", spath);
    unsigned char *data = NULL; size_t len = 0;
    if (read_file(script, &data, &len) == 0) {
        run_script((char *)data, purge ? "purge" : "", ctx);
        free(data);
    }

    for (size_t i = 0; i < m.file_count; ++i) {
        full_target_path(ctx, m.files[i].path, fpath);
        unlink(fpath);
    }

    snprintf(script, sizeof(script), "%s/postrm", spath);
    data = NULL; len = 0;
    if (read_file(script, &data, &len) == 0) {
        run_script((char *)data, purge ? "purge" : "", ctx);
        free(data);
    }

    snprintf(fpath, sizeof(fpath), "%s/%s.state", ctx->pkg_dir, pkg);
    unlink(fpath);
    rm_rf(spath);

    printf("✓ Removed %s %s\n", m.name, m.version);
    manifest_free(&m);
    return 0;
}

static int cmd_info(const Ctx *ctx, const char *pkg) {
    Manifest m;
    if (load_pkg_state(ctx, pkg, &m) != 0) {
        fprintf(stderr, "package not installed: %s\n", pkg);
        return 1;
    }
    printf("Name:        %s\n", m.name);
    printf("Version:     %s\n", m.version);
    printf("Arch:        %s\n", m.arch);
    printf("Description: %s\n", m.description ? m.description : "");
    printf("Files:       %zu file(s)\n", m.file_count);
    manifest_free(&m);
    return 0;
}

static int cmd_verify(const Ctx *ctx) {
    char cmd[PATHBUF + 64];
    snprintf(cmd, sizeof(cmd), "find '%s' -maxdepth 1 -name '*.state' -type f", ctx->pkg_dir);
    FILE *fp = popen(cmd, "r");
    if (!fp) return 1;
    int issues = 0;
    char line[PATHBUF];
    while (fgets(line, sizeof(line), fp)) {
        line[strcspn(line, "\n")] = '\0';
        char *base = strrchr(line, '/');
        if (!base) continue;
        base++;
        char *dot = strstr(base, ".state");
        if (!dot) continue;
        *dot = '\0';

        Manifest m;
        if (load_pkg_state(ctx, base, &m) != 0) continue;
        for (size_t i = 0; i < m.file_count; ++i) {
            ManifestFile *f = &m.files[i];
            char path[PATHBUF], got[65];
            full_target_path(ctx, f->path, path);
            if (strcmp(f->type, "symlink") == 0) {
                char target[PATHBUF];
                ssize_t n = readlink(path, target, sizeof(target) - 1);
                if (n < 0) {
                    printf("  MISSING %s (from %s)\n", f->path, m.name);
                    issues++;
                    continue;
                }
                target[n] = '\0';
                sha256_hex_bytes((const unsigned char *)target, (size_t)n, got);
            } else {
                if (sha256_hex_file(path, got) != 0) {
                    printf("  MISSING %s (from %s)\n", f->path, m.name);
                    issues++;
                    continue;
                }
            }
            if (strcmp(got, f->hash) != 0) {
                printf("  MODIFIED %s (from %s)\n", f->path, m.name);
                issues++;
            }
        }
        manifest_free(&m);
    }
    pclose(fp);
    if (issues == 0) {
        printf("✓ All installed files verified successfully.\n");
        return 0;
    }
    return 1;
}

static int cmd_list(const Ctx *ctx) {
    char cmd[PATHBUF + 64];
    snprintf(cmd, sizeof(cmd), "find '%s' -maxdepth 1 -name '*.state' -type f", ctx->pkg_dir);
    FILE *fp = popen(cmd, "r");
    if (!fp) return 1;
    char line[PATHBUF];
    while (fgets(line, sizeof(line), fp)) {
        line[strcspn(line, "\n")] = '\0';
        char *base = strrchr(line, '/');
        if (!base) continue;
        base++;
        char *dot = strstr(base, ".state");
        if (dot) *dot = '\0';
        Manifest m;
        if (load_pkg_state(ctx, base, &m) == 0) {
            printf("%s\t%s\n", m.name, m.version);
            manifest_free(&m);
        }
    }
    pclose(fp);
    return 0;
}

static void usage(void) {
    fprintf(stderr,
        "dimsim - BlueyOS package manager (C edition)\n\n"
        "Usage:\n"
        "  dimsim [--root DIR] install <package.dpk...>\n"
        "  dimsim [--root DIR] remove [--purge] <package...>\n"
        "  dimsim [--root DIR] info <package>\n"
        "  dimsim [--root DIR] verify\n"
        "  dimsim [--root DIR] list\n");
}

int main(int argc, char **argv) {
    const char *root = "";
    int i = 1;
    if (argc < 2) { usage(); return 1; }
    if (strcmp(argv[i], "--root") == 0) {
        if (i + 1 >= argc) { usage(); return 1; }
        root = argv[i + 1];
        i += 2;
    }
    if (i >= argc) { usage(); return 1; }

    Ctx ctx;
    if (init_ctx(&ctx, root) != 0) {
        fprintf(stderr, "failed to initialize state at %s\n", ctx.state_dir);
        return 1;
    }

    if (strcmp(argv[i], "install") == 0) {
        if (i + 1 >= argc) { usage(); return 1; }
        return cmd_install(&ctx, argc - i - 1, &argv[i + 1]);
    }
    if (strcmp(argv[i], "remove") == 0) {
        int purge = 0;
        int start = i + 1;
        if (start < argc && strcmp(argv[start], "--purge") == 0) { purge = 1; start++; }
        if (start >= argc) { usage(); return 1; }
        for (int j = start; j < argc; ++j) {
            if (cmd_remove(&ctx, argv[j], purge) != 0) return 1;
        }
        return 0;
    }
    if (strcmp(argv[i], "info") == 0) {
        if (i + 1 >= argc) { usage(); return 1; }
        return cmd_info(&ctx, argv[i + 1]);
    }
    if (strcmp(argv[i], "verify") == 0) {
        return cmd_verify(&ctx);
    }
    if (strcmp(argv[i], "list") == 0) {
        return cmd_list(&ctx);
    }

    usage();
    return 1;
}
