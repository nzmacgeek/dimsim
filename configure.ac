AC_PREREQ([2.69])
AC_INIT([dimsim], [2.0.0-c1], [https://github.com/nzmacgeek/dimsim/issues])
AC_CONFIG_SRCDIR([src/cli/main.c])
AC_CONFIG_AUX_DIR([build-aux])
AM_INIT_AUTOMAKE([foreign subdir-objects silent-rules])
AC_CANONICAL_HOST

AC_ARG_ENABLE([static-binary],
    AS_HELP_STRING([--enable-static-binary], [Link dimsim as a fully static executable]),
    [enable_static_binary="$enableval"],
    [enable_static_binary=yes])

AC_ARG_WITH([musl],
    AS_HELP_STRING([--with-musl[=PREFIX]], [Use musl-gcc from PATH or PREFIX/bin/musl-gcc]),
    [with_musl="$withval"],
    [with_musl=yes])

AC_ARG_WITH([sysroot],
    AS_HELP_STRING([--with-sysroot=PATH], [Compiler sysroot path (for musl-blueyos)]),
    [with_sysroot="$withval"],
    [with_sysroot=""])

AS_IF([test "x$with_musl" != "xno"], [
    AS_IF([test "x$with_musl" = "xyes"], [
        AC_PATH_PROG([MUSL_GCC], [musl-gcc])
        AS_IF([test -z "$MUSL_GCC"], [
            AC_MSG_ERROR([--with-musl requested but musl-gcc was not found in PATH])
        ])
        CC="$MUSL_GCC"
    ], [
        AS_IF([test -x "$with_musl"], [
            CC="$with_musl"
        ], [
            AS_IF([test -x "$with_musl/bin/musl-gcc"], [
                CC="$with_musl/bin/musl-gcc"
            ], [
                AC_MSG_ERROR([cannot find musl-gcc at $with_musl or $with_musl/bin/musl-gcc])
            ])
        ])
    ])
])

AC_PROG_CC

DIMSIM_CFLAGS="-std=gnu11"
AS_IF([test "x$GCC" = "xyes"], [
    DIMSIM_CFLAGS="$DIMSIM_CFLAGS -Wall -Wextra -Wpedantic"
])

DIMSIM_LDFLAGS=""
AS_IF([test "x$enable_static_binary" = "xyes"], [
    DIMSIM_LDFLAGS="$DIMSIM_LDFLAGS -static -Wl,--gc-sections"
    AC_DEFINE([DIMSIM_STATIC_BINARY], [1], [Set to 1 when building static binaries])
], [
    AC_DEFINE([DIMSIM_STATIC_BINARY], [0], [Set to 1 when building static binaries])
])

AS_IF([test -n "$with_sysroot"], [
    DIMSIM_CFLAGS="$DIMSIM_CFLAGS --sysroot=$with_sysroot"
    DIMSIM_LDFLAGS="$DIMSIM_LDFLAGS --sysroot=$with_sysroot"
])

AC_SUBST([DIMSIM_CFLAGS])
AC_SUBST([DIMSIM_LDFLAGS])

BUILD_NUMBER=${BUILD_NUMBER-}
AS_IF([test -z "$BUILD_NUMBER"], [
    BUILD_NUMBER=`git rev-list --count HEAD 2>/dev/null || echo 1`
])
BUILD_COMMIT=`git rev-parse --short HEAD 2>/dev/null || echo unknown`
BUILD_DATE=`date -u '+%Y-%m-%d'`
BUILD_TIME=`date -u '+%H:%M:%S'`
BUILD_HOST=`hostname 2>/dev/null || echo unknown-host`
BUILD_USER=`whoami 2>/dev/null || echo unknown-user`

AC_SUBST([BUILD_NUMBER])
AC_SUBST([BUILD_COMMIT])
AC_SUBST([BUILD_DATE])
AC_SUBST([BUILD_TIME])
AC_SUBST([BUILD_HOST])
AC_SUBST([BUILD_USER])

AC_CONFIG_HEADERS([config.h])
AC_CONFIG_FILES([
    Makefile
    src/Makefile
    include/dimsim/version.h
])
AC_OUTPUT
