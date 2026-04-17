# dimsim

A package manager for [BlueyOS](https://github.com/nzmacgeek/biscuits) — an imaginary Linux-like operating system whose userland consists of **bash**, **musl-libc**, and **claw** (init).

## Documentation

| Guide | Description |
|-------|-------------|
| [Creating a Package](docs/creating-a-package.md) | Build a `.dpk` from scratch with `dpkbuild` |
| [Package Management](docs/package-management.md) | Install, remove, inspect, and verify local packages |
| [Offline / Target-Root Install](docs/offline-install.md) | Provision packages into a non-booted BlueyOS rootfs |

## Binaries

| Binary | Purpose |
|--------|---------|
| `dimsim` | Package manager CLI |
| `dpkbuild` | Build and scaffold `.dpk` packages |

## Building for BlueyOS

Both binaries are compiled as fully **static** C executables (`-static`) so they carry no dependency on glibc or any shared library.

```bash
# Build both binaries for linux/amd64 (default)
make

# Build with the BlueyOS musl toolchain profile
make blueyos

# Outputs
bin/dimsim
bin/dpkbuild
```

Install to `/usr/bin`:

```bash
make install
```

Use a non-default musl compiler path when needed:

```bash
make blueyos MUSL_CC=/opt/musl/bin/musl-gcc
```

## State files

| Path | Purpose |
|------|---------|
| `/var/lib/dimsim/state.db` | SQLite database (packages, repos, files, TUF metadata) |
| `/var/lib/dimsim/world` | Explicitly installed packages, one per line |
| `/var/lib/dimsim/cache/` | Downloaded `.dpk` files |
| `/var/lib/dimsim/staging/` | Transactional install staging area |

## dimsim CLI (C edition)

```
dimsim install <pkg.dpk...>    Install local package archives
dimsim remove <pkg...>         Remove installed packages
dimsim info <pkg>              Show installed package details
dimsim verify                  Verify installed file integrity
dimsim list                    List installed packages
```

## Package format (.dpk)

A `.dpk` file is a **tar** archive with two top-level directories. The C rewrite intentionally dropped zstd compression to avoid runtime compression-library dependencies in static builds.

```
meta/
  manifest.json        Package metadata and file list
  scripts/
    preinst            Run before files are placed  (bash)
    postinst           Run after files are placed   (bash)
    prerm              Run before files are removed (bash)
    postrm             Run after files are removed  (bash)
payload/               Files installed to / on the target system
```

### manifest.json fields

```json
{
  "name": "example",
  "version": "1.0.0",
  "arch": "amd64",
  "description": "An example package",
  "depends":    ["libc>=1.0"],
  "recommends": [],
  "conflicts":  [],
  "provides":   [],
  "maintainer": "Your Name <you@example.com>",
  "homepage":   "https://example.com",
  "files": [
    {"path": "/usr/bin/example", "hash": "<sha256>", "size": 12345, "mode": "0755"},
    {"path": "/usr/bin/example-link", "hash": "<sha256(link target)>", "size": 7, "mode": "0777", "type": "symlink", "target": "example"}
  ],
  "scripts": {
    "preinst":  "#!/bin/bash\nset -e\nexit 0\n",
    "postinst": "#!/bin/bash\nset -e\nexit 0\n",
    "prerm":    "#!/bin/bash\nset -e\nexit 0\n",
    "postrm":   "#!/bin/bash\nset -e\nexit 0\n"
  }
}
```

Symlink payloads are supported natively. Symlink manifest entries record the
link target, and the recorded hash is computed from that link target rather
than the contents of the referenced file.

## dpkbuild

```bash
# Scaffold a new package directory
dpkbuild init mypkg

# Build a .dpk from a package directory
dpkbuild build mypkg/
```
