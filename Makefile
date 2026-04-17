CC ?= cc
CFLAGS ?= -O2 -Wall -Wextra -std=c11
LDFLAGS ?=
STATIC ?= 1

PREFIX ?= /usr
BINDIR ?= $(PREFIX)/bin

OUTDIR := bin
DIMSIM := $(OUTDIR)/dimsim
DPKBUILD := $(OUTDIR)/dpkbuild

COMMON_SRCS := src/common.c src/tar.c src/manifest.c

ifeq ($(STATIC),1)
# BlueyOS targets musl. Static linking on glibc hosts may have NSS limitations.
LDFLAGS += -static
endif

.PHONY: all clean test install uninstall

all: $(DIMSIM) $(DPKBUILD)

$(OUTDIR):
	mkdir -p $(OUTDIR)

$(DIMSIM): $(OUTDIR) src/dimsim.c $(COMMON_SRCS) src/common.h src/tar.h src/manifest.h
	$(CC) $(CFLAGS) -o $@ src/dimsim.c $(COMMON_SRCS) $(LDFLAGS)

$(DPKBUILD): $(OUTDIR) src/dpkbuild.c $(COMMON_SRCS) src/common.h src/tar.h src/manifest.h
	$(CC) $(CFLAGS) -o $@ src/dpkbuild.c $(COMMON_SRCS) $(LDFLAGS)

test: all
	@echo "No standalone test suite is currently defined."

install: all
	install -d "$(DESTDIR)$(BINDIR)"
	install -m 0755 $(DIMSIM) "$(DESTDIR)$(BINDIR)/dimsim"
	install -m 0755 $(DPKBUILD) "$(DESTDIR)$(BINDIR)/dpkbuild"

uninstall:
	rm -f "$(DESTDIR)$(BINDIR)/dimsim" "$(DESTDIR)$(BINDIR)/dpkbuild"

clean:
	rm -rf $(OUTDIR)
