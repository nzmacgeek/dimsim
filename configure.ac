AC_INIT([dimsim],[2.0.0-c],[maintainers@blueyos.example])
AM_INIT_AUTOMAKE([foreign])
AC_PROG_CC
AC_ARG_ENABLE([static],
  AS_HELP_STRING([--disable-static],[build without -static]),
  [],
  [enable_static=yes])
AS_IF([test "x$enable_static" = "xyes"], [EXTRA_LDFLAGS="-static"], [EXTRA_LDFLAGS=""])
AC_SUBST([EXTRA_LDFLAGS])
AC_CONFIG_FILES([Makefile])
AC_OUTPUT
