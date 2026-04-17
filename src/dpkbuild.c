#define _XOPEN_SOURCE 700
#include "common.h"
#include "manifest.h"
#include "tar.h"

#include <errno.h>
#include <ftw.h>
#include <libgen.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/stat.h>
#include <unistd.h>

typedef struct {
    Manifest *manifest;
    const char *payload_root;
} BuildCtx;

static char *arch_default(void) {
#if defined(__x86_64__)
    return strdup("amd64");
#elif defined(__aarch64__)
    return strdup("arm64");
#else
    return strdup("noarch");
#endif
}

static int collect_files_cb(const char *abs, const char *rel, void *ctxp) {
    BuildCtx *ctx = (BuildCtx *)ctxp;
    struct stat st;
    ManifestFile f;
    if (lstat(abs, &st) != 0) return -1;
    if (S_ISDIR(st.st_mode)) return 0;

    memset(&f, 0, sizeof(f));
    char path[PATHBUF];
    snprintf(path, sizeof(path), "/%s", rel);
    f.path = strdup(path);
    f.mode = (char *)malloc(8);
    snprintf(f.mode, 8, "%04o", (unsigned int)(st.st_mode & 07777));

    if (S_ISLNK(st.st_mode)) {
        char target[PATHBUF];
        ssize_t n = readlink(abs, target, sizeof(target) - 1);
        if (n < 0) return -1;
        target[n] = '\0';
        f.type = strdup("symlink");
        f.target = strdup(target);
        f.size = n;
        f.hash = (char *)malloc(65);
        sha256_hex_bytes((const unsigned char *)target, (size_t)n, f.hash);
    } else if (S_ISREG(st.st_mode)) {
        f.type = strdup("file");
        f.target = strdup("");
        f.size = st.st_size;
        f.hash = (char *)malloc(65);
        if (sha256_hex_file(abs, f.hash) != 0) return -1;
    } else {
        free(f.path); free(f.mode);
        return 0;
    }

    ManifestFile *n = realloc(ctx->manifest->files, (ctx->manifest->file_count + 1) * sizeof(*ctx->manifest->files));
    if (!n) return -1;
    ctx->manifest->files = n;
    ctx->manifest->files[ctx->manifest->file_count++] = f;
    return 0;
}

typedef struct {
    char **src;
    char **dst;
    size_t n;
} PathList;

static int push_path(PathList *pl, const char *src, const char *dst) {
    char **nsrc = realloc(pl->src, (pl->n + 1) * sizeof(*pl->src));
    char **ndst = realloc(pl->dst, (pl->n + 1) * sizeof(*pl->dst));
    if (!nsrc || !ndst) return -1;
    pl->src = nsrc; pl->dst = ndst;
    pl->src[pl->n] = strdup(src);
    pl->dst[pl->n] = strdup(dst);
    if (!pl->src[pl->n] || !pl->dst[pl->n]) return -1;
    pl->n++;
    return 0;
}

static int collect_tar_paths_cb(const char *abs, const char *rel, void *ctxp) {
    PathList *pl = (PathList *)ctxp;
    char tarpath[PATHBUF];
    snprintf(tarpath, sizeof(tarpath), "payload/%s", rel);
    return push_path(pl, abs, tarpath);
}

static int cmd_init(const char *name) {
    char path[PATHBUF];
    const char *scripts[] = {"preinst", "postinst", "prerm", "postrm"};
    const char *template_script = "#!/bin/bash\nset -e\n\nexit 0\n";

    if (strpbrk(name, " /\\:")) {
        fprintf(stderr, "package name must not contain spaces or special characters\n");
        return 1;
    }

    snprintf(path, sizeof(path), "%s/meta/scripts", name);
    if (mkdir_p(path, 0755) != 0) return 1;
    snprintf(path, sizeof(path), "%s/payload", name);
    if (mkdir_p(path, 0755) != 0) return 1;

    Manifest m;
    manifest_init(&m);
    m.name = strdup(name);
    m.version = strdup("0.1.0");
    m.arch = arch_default();
    m.description = (char *)malloc(strlen(name) + 20);
    sprintf(m.description, "Description of %s", name);
    m.maintainer = strdup("Your Name <your@email.com>");
    m.homepage = (char *)malloc(strlen(name) + 24);
    sprintf(m.homepage, "https://example.com/%s", name);
    m.preinst = strdup(template_script);
    m.postinst = strdup(template_script);
    m.prerm = strdup(template_script);
    m.postrm = strdup(template_script);

    char *json = manifest_to_json(&m);
    snprintf(path, sizeof(path), "%s/meta/manifest.json", name);
    if (write_file(path, (const unsigned char *)json, strlen(json), 0644) != 0) return 1;

    for (size_t i = 0; i < sizeof(scripts)/sizeof(scripts[0]); ++i) {
        snprintf(path, sizeof(path), "%s/meta/scripts/%s", name, scripts[i]);
        if (write_file(path, (const unsigned char *)template_script, strlen(template_script), 0755) != 0) return 1;
    }

    snprintf(path, sizeof(path), "%s/payload/.gitkeep", name);
    if (write_file(path, (const unsigned char *)"", 0, 0644) != 0) return 1;

    printf("✓ Scaffolded package directory: %s/\n", name);
    free(json);
    manifest_free(&m);
    return 0;
}

