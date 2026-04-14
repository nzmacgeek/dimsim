# Offline / Target-Root Install

This guide explains how to install packages into a BlueyOS root filesystem that
is not currently booted — for example when provisioning a disk image or chroot
before first boot.

## Overview

When `dimsim` is given the `--root <dir>` flag it operates in **offline mode**:

| Behaviour | Online (no `--root`) | Offline (`--root <dir>`) |
|-----------|---------------------|--------------------------|
| Files placed at | `/path` | `<dir>/path` |
| State database | `/var/lib/dimsim/state.db` | `<dir>/var/lib/dimsim/state.db` |
| `world` file | `/var/lib/dimsim/world` | `<dir>/var/lib/dimsim/world` |
| Cache | `/var/lib/dimsim/cache/` | `<dir>/var/lib/dimsim/cache/` |
| `preinst` script | Runs immediately | Staged as claw service (first boot) |
| `postinst` script | Runs immediately | Staged as claw service (first boot) |
| `prerm`/`postrm` | Run immediately | Skipped (target is not running) |

---

## Prerequisites

- A BlueyOS root filesystem mounted at a known path (e.g. `/mnt/blueyos`)
- The target must already have `/bin/bash` and `/etc/claw/` present — dimsim
  validates this before touching any files
- `dimsim` running on the host machine (build OS can be anything)

---

## Step 1 — Mount the target filesystem

```bash
# Example: loopback-mount a raw disk image
mount -o loop blueyos.img /mnt/blueyos
```

---

## Step 2 — Configure a repository for the target

The target's package database is separate from the host's. Configure repos
against the **target's** database:

```bash
dimsim --root /mnt/blueyos repo add main http://packages.blueyos.example
```

This downloads and trusts `root.json` from the repository into
`/mnt/blueyos/var/lib/dimsim/state.db`.

---

## Step 3 — Refresh the package index

```bash
dimsim --root /mnt/blueyos update
```

---

## Step 4 — Install packages

```bash
dimsim --root /mnt/blueyos install hello nano wget
```

```
The following packages will be installed:
  hello 1.0.0 [explicit]
  (offline mode — scripts will be staged for first boot via claw)
  Downloading hello-1.0.0-amd64.dpk...
  Staged preinst for hello as claw firstboot service
  Staged postinst for hello as claw firstboot service
  ✓ Installed hello 1.0.0
```

`--root` works for all package management commands:

```bash
dimsim --root /mnt/blueyos upgrade
dimsim --root /mnt/blueyos remove nano
dimsim --root /mnt/blueyos verify
dimsim --root /mnt/blueyos doctor
```

---

## Step 5 — What gets written into the target

For each package whose lifecycle scripts are staged, dimsim writes:

```
<dir>/var/lib/dimsim/firstboot/<pkg>/
  preinst.sh          Raw preinst script (from the package manifest)
  run-preinst         Self-removing wrapper (executed by claw)
  postinst.sh         Raw postinst script
  run-postinst        Self-removing wrapper

<dir>/etc/claw/services.d/
  dimsim-preinst-<pkg>.yml    Claw oneshot service (runs before postinst)
  dimsim-postinst-<pkg>.yml   Claw oneshot service (runs after preinst)
```

### Claw service ordering

Each staged service is declared as `type: oneshot` with:

```yaml
after:   claw-rootfs.target   # filesystem is mounted and writable
before:  claw-multiuser.target # completes before login is available
restart: no
```

The `postinst` service additionally declares:

```yaml
after: claw-rootfs.target dimsim-preinst-<pkg>
```

This ensures scripts run in the correct order: `preinst` first (user/group
setup etc.), then files (already placed by `dimsim --root`), then `postinst`
(service enable, configuration, etc.).

### Self-removing wrappers

Each `run-<script>` wrapper:

