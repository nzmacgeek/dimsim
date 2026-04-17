# dimsim

A package manager for [BlueyOS](https://github.com/nzmacgeek/biscuits).

`dimsim` has been rebuilt in C for musl-blueyos compatibility and static deployment into BlueyOS images.

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
| `dimsim` | Package manager CLI (C rewrite, static musl build) |
| `dpkbuild` | Build and scaffold `.dpk` packages |

## Building for BlueyOS (C rewrite)

The `dimsim` binary is built via autoconf/automake and linked fully static by default.

```bash
# Build C dimsim + Go dpkbuild
make

# Build only dimsim in an out-of-tree build dir
./autogen.sh
mkdir -p build && cd build
../configure-blueyos --prefix= --bindir=/bin --sysconfdir=/etc --localstatedir=/var
make -j$(nproc)

# Verify static linking
file src/dimsim
ldd src/dimsim 2>&1 || true

# Install into a host-side sysroot for image construction
make install DESTDIR=/tmp/blueyos-root

# Outputs
_build/src/dimsim
bin/dpkbuild
```

`make` uses a convenience wrapper that runs `autogen.sh`, configures with static linking, and builds in `_build/`.

Useful variables:

```bash
# Optional: point to a specific musl toolchain prefix
make MUSL_PREFIX=/opt/blueyos-cross

# Optional: override sysroot
make SYSROOT=/opt/blueyos-sysroot

# Optional: set build number embedded into --version output
make BUILD_NUMBER=42
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
dimsim --version               Show version/build metadata
dimsim -v/-vv ...              Increase log verbosity
dimsim --verbose-level 0|1|2   Set verbosity explicitly
dimsim --root <path> ...       Operate on a target root filesystem

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

The current C rewrite fully implements repository list/add/remove. Additional package operations are being migrated from the historical Go implementation.

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
