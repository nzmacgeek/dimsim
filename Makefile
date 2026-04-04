# Makefile for dimsim — static binaries for BlueyOS (musl-libc, bash, claw)
#
# BlueyOS provides only: bash, musl-libc, claw (init).
# All binaries must be fully statically linked (CGO_ENABLED=0) so they carry
# no dependency on glibc or any external shared library.

GOOS        ?= linux
GOARCH      ?= amd64
CGO_ENABLED  = 0

# -w -s strips DWARF debug info and symbol table to shrink the binary.
LDFLAGS     := -w -s

BINDIR      := bin
DIMSIM      := $(BINDIR)/dimsim
DPKBUILD    := $(BINDIR)/dpkbuild

.PHONY: all clean dimsim dpkbuild test vet

all: dimsim dpkbuild

dimsim: $(DIMSIM)
dpkbuild: $(DPKBUILD)

$(BINDIR):
	mkdir -p $(BINDIR)

$(DIMSIM): $(BINDIR) $(shell find cmd/dimsim internal -name '*.go')
	CGO_ENABLED=$(CGO_ENABLED) GOOS=$(GOOS) GOARCH=$(GOARCH) \
		go build -ldflags "$(LDFLAGS)" -o $@ ./cmd/dimsim

$(DPKBUILD): $(BINDIR) $(shell find cmd/dpkbuild internal -name '*.go')
	CGO_ENABLED=$(CGO_ENABLED) GOOS=$(GOOS) GOARCH=$(GOARCH) \
		go build -ldflags "$(LDFLAGS)" -o $@ ./cmd/dpkbuild

vet:
	CGO_ENABLED=$(CGO_ENABLED) go vet ./...

test:
	CGO_ENABLED=$(CGO_ENABLED) go test ./...

clean:
	rm -rf $(BINDIR)

# Cross-compile for a specific architecture (e.g. make cross GOARCH=arm64)
.PHONY: cross
cross:
	$(MAKE) GOARCH=$(GOARCH)
