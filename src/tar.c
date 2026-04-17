#define _XOPEN_SOURCE 700
#include "tar.h"
#include "common.h"

#include <errno.h>
#include <fcntl.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/stat.h>
#include <unistd.h>

typedef struct {
    char name[100];
    char mode[8];
    char uid[8];
    char gid[8];
    char size[12];
    char mtime[12];
    char chksum[8];
    char typeflag;
    char linkname[100];
    char magic[6];
    char version[2];
    char uname[32];
    char gname[32];
    char devmajor[8];
    char devminor[8];
    char prefix[155];
    char pad[12];
} tar_hdr;

static void octal(char *dst, size_t n, unsigned long long v) {
    snprintf(dst, n, "%0*llo", (int)(n - 1), v);
}

static int write_padding(int fd, size_t len) {
    static const char zeros[512] = {0};
    size_t rem = len % 512;
    if (!rem) return 0;
    rem = 512 - rem;
    return write(fd, zeros, rem) == (ssize_t)rem ? 0 : -1;
}

static int split_name(const char *path, char name[100], char prefix[155]) {
    size_t len = strlen(path);
    memset(name, 0, 100);
    memset(prefix, 0, 155);
    if (len < 100) {
        memcpy(name, path, len);
        return 0;
    }
    const char *slash = strrchr(path, '/');
    if (!slash) {
        fprintf(stderr, "tar path too long for ustar header: %s\n", path);
        return -1;
    }
    size_t plen = (size_t)(slash - path);
    size_t nlen = strlen(slash + 1);
    if (plen >= 155 || nlen >= 100) {
        fprintf(stderr, "tar path too long for ustar header: %s\n", path);
        return -1;
    }
    memcpy(prefix, path, plen);
    memcpy(name, slash + 1, nlen);
    return 0;
}

static int write_all(int fd, const void *buf, size_t count) {
    const char *p = (const char *)buf;
    size_t written = 0;
    while (written < count) {
        ssize_t n = write(fd, p + written, count - written);
        if (n < 0) {
            if (errno == EINTR) continue;
            return -1;
        }
        if (n == 0) return -1;
        written += (size_t)n;
    }
    return 0;
}

static int write_header(int fd, const char *tar_path, const struct stat *st, char typeflag, const char *linkname) {
    tar_hdr h;
    unsigned int sum = 0;
    unsigned char *p;
    memset(&h, 0, sizeof(h));
    if (split_name(tar_path, h.name, h.prefix) != 0) return -1;
    octal(h.mode, sizeof(h.mode), (unsigned long long)(st->st_mode & 07777));
    octal(h.uid, sizeof(h.uid), 0);
    octal(h.gid, sizeof(h.gid), 0);
    octal(h.size, sizeof(h.size), typeflag == '0' ? (unsigned long long)st->st_size : 0);
    octal(h.mtime, sizeof(h.mtime), (unsigned long long)st->st_mtime);
    memset(h.chksum, ' ', sizeof(h.chksum));
    h.typeflag = typeflag;
    if (linkname) strncpy(h.linkname, linkname, sizeof(h.linkname) - 1);
    memcpy(h.magic, "ustar", 5);
    memcpy(h.version, "00", 2);

    p = (unsigned char *)&h;
    for (size_t i = 0; i < sizeof(h); ++i) sum += p[i];
    snprintf(h.chksum, sizeof(h.chksum), "%06o", sum);
    h.chksum[6] = '\0';
    h.chksum[7] = ' ';

    return write_all(fd, &h, sizeof(h));
}

int tar_create_from_list(const char *tar_path, const char **src_paths, const char **tar_paths, size_t count) {
    int fd = open(tar_path, O_WRONLY | O_CREAT | O_TRUNC, 0644);
    if (fd < 0) return -1;
    for (size_t i = 0; i < count; ++i) {
        struct stat st;
        if (lstat(src_paths[i], &st) != 0) { close(fd); return -1; }
        if (S_ISDIR(st.st_mode)) {
            char dirpath[PATHBUF];
            snprintf(dirpath, sizeof(dirpath), "%s/", tar_paths[i]);
            if (write_header(fd, dirpath, &st, '5', NULL) != 0) { close(fd); return -1; }
        } else if (S_ISLNK(st.st_mode)) {
            char target[PATHBUF];
            ssize_t n = readlink(src_paths[i], target, sizeof(target) - 1);
            if (n < 0) { close(fd); return -1; }
            target[n] = '\0';
            if (write_header(fd, tar_paths[i], &st, '2', target) != 0) { close(fd); return -1; }
        } else if (S_ISREG(st.st_mode)) {
            int in;
            char buf[65536];
            ssize_t r;
            if (write_header(fd, tar_paths[i], &st, '0', NULL) != 0) { close(fd); return -1; }
            in = open(src_paths[i], O_RDONLY);
            if (in < 0) { close(fd); return -1; }
            while ((r = read(in, buf, sizeof(buf))) > 0) {
                if (write_all(fd, buf, (size_t)r) != 0) { close(in); close(fd); return -1; }
            }
            close(in);
            if (r < 0 || write_padding(fd, (size_t)st.st_size) != 0) { close(fd); return -1; }
        }
    }
    static const char zero[1024] = {0};
    if (write_all(fd, zero, sizeof(zero)) != 0) { close(fd); return -1; }
    return close(fd);
}

