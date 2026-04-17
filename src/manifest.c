#include "manifest.h"
#include "common.h"

#include <ctype.h>
#include <stdarg.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

void manifest_init(Manifest *m) { memset(m, 0, sizeof(*m)); }
static char *xstrdup(const char *s) {
    size_t n = strlen(s) + 1;
    char *p = malloc(n);
    if (!p) return NULL;
    memcpy(p, s, n);
    return p;
}

static void free_str(char **p) { if (*p) free(*p); *p = NULL; }

void manifest_free(Manifest *m) {
    free_str(&m->name); free_str(&m->version); free_str(&m->arch); free_str(&m->description);
    free_str(&m->maintainer); free_str(&m->homepage); free_str(&m->preinst); free_str(&m->postinst);
    free_str(&m->prerm); free_str(&m->postrm);
    for (size_t i = 0; i < m->file_count; ++i) {
        free(m->files[i].path); free(m->files[i].hash); free(m->files[i].mode);
        free(m->files[i].type); free(m->files[i].target);
    }
    free(m->files);
    memset(m, 0, sizeof(*m));
}

static const char *skip_ws(const char *p) {
    while (*p && isspace((unsigned char)*p)) p++;
    return p;
}

static char *dup_json_string(const char *start) {
    const char *p = start;
    char *out, *q;
    size_t cap = strlen(start) + 1;
    if (*p != '"') return NULL;
    p++;
    out = (char *)malloc(cap);
    if (!out) return NULL;
    q = out;
    while (*p && *p != '"') {
        if (*p == '\\') {
            p++;
            if (*p == 'n') *q++ = '\n';
            else if (*p == 'r') *q++ = '\r';
            else if (*p == 't') *q++ = '\t';
            else if (*p == '"' || *p == '\\') *q++ = *p;
            else *q++ = *p;
            if (*p) p++;
        } else {
            *q++ = *p++;
        }
    }
    *q = '\0';
    if (*p != '"') { free(out); return NULL; }
    return out;
}

static int json_get_string(const char *json, const char *key, char **out) {
    char needle[128];
    const char *p, *q;
    snprintf(needle, sizeof(needle), "\"%s\"", key);
    p = strstr(json, needle);
    if (!p) return -1;
    p += strlen(needle);
    p = skip_ws(p);
    if (*p != ':') return -1;
    p = skip_ws(p + 1);
    if (*p != '"') return -1;
    q = p;
    *out = dup_json_string(q);
    return *out ? 0 : -1;
}

static int json_get_files(const char *json, Manifest *m) {
    const char *p = strstr(json, "\"files\"");
    if (!p) return 0;
    p = strchr(p, '[');
    if (!p) return -1;
    p++;
    while (*p) {
        p = skip_ws(p);
        if (*p == ']') break;
        if (*p != '{') return -1;
        const char *obj_end = strchr(p, '}');
        if (!obj_end) return -1;
        size_t len = (size_t)(obj_end - p + 1);
        char *obj = (char *)malloc(len + 1);
        ManifestFile f;
        if (!obj) return -1;
        memset(&f, 0, sizeof(f));
        memcpy(obj, p, len);
        obj[len] = '\0';

        if (json_get_string(obj, "path", &f.path) != 0) { free(obj); return -1; }
        if (json_get_string(obj, "hash", &f.hash) != 0) { free(obj); free(f.path); return -1; }
        if (json_get_string(obj, "mode", &f.mode) != 0) f.mode = xstrdup("0644");
        if (json_get_string(obj, "type", &f.type) != 0) f.type = xstrdup("file");
        if (json_get_string(obj, "target", &f.target) != 0) f.target = xstrdup("");

        const char *sz = strstr(obj, "\"size\"");
        f.size = 0;
        if (sz) {
            sz = strchr(sz, ':');
            if (sz) f.size = atoll(sz + 1);
        }

        ManifestFile *n = realloc(m->files, (m->file_count + 1) * sizeof(*m->files));
        if (!n) { free(obj); free(f.path); free(f.hash); free(f.mode); free(f.type); free(f.target); return -1; }
        m->files = n;
        m->files[m->file_count++] = f;
        free(obj);
        p = obj_end + 1;
        const char *comma = strchr(p, ',');
        const char *close = strchr(p, ']');
        if (close && (!comma || close < comma)) break;
        if (comma) p = comma + 1;
    }
    return 0;
}

int manifest_parse_basic(const char *json, Manifest *m) {
    if (json_get_string(json, "name", &m->name) != 0) return -1;
    if (json_get_string(json, "version", &m->version) != 0) return -1;
    if (json_get_string(json, "arch", &m->arch) != 0) m->arch = xstrdup("amd64");
    if (json_get_string(json, "description", &m->description) != 0) m->description = xstrdup("");
    if (json_get_string(json, "maintainer", &m->maintainer) != 0) m->maintainer = xstrdup("");
    if (json_get_string(json, "homepage", &m->homepage) != 0) m->homepage = xstrdup("");

    if (!is_safe_identifier(m->name)) {
        fprintf(stderr, "invalid package name\n");
        return -1;
    }
    if (!is_safe_identifier(m->version)) {
        fprintf(stderr, "invalid package version\n");
        return -1;
    }
    if (!is_safe_identifier(m->arch)) {
        fprintf(stderr, "invalid package arch\n");
        return -1;
    }

    return 0;
}