1. Calls `/bin/bash <script>.sh` (the lifecycle script from the package)
2. On success, **deletes its own claw service file** from
   `/etc/claw/services.d/` so it does not run again on subsequent boots
3. Exits with the script's exit code

A non-zero exit from a lifecycle script is logged by claw and prevents
`claw-multiuser.target` from being reached until the issue is resolved.

---

## Step 6 — Boot the target

Unmount and boot as normal:

```bash
umount /mnt/blueyos
```

On first boot, claw loads all unit files from `/etc/claw/services.d/`. The
staged `dimsim-preinst-*` and `dimsim-postinst-*` services activate
automatically between `claw-rootfs.target` and `claw-multiuser.target`.

```
[claw] Activating service: dimsim-preinst-hello
[claw] Activating service: dimsim-postinst-hello
[claw] The Magic Claw welcomes all challengers. Log in if you dare!
```

After each service runs successfully it removes itself from
`/etc/claw/services.d/`. Subsequent reboots see no firstboot services.

---

## BlueyOS system validation

`dimsim --root` validates the target directory before making any changes:

| Check | Required path | Failure message |
|-------|--------------|-----------------|
| claw init present | `<dir>/etc/claw/` | `missing /etc/claw/` |
| bash present | `<dir>/bin/bash` | `missing /bin/bash` |

If either check fails the install is aborted with no modifications to the
target filesystem.

---

## Full example workflow

```bash
# 1. Create a minimal BlueyOS rootfs (out of scope for this guide)
# 2. Mount the rootfs
mount -o loop blueyos.img /mnt/blueyos

# 3. Configure the package repository for the target
dimsim --root /mnt/blueyos repo add main http://packages.blueyos.example

# 4. Fetch the package index
dimsim --root /mnt/blueyos update

# 5. Install packages
dimsim --root /mnt/blueyos install base-tools openssh-server myapp

# 6. Verify the install
dimsim --root /mnt/blueyos verify

# 7. Check what claw firstboot services were staged
ls /mnt/blueyos/etc/claw/services.d/dimsim-*.yml

# 8. Unmount and boot
umount /mnt/blueyos
```

---

## Upgrading the target offline

```bash
dimsim --root /mnt/blueyos update
dimsim --root /mnt/blueyos upgrade
```

Upgraded packages that have `postinst` scripts will stage new firstboot
services. The target picks them up on its next boot.

---

## Known Issues and External Dependencies

### D-3: `getcwd` failure in bash service scripts (depends on kernel fix)

**Status:** OPEN — depends on kernel K-6 and claw C-1

Every bash process spawned by claw fails with:
```
shell-init: error retrieving current directory: getcwd: cannot access parent directories: No such file or directory
```

This is non-fatal for script execution but causes bash to operate without a known working directory, which breaks any script using relative paths or `pwd`.

**Root cause:** The BlueyOS kernel VFS implementation does not correctly populate `.` and `..` directory entries (K-6), and claw does not `chdir` to a valid directory before spawning service processes (C-1).

**Resolution:** This issue must be fixed in the kernel and claw, not in dimsim.

---

### D-4: Interleaved log output from concurrent services (depends on kernel/claw fix)

**Status:** OPEN — kernel serialization issue

The boot log shows log lines from multiple concurrent bash/claw processes interleaved in a single stream. This makes debugging very difficult.

**Root cause:** The kernel's VGA output (`kprintf`) is not serialized against user-mode writes to `/var/log/kernel.log`.

**Fix options:**
1. **Short term:** Add a kernel spinlock around the VGA write path
2. **Long term:** Route all service stdout/stderr through `yap` (the syslog daemon) with a sequence number prefix

**Resolution:** This issue must be fixed in the kernel and/or claw, not in dimsim.

---

## Relationship to the running-system install flow

When `--root` is **not** provided, `dimsim install` behaves exactly as
before — preinst/postinst scripts are executed immediately in the current
process. The `--root` path is purely additive; no existing behaviour changes.
