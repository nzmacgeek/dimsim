# Creating a Package

This guide walks through the complete lifecycle of building a `.dpk` package — from
scaffolding the directory to a signed, publishable archive ready for a dimsim repository.

## Prerequisites

- `dpkbuild` built and on your `PATH` (see [README](../README.md#building-for-blueyos))
- The files you want to distribute

---

## Step 1 — Scaffold a new package

```bash
dpkbuild init hello
```

This creates:

```
hello/
  meta/
    manifest.json        Package metadata
    scripts/
      preinst            Runs before files are placed
      postinst           Runs after files are placed
      prerm              Runs before files are removed
      postrm             Runs after files are removed
  payload/               Files to install on the target system
  payload/.gitkeep
```

---

## Step 2 — Edit the manifest

Open `hello/meta/manifest.json` and fill in every field:

```json
{
  "name": "hello",
  "version": "1.0.0",
  "arch": "amd64",
  "description": "Prints a friendly greeting",
  "depends": [],
  "recommends": [],
  "conflicts": [],
  "provides": [],
  "maintainer": "Ada Lovelace <ada@blueyos.example>",
  "homepage": "https://blueyos.example/hello"
}
```

### Dependency syntax

| Expression | Meaning |
|-----------|---------|
| `libc` | Any version |
| `libc>=1.2` | Version 1.2 or newer |
| `libc=1.2.3` | Exactly 1.2.3 |
| `libc<2.0` | Older than 2.0 |

---

## Step 3 — Add payload files

Place everything that should end up on the target system under `hello/payload/`.
The directory tree under `payload/` is mapped directly onto `/` at install time.

**Example** — a standalone shell script:

```bash
mkdir -p hello/payload/usr/local/bin
cat > hello/payload/usr/local/bin/hello << 'EOF'
#!/bin/bash
echo "Hello from BlueyOS!"
EOF
chmod 0755 hello/payload/usr/local/bin/hello
```

After install, `/usr/local/bin/hello` will exist on the target system.

---

## Step 4 — Write lifecycle scripts (optional)

All scripts use `/bin/bash` (BlueyOS has bash but no `/bin/sh`).

### `meta/scripts/preinst` — runs before files are placed

```bash
#!/bin/bash
set -e
# Example: create a system user the package needs

# IMPORTANT: BlueyOS does not have getent. Use grep against /etc/passwd and /etc/group:
# Check if user exists: grep -q '^username:' /etc/passwd
# Check if group exists: grep -q '^groupname:' /etc/group

# if ! grep -q '^hellouser:' /etc/passwd; then
#   useradd -r -s /bin/false hellouser
# fi

exit 0
```

### `meta/scripts/postinst` — runs after files are placed

```bash
#!/bin/bash
set -e
echo "hello installed successfully"
exit 0
```

### `meta/scripts/prerm` — runs before files are removed

```bash
#!/bin/bash
set -e
# Example: stop a service before removal
# systemctl stop hello || true
exit 0
```

### `meta/scripts/postrm` — runs after files are removed

```bash
#!/bin/bash
set -e
# Cleanup any data left behind
exit 0
```

> A non-zero exit from `preinst` or `postinst` aborts the install and triggers
> rollback. Errors in `prerm`/`postrm` are reported but do not block removal.

---

## Step 5 — Build the package

```bash
dpkbuild build hello
```

Output:

```
Building hello-1.0.0-amd64.dpk...
✓ Built hello-1.0.0-amd64.dpk (3271 bytes)
  SHA256: a3f8c2...
```

The resulting file `hello-1.0.0-amd64.dpk` is a `tar` archive containing:

```
meta/manifest.json
payload/usr/local/bin/hello
```

---

## Step 6 — Publish to a repository

See [Repository Setup](./repository-setup.md) for how to add the `.dpk` to a
TUF-signed repository so it can be installed with `dimsim install hello`.

---

## Architecture field

The `arch` field defaults to the architecture of the machine running `dpkbuild`.
Override it in `manifest.json` if you are cross-compiling or distributing
architecture-independent packages:

| Value | Platform |
|-------|---------|
| `amd64` | x86-64 |
| `arm64` | AArch64 |
| `noarch` | Architecture-independent |

---

## Tips

- **File permissions**: `dpkbuild build` preserves the mode bits of files under
  `payload/`. Make executables `chmod 0755` before building.
- **File hashes**: The `files` array in `manifest.json` is populated automatically
  at build time. You do not need to fill it in manually.
- **Scripts embedded in manifest**: Script content is stored inside
  `manifest.json` under the `scripts` key. You do not need to distribute them
  separately.
- **Versioning**: Use `MAJOR.MINOR.PATCH` (e.g. `1.2.3`). Pre-release suffixes
  like `1.0.0-beta1` are supported but sort *before* `1.0.0`.