static unsigned long parse_octal(const char *s, size_t n) {
    unsigned long v = 0;
    for (size_t i = 0; i < n && s[i]; ++i) {
        if (s[i] < '0' || s[i] > '7') continue;
        v = (v << 3) + (unsigned long)(s[i] - '0');
    }
    return v;
}

static int is_safe_path(const char *path) {
    if (!path || !*path) return 0;
    if (path[0] == '/') return 0;
    const char *p = path;
    while (*p) {
        if (p[0] == '.' && p[1] == '.' && (p[2] == '/' || p[2] == '\0')) return 0;
        if (p[0] == '.' && p[1] == '.' && p == path) return 0;
        p++;
    }
    return 1;
}

int tar_extract(const char *tar_path, const char *dest_dir) {
    int fd = open(tar_path, O_RDONLY);
    if (fd < 0) return -1;
    if (mkdir_p(dest_dir, 0755) != 0) { close(fd); return -1; }

    for (;;) {
        tar_hdr h;
        ssize_t n = read(fd, &h, sizeof(h));
        if (n == 0) break;
        if (n != (ssize_t)sizeof(h)) { close(fd); return -1; }
        if (h.name[0] == '\0') {
            char next[512];
            if (read(fd, next, sizeof(next)) != (ssize_t)sizeof(next)) { close(fd); return -1; }
            break;
        }

        h.name[sizeof(h.name) - 1] = '\0';
        h.prefix[sizeof(h.prefix) - 1] = '\0';
        h.linkname[sizeof(h.linkname) - 1] = '\0';

        if (!is_safe_path(h.name)) {
            fprintf(stderr, "tar: unsafe path in name field\n");
            close(fd);
            return -1;
        }
        if (h.prefix[0] && !is_safe_path(h.prefix)) {
            fprintf(stderr, "tar: unsafe path in prefix field\n");
            close(fd);
            return -1;
        }

        char path[PATHBUF];
        if (h.prefix[0]) snprintf(path, sizeof(path), "%s/%s/%s", dest_dir, h.prefix, h.name);
        else snprintf(path, sizeof(path), "%s/%s", dest_dir, h.name);
        unsigned long mode = parse_octal(h.mode, sizeof(h.mode));
        unsigned long size = parse_octal(h.size, sizeof(h.size));

        if (h.typeflag == '5') {
            if (mkdir_p(path, (mode_t)(mode ? mode : 0755)) != 0) { close(fd); return -1; }
        } else if (h.typeflag == '2') {
            if (ensure_parent_dir(path, 0755) != 0) { close(fd); return -1; }
            unlink(path);
            if (symlink(h.linkname, path) != 0) { close(fd); return -1; }
        } else {
            int out;
            unsigned long left = size;
            char buf[65536];
            if (ensure_parent_dir(path, 0755) != 0) { close(fd); return -1; }
            out = open(path, O_WRONLY | O_CREAT | O_TRUNC, (mode_t)(mode ? mode : 0644));
            if (out < 0) { close(fd); return -1; }
            while (left > 0) {
                size_t want = left > sizeof(buf) ? sizeof(buf) : (size_t)left;
                ssize_t r = read(fd, buf, want);
                if (r <= 0) { close(out); close(fd); return -1; }
                if (write_all(out, buf, (size_t)r) != 0) { close(out); close(fd); return -1; }
                left -= (unsigned long)r;
            }
            close(out);
            unsigned long pad = (512 - (size % 512)) % 512;
            if (pad) {
                char throwaway[512];
                if (read(fd, throwaway, pad) != (ssize_t)pad) { close(fd); return -1; }
            }
        }
    }
    return close(fd);
}
