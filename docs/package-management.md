# Package Management (C edition)

This guide covers local package lifecycle commands implemented by the C version of `dimsim`.

> Use `--root <dir>` to operate on an offline target root filesystem.

## Install a package

```bash
dimsim install hello-1.0.0-amd64.dpk
```

## Install multiple packages

```bash
dimsim install a-1.0.0-amd64.dpk b-2.0.0-amd64.dpk
```

## List installed packages

```bash
dimsim list
```

## Show package details

```bash
dimsim info hello
```

## Remove packages

```bash
dimsim remove hello
```

Remove and pass `purge` to package removal scripts:

```bash
dimsim remove --purge hello
```

## Verify installed files

```bash
dimsim verify
```

When all files match recorded hashes:

```
✓ All installed files verified successfully.
```
