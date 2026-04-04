# Package Management

This guide covers day-to-day package management on BlueyOS using `dimsim`.

> **Note**: most commands that modify system state require root privileges because
> they write to `/` and `/var/lib/dimsim/`. Run them with `sudo` or as root.

> **Offline installs**: to install packages into a non-booted target rootfs
> (e.g. when provisioning a disk image), pass `--root <dir>` to any command.
> See [Offline / Target-Root Install](./offline-install.md) for the full guide.

---

## First-time setup

### Add a repository

```bash
dimsim repo add main http://packages.blueyos.example
```

dimsim downloads and trusts `root.json` from the URL. All subsequent operations
verify the full TUF chain before downloading anything.

### Refresh the package index

```bash
dimsim update
```

Run this whenever you want to see the latest packages available in any
configured repository.

---

## Installing packages

### Search for a package

```bash
dimsim search hello
```

```
NAME                      VERSION      REPO       DESCRIPTION
----                      -------      ----       -----------
hello                     1.0.0        main       Prints a friendly greeting
```

### View package details

```bash
dimsim info hello
```

```
Name:        hello
Version:     1.0.0
Arch:        amd64
Description: Prints a friendly greeting
Repository:  main
Status:      not installed
```

### Install a package

```bash
dimsim install hello
```

```
The following packages will be installed:
  hello 1.0.0 [explicit]
  Downloading hello-1.0.0-amd64.dpk...
  ✓ Installed hello 1.0.0
```

dimsim:

1. Resolves all dependencies and queues them for installation.
2. Downloads every `.dpk` needed, verifying hash and size against
   `targets.json` before writing to disk.
3. Extracts each package to a staging area.
4. Runs `preinst` (if provided).
5. Moves files to their final locations.
6. Runs `postinst` (if provided).
7. Records the package and every installed file in
   `/var/lib/dimsim/state.db`.
8. Adds explicitly requested packages to `/var/lib/dimsim/world`.

If any step fails, all changes made so far in that transaction are rolled back.

### Install multiple packages at once

```bash
dimsim install nano curl wget
```

### Check what is installed

```bash
dimsim info hello
```

```
Name:        hello
Version:     1.0.0
Arch:        amd64
Description: Prints a friendly greeting
Installed:   2026-04-04 09:00:00
Files:       1 file(s)
```

---

## Removing packages

### Remove a package

```bash
dimsim remove hello
```

```
  ✓ Removed hello 1.0.0
```

dimsim:

1. Checks that no other installed package depends on `hello`.
2. Runs `prerm` (loaded from the cached `.dpk`).
3. Removes every file that was recorded at install time.
4. Runs `postrm`.
5. Removes the package record from `/var/lib/dimsim/state.db`.
6. Removes `hello` from `/var/lib/dimsim/world`.

### Remove multiple packages

```bash
dimsim remove nano curl
```

### Remove with configuration files (purge)

```bash
dimsim remove --purge hello
```

The `--purge` flag is passed through to the `postrm` script via the argument
`purge`. Use it in your `postrm` to delete configuration data:

```bash
#!/bin/bash
set -e
if [ "$1" = "purge" ]; then
    rm -rf /etc/hello
fi
exit 0
```

---

## Keeping the system up to date

### Fetch the latest package index

```bash
dimsim update
```

Always run this before upgrading so dimsim knows about new versions.

### Upgrade all packages

```bash
dimsim update && dimsim upgrade
```

```
  hello: 1.0.0 -> 1.1.0
  Downloading hello-1.1.0-amd64.dpk...
  ✓ Installed hello 1.1.0
```

Only packages with a newer version available in any configured repository
are upgraded. Pinned packages are skipped.

### Upgrade specific packages

```bash
dimsim upgrade hello nano
```

### Remove automatically installed dependencies no longer needed

```bash
dimsim autoremove
```

When you install package `A` which depends on `libfoo`, dimsim marks `libfoo`
as automatically installed. If you later remove `A`, `libfoo` is no longer
needed. `autoremove` cleans up these orphaned packages.

---

## Pinning packages

Pin a package to prevent it from being upgraded:

```bash
dimsim pin hello
```

```
✓ Pinned hello
```

The package is still reported in `dimsim info` and `dimsim upgrade` output, but
it is skipped. Attempting to remove a pinned package requires unpinning first.

Unpin to allow upgrades again:

```bash
dimsim unpin hello
```

```
✓ Unpinned hello
```

---

## Verifying installed files

Check that every file belonging to an installed package still matches its
recorded SHA-256 hash:

```bash
dimsim verify
```

```
  MODIFIED /usr/local/bin/hello (from hello)
```

or, if everything is intact:

```
✓ All installed files verified successfully.
```

Use this to detect accidental modifications or file system corruption.

---

## System health check

```bash
dimsim doctor
```

`doctor` checks for:

- **Broken dependencies** — packages whose required dependencies are not installed
  or do not satisfy the version constraint.
- **Missing files** — files recorded in the state database that no longer exist
  on disk.
- **Expired TUF metadata** — repositories whose `timestamp.json` has expired (run
  `dimsim update` to refresh).

```
  ✗ Broken dependency: myapp requires libfoo>=2.0 (not installed)
  ✗ Missing file: /usr/local/bin/hello (from hello)
  Found 2 issue(s).
```

or if everything is healthy:

```
✓ System is healthy.
```

---

## Managing repositories

### List configured repositories

```bash
dimsim repo list
```

```
NAME                 ENABLED  PRIORITY URL
----                 -------  -------- ---
main                 yes      100      http://packages.blueyos.example
testing              yes      50       http://testing.blueyos.example
```

### Add a repository with custom priority

Higher priority repositories are searched first. If the same package exists in
multiple repositories, the one with the highest priority wins.

```bash
dimsim repo add testing http://testing.blueyos.example --priority=50
```

### Remove a repository

```bash
dimsim repo remove testing
```

---

## State files reference

| File | Purpose |
|------|---------|
| `/var/lib/dimsim/state.db` | SQLite database — packages, files, repos, TUF metadata cache |
| `/var/lib/dimsim/world` | One package name per line — explicitly installed packages |
| `/var/lib/dimsim/cache/` | Downloaded `.dpk` files (safe to clear) |
| `/var/lib/dimsim/staging/` | Temporary extraction area during install |

### The world file

`/var/lib/dimsim/world` records which packages were explicitly requested by the
user (as opposed to installed automatically as a dependency). This is used by
`autoremove` to decide which packages are safe to remove.

Example:

```
hello
nano
wget
```

You can edit this file by hand if you want to mark a package as explicitly
installed (preventing `autoremove` from removing it) or as automatic
(allowing `autoremove` to remove it if it is no longer needed).
