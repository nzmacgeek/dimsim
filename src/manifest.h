#ifndef DIMSIM_MANIFEST_H
#define DIMSIM_MANIFEST_H

#include <stddef.h>

typedef struct {
    char *path;
    char *hash;
    char *mode;
    char *type;
    char *target;
    long long size;
} ManifestFile;

typedef struct {
    char *name;
    char *version;
    char *arch;
    char *description;
    char *maintainer;
    char *homepage;
    char *preinst;
    char *postinst;
    char *prerm;
    char *postrm;
    ManifestFile *files;
    size_t file_count;
} Manifest;

void manifest_init(Manifest *m);
void manifest_free(Manifest *m);
int manifest_parse_basic(const char *json, Manifest *m);
int manifest_parse_full(const char *json, Manifest *m);
char *manifest_to_json(const Manifest *m);

#endif