int manifest_parse_full(const char *json, Manifest *m) {
    if (manifest_parse_basic(json, m) != 0) return -1;
    if (json_get_string(json, "preinst", &m->preinst) != 0) m->preinst = xstrdup("");
    if (json_get_string(json, "postinst", &m->postinst) != 0) m->postinst = xstrdup("");
    if (json_get_string(json, "prerm", &m->prerm) != 0) m->prerm = xstrdup("");
    if (json_get_string(json, "postrm", &m->postrm) != 0) m->postrm = xstrdup("");
    return json_get_files(json, m);
}

static int appendf(char **buf, size_t *cap, size_t *off, const char *fmt, ...) {
    while (1) {
        va_list ap;
        va_start(ap, fmt);
        int n = vsnprintf(*buf + *off, *cap - *off, fmt, ap);
        va_end(ap);
        if (n < 0) return -1;
        if ((size_t)n < *cap - *off) {
            *off += (size_t)n;
            return 0;
        }
        size_t new_cap = (*cap) * 2 + (size_t)n + 64;
        char *new_buf = realloc(*buf, new_cap);
        if (!new_buf) return -1;
        *buf = new_buf;
        *cap = new_cap;
    }
}

char *manifest_to_json(const Manifest *m) {
    size_t cap = 2048;
    char *buf = (char *)malloc(cap);
    size_t off = 0;
    if (!buf) return NULL;

    char *name = json_escape(m->name ? m->name : "");
    char *version = json_escape(m->version ? m->version : "");
    char *arch = json_escape(m->arch ? m->arch : "amd64");
    char *desc = json_escape(m->description ? m->description : "");
    char *maint = json_escape(m->maintainer ? m->maintainer : "");
    char *home = json_escape(m->homepage ? m->homepage : "");
    char *preinst = json_escape(m->preinst ? m->preinst : "");
    char *postinst = json_escape(m->postinst ? m->postinst : "");
    char *prerm = json_escape(m->prerm ? m->prerm : "");
    char *postrm = json_escape(m->postrm ? m->postrm : "");

    if (!name || !version || !arch || !desc || !maint || !home || !preinst || !postinst || !prerm || !postrm) {
        free(buf);
        free(name); free(version); free(arch); free(desc); free(maint); free(home);
        free(preinst); free(postinst); free(prerm); free(postrm);
        return NULL;
    }

    if (appendf(&buf, &cap, &off,
        "{\n"
        "  \"name\": \"%s\",\n"
        "  \"version\": \"%s\",\n"
        "  \"arch\": \"%s\",\n"
        "  \"description\": \"%s\",\n"
        "  \"depends\": [],\n"
        "  \"recommends\": [],\n"
        "  \"conflicts\": [],\n"
        "  \"provides\": [],\n"
        "  \"maintainer\": \"%s\",\n"
        "  \"homepage\": \"%s\",\n"
        "  \"files\": [\n",
        name, version, arch, desc, maint, home) != 0) {
        free(buf); buf = NULL; goto done;
    }

    for (size_t i = 0; i < m->file_count; ++i) {
        ManifestFile *f = &m->files[i];
        char *path = json_escape(f->path ? f->path : "");
        char *hash = json_escape(f->hash ? f->hash : "");
        char *mode = json_escape(f->mode ? f->mode : "0644");
        char *type = json_escape(f->type ? f->type : "file");
        char *target = json_escape(f->target ? f->target : "");
        if (!path || !hash || !mode || !type || !target) {
            free(path); free(hash); free(mode); free(type); free(target);
            free(buf);
            buf = NULL;
            goto done;
        }
        if (f->target && *f->target) {
            if (appendf(&buf, &cap, &off,
                "    {\"path\": \"%s\", \"hash\": \"%s\", \"size\": %lld, \"mode\": \"%s\", \"type\": \"%s\", \"target\": \"%s\"}%s\n",
                path, hash, f->size, mode, type, target, (i + 1 < m->file_count) ? "," : "") != 0) {
                free(path); free(hash); free(mode); free(type); free(target);
                free(buf); buf = NULL; goto done;
            }
        } else {
            if (appendf(&buf, &cap, &off,
                "    {\"path\": \"%s\", \"hash\": \"%s\", \"size\": %lld, \"mode\": \"%s\", \"type\": \"%s\"}%s\n",
                path, hash, f->size, mode, type, (i + 1 < m->file_count) ? "," : "") != 0) {
                free(path); free(hash); free(mode); free(type); free(target);
                free(buf); buf = NULL; goto done;
            }
        }
        free(path); free(hash); free(mode); free(type); free(target);
    }

    if (appendf(&buf, &cap, &off,
        "  ],\n"
        "  \"scripts\": {\n"
        "    \"preinst\": \"%s\",\n"
        "    \"postinst\": \"%s\",\n"
        "    \"prerm\": \"%s\",\n"
        "    \"postrm\": \"%s\"\n"
        "  }\n"
        "}\n",
        preinst, postinst, prerm, postrm) != 0) {
        free(buf); buf = NULL;
    }

done:
    free(name); free(version); free(arch); free(desc); free(maint); free(home);
    free(preinst); free(postinst); free(prerm); free(postrm);
    return buf;
}
