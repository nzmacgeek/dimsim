# Agent Instructions for BlueyOS / dimsim

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

**When working on package install/remove flows, always check claw and biscuits
integration expectations as well as musl-blueyos runtime behavior.**

---

## 2. Verbosity control — standard practice across all BlueyOS software

All userspace daemons and tools must honour a `--verbose` / `-v` flag **and**
the `VERBOSE` environment variable (set by claw from the kernel `verbose=`
arg):

```
VERBOSE=0  quiet (default) — errors + lifecycle events only
VERBOSE=1  info — detailed operational messages
VERBOSE=2  debug — all trace messages
```

**Retrofit rule:** when modifying any logging call in a userspace daemon,
check that the log level used is appropriate for the message content, and
add a verbosity guard if the message is debug-only.

---

## 3. Coding conventions

- Userspace tools are C11 (`-std=gnu11`) with musl libc, statically linked.
- 4-space indentation.  No tabs except in Makefiles.
- Prefer explicit error returns and clear stderr diagnostics.
- Keep package database and manifest handling deterministic and text-friendly.
- Always add/update docs when behavior or command syntax changes.

---

## 4. Build and packaging

- For complete builds, update the project build number before building.
- When producing a dimsim package from a complete build, include that updated
  build number in the package version.
- Do not update the build number for syntax-only checks, single-file
  compilation, or other partial validation runs.
- Toolchain target is musl-blueyos; static linking is required for deployment.
- Prefer out-of-tree builds for autotools (`mkdir build && cd build`).

```bash
./autogen.sh
mkdir -p build && cd build
../configure --with-musl --enable-static-binary \
  --prefix= --bindir=/bin --sysconfdir=/etc --localstatedir=/var
make -j$(nproc)
```

---

## 5. Repo memory hygiene

When you discover a new fact about the codebase that would help future agents,
save it to `/memories/repo/` with a short topic filename and a one-sentence
fact citing the source file and line. Update existing entries when they become
stale.
