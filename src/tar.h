#ifndef DIMSIM_TAR_H
#define DIMSIM_TAR_H

#include <stddef.h>

int tar_create_from_list(const char *tar_path,
                         const char **src_paths,
                         const char **tar_paths,
                         size_t count);
int tar_extract(const char *tar_path, const char *dest_dir);

#endif
