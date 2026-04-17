#ifndef DIMSIM_COMMON_H
#define DIMSIM_COMMON_H

#include <stddef.h>
#include <sys/types.h>

#define PATHBUF 4096

int mkdir_p(const char *path, mode_t mode);
int ensure_parent_dir(const char *path, mode_t mode);
int read_file(const char *path, unsigned char **out, size_t *out_len);
int write_file(const char *path, const unsigned char *data, size_t len, mode_t mode);
int copy_file(const char *src, const char *dst, mode_t mode);
int copy_symlink(const char *src, const char *dst);
int rm_rf(const char *path);
int walk_dir(const char *root, int (*cb)(const char *abs_path, const char *rel_path, void *ctx), void *ctx);
char *json_escape(const char *s);
int sha256_hex_file(const char *path, char out_hex[65]);
void sha256_hex_bytes(const unsigned char *data, size_t len, char out_hex[65]);
int is_safe_identifier(const char *s);
int is_safe_install_path(const char *path);

#endif