static int cmd_build(const char *dir_in) {
    char dir[PATHBUF], path[PATHBUF], out[PATHBUF];
    unsigned char *manifest_data = NULL;
    size_t manifest_len = 0;
    Manifest m;
    BuildCtx bctx;
    PathList pl = {0};

    if (!realpath(dir_in, dir)) {
        perror("resolve path");
        return 1;
    }

    snprintf(path, sizeof(path), "%s/meta/manifest.json", dir);
    if (read_file(path, &manifest_data, &manifest_len) != 0) {
        fprintf(stderr, "read manifest failed: %s\n", path);
        return 1;
    }

    manifest_init(&m);
    if (manifest_parse_basic((const char *)manifest_data, &m) != 0) {
        fprintf(stderr, "parse manifest failed\n");
        free(manifest_data);
        return 1;
    }

    snprintf(path, sizeof(path), "%s/payload", dir);
    struct stat st;
    if (stat(path, &st) != 0 || !S_ISDIR(st.st_mode)) {
        fprintf(stderr, "payload directory not found at %s\n", path);
        free(manifest_data);
        manifest_free(&m);
        return 1;
    }

    bctx.manifest = &m;
    bctx.payload_root = path;
    if (walk_dir(path, collect_files_cb, &bctx) != 0) {
        fprintf(stderr, "walk payload failed\n");
        free(manifest_data);
        manifest_free(&m);
        return 1;
    }

    const char *snames[] = {"preinst", "postinst", "prerm", "postrm"};
    char **slots[] = {&m.preinst, &m.postinst, &m.prerm, &m.postrm};
    for (int i = 0; i < 4; ++i) {
        unsigned char *sdata = NULL;
        size_t slen = 0;
        snprintf(path, sizeof(path), "%s/meta/scripts/%s", dir, snames[i]);
        if (read_file(path, &sdata, &slen) == 0) {
            free(*slots[i]);
            *slots[i] = (char *)sdata;
        } else {
            free(*slots[i]);
            *slots[i] = strdup("");
        }
    }

    char *json = manifest_to_json(&m);
    snprintf(path, sizeof(path), "%s/meta/manifest.json", dir);
    if (write_file(path, (const unsigned char *)json, strlen(json), 0644) != 0) {
        fprintf(stderr, "write updated manifest failed\n");
        free(json); free(manifest_data); manifest_free(&m); return 1;
    }

    snprintf(out, sizeof(out), "%s-%s-%s.dpk", m.name, m.version, m.arch);
    printf("Building %s...\n", out);

    if (push_path(&pl, path, "meta/manifest.json") != 0) return 1;
    snprintf(path, sizeof(path), "%s/payload", dir);
    if (push_path(&pl, path, "payload") != 0) return 1;
    if (walk_dir(path, collect_tar_paths_cb, &pl) != 0) {
        fprintf(stderr, "collect tar paths failed\n");
        return 1;
    }

    const char **src = (const char **)pl.src;
    const char **dst = (const char **)pl.dst;
    if (tar_create_from_list(out, src, dst, pl.n) != 0) {
        fprintf(stderr, "write dpk failed\n");
        return 1;
    }

    char hash[65];
    if (sha256_hex_file(out, hash) != 0) return 1;
    struct stat ost;
    if (stat(out, &ost) != 0) return 1;
    printf("✓ Built %s (%lld bytes)\n", out, (long long)ost.st_size);
    printf("  SHA256: %s\n", hash);

    for (size_t i = 0; i < pl.n; ++i) { free(pl.src[i]); free(pl.dst[i]); }
    free(pl.src); free(pl.dst);
    free(json); free(manifest_data); manifest_free(&m);
    return 0;
}

static void usage(void) {
    fprintf(stderr,
        "dpkbuild - Build and scaffold .dpk packages\n\n"
        "Usage:\n"
        "  dpkbuild init <name>\n"
        "  dpkbuild build [dir]\n");
}

int main(int argc, char **argv) {
    if (argc < 2) { usage(); return 1; }
    if (strcmp(argv[1], "init") == 0) {
        if (argc != 3) { usage(); return 1; }
        return cmd_init(argv[2]);
    }
    if (strcmp(argv[1], "build") == 0) {
        const char *dir = argc >= 3 ? argv[2] : ".";
        return cmd_build(dir);
    }
    usage();
    return 1;
}
