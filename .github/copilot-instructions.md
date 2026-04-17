# Agent Instructions for claw

> These instructions apply to **every** Copilot coding agent working in this
> repository.  Read this file before starting any task.

---

## 1. Ecosystem overview — look at ALL nzmacgeek repos

BlueyOS is a multi-repo operating system project.  Before making any change
you must check whether the change touches an interface that spans multiple
repos and consult the relevant repo:

| Repo | Role | Key files |
|------|------|-----------|
| **nzmacgeek/biscuits** | i386 kernel, VFS, syscalls, drivers | `kernel/`, `drivers/`, `fs/`, `net/` |
| **nzmacgeek/claw** | PID 1 init daemon (service manager) | `src/claw/main.c`, `src/core/service/supervisor.c` |
| **nzmacgeek/matey** | getty / login prompt | `matey.c` |
| **nzmacgeek/walkies** | Network configuration tool (netctl) | see WALKIES_PROMPT.md in biscuits |
| **nzmacgeek/yap** | Syslog daemon / log rotation | `yap.c` (or equivalent) |
| **nzmacgeek/dimsim** | Package manager / firstboot scripts | `cmd/`, `internal/`, `template/` |
| **nzmacgeek/musl-blueyos** | musl libc patched for BlueyOS syscalls | `arch/i386/`, `src/` |
| **nzmacgeek/blueyos-bash** | Bash 5 patched for BlueyOS | `configure.ac`, patches |

**When working on service lifecycle or IPC, check biscuits syscall definitions
and musl-blueyos to understand caller expectations.**

---

## 2. Verbosity control

Claw reads the `verbose=N` kernel boot argument (propagated via the `VERBOSE`
environment variable set by PID 1) and all logging must respect it:

```
VERBOSE=0  quiet (default) — errors + lifecycle events only
VERBOSE=1  info — detailed operational messages
VERBOSE=2  debug — all trace messages
```

**Retrofit rule:** when modifying any logging call, check that the log level
is appropriate for the message content and add a verbosity guard for
debug-only messages.

---

## 3. Coding conventions

- Userspace code is C99 (`-std=c99`) with musl libc, statically linked.
- 4-space indentation.  No tabs except in Makefiles.
- Services are started via `execve` (never through a shell intermediary).
- All executables must be invoked with an explicit environment via `execve`; do
  not use `system()` or `popen()`.
- Always add a corresponding entry in `docs/DEFECT_ANALYSIS.md` when fixing a
  catalogued defect.

---

## 4. Build and Packaging

- For complete builds, update the project build number before building.
- When producing a dimsim package from a complete build, include that updated
  build number in the package version.
- Do not update the build number for syntax-only checks, single-file
  compilation, or other partial validation runs.
- Toolchain: `/opt/blueyos-cross/bin/i386-blueyos-elf-gcc` +
  sysroot `/opt/blueyos-sysroot`; the `configure-blueyos` wrapper handles all
  cross-compile flags automatically (specs file, CRT paths, `-nostdinc`, etc.).
- Always use an out-of-tree build directory to avoid polluting the source tree
  with object files — stale `.o`/`.a` files in the source tree confuse VPATH
  and break cross-builds.

```bash
# Standard BlueyOS cross-compile (recommended):
mkdir build && cd build
../configure-blueyos --prefix=/ --sbindir=/sbin --bindir=/bin \
                     --sysconfdir=/etc --localstatedir=/var
make -j$(nproc)

# Explicit cross-compile (if configure-blueyos is unavailable):
mkdir build && cd build
../configure --host=i386-blueyos-elf --enable-static-binary \
             --prefix=/ --sbindir=/sbin --bindir=/bin \
             --sysconfdir=/etc --localstatedir=/var
make -j$(nproc)
```

---

## 5. Repo memory hygiene

When you discover a new fact about the codebase that would help future agents,
save it to `/memories/repo/` with a short topic filename and a one-sentence
fact citing the source file and line.  Update existing entries when they become
stale.
