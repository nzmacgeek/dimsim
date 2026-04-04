# dimsim

A package manager for [BlueyOS](https://github.com/nzmacgeek/biscuits) — an imaginary Linux-like operating system whose userland consists of **bash**, **musl-libc**, and **claw** (init).

## Documentation

| Guide | Description |
|-------|-------------|
| [Creating a Package](docs/creating-a-package.md) | Build a `.dpk` from scratch with `dpkbuild` |
| [Repository Setup](docs/repository-setup.md) | Create and maintain a TUF-signed repository |
| [Package Management](docs/package-management.md) | Install, remove, upgrade, and maintain packages |
| [Offline / Target-Root Install](docs/offline-install.md) | Provision packages into a non-booted BlueyOS rootfs |

## Binaries

| Binary | Purpose |
|--------|---------|
| `dimsim` | Package manager CLI |
| `dpkbuild` | Build and scaffold `.dpk` packages |

## Building for BlueyOS

Both binaries are compiled as fully **static** executables (`CGO_ENABLED=0`) so they carry no dependency on glibc or any shared library.

```bash
# Build both binaries for linux/amd64 (default)
make

# Cross-compile for another architecture
make GOARCH=arm64

# Outputs
bin/dimsim
bin/dpkbuild
```

You can also build directly with `go`:

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "-w -s" -o bin/dimsim ./cmd/dimsim
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "-w -s" -o bin/dpkbuild ./cmd/dpkbuild
```

## State files

| Path | Purpose |
|------|---------|
| `/var/lib/dimsim/state.db` | SQLite database (packages, repos, files, TUF metadata) |
| `/var/lib/dimsim/world` | Explicitly installed packages, one per line |
| `/var/lib/dimsim/cache/` | Downloaded `.dpk` files |
| `/var/lib/dimsim/staging/` | Transactional install staging area |

## dimsim CLI

```
dimsim repo add <name> <url>   Add a repository
dimsim repo list               List configured repositories
dimsim update                  Refresh TUF metadata for all repos
dimsim search <query>          Search available packages
dimsim info <pkg>              Show package details
dimsim install <pkg...>        Install packages
dimsim remove <pkg...>         Remove packages
dimsim upgrade [pkg...]        Upgrade all or specific packages
dimsim autoremove              Remove unneeded auto-installed packages
dimsim verify                  Verify installed file integrity
dimsim pin <pkg>               Prevent a package from being upgraded
dimsim unpin <pkg>             Allow a package to be upgraded again
dimsim doctor                  Check for broken dependencies and other issues
```

## Repository security (TUF)

Repositories use [The Update Framework](https://theupdateframework.io/) with four signed metadata roles:

- **root** — root of trust; contains public keys and role definitions
- **timestamp** — short-lived; contains the hash of `snapshot.json`
- **snapshot** — contains hashes of all other metadata files
- **targets** — contains per-package hash, size, and custom metadata

Signature verification uses **ed25519**. Metadata expiry is enforced before any package is downloaded.

## Package format (.dpk)

A `.dpk` file is a **tar.zst** archive with two top-level directories:

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
    {"path": "/usr/bin/example", "hash": "<sha256>", "size": 12345, "mode": "0755"}
  ],
  "scripts": {
    "preinst":  "#!/bin/bash\nset -e\nexit 0\n",
    "postinst": "#!/bin/bash\nset -e\nexit 0\n",
    "prerm":    "#!/bin/bash\nset -e\nexit 0\n",
    "postrm":   "#!/bin/bash\nset -e\nexit 0\n"
  }
}
```

## dpkbuild

```bash
# Scaffold a new package directory
dpkbuild init mypkg

# Build a .dpk from a package directory
dpkbuild build mypkg/
```
