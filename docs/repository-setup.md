# Repository Setup

A dimsim repository is a directory of static files served over HTTP. All
metadata is cryptographically signed using
[The Update Framework (TUF)](https://theupdateframework.io/) with `ed25519` keys.

## Directory layout

```
my-repo/
  root.json        Root of trust — public keys and role definitions
  targets.json     Package index — one entry per .dpk file
  snapshot.json    Hash of targets.json
  timestamp.json   Hash of snapshot.json (must be refreshed weekly)
  packages/        The .dpk archives
  keys/
    root.key       Private signing key — keep this secret!
```

Clients download files in the order: `root.json` → `timestamp.json` →
`snapshot.json` → `targets.json` → `packages/<name>.dpk`.  
Every step is verified before the next is fetched.

---

## Step 1 — Initialise the repository

```bash
dpkbuild repo init my-repo
```

This generates a fresh `ed25519` keypair, creates all four TUF metadata files,
and writes the private key to `my-repo/keys/root.key`.

Example output:

```
✓ Initialised repository at /path/to/my-repo
  Key ID: a1b2c3d4...
  Private key: my-repo/keys/root.key
  ⚠  Keep keys/root.key secret — it is used to sign all repository metadata.

Serve the directory over HTTP and add it with:
  dimsim repo add <name> http://<host>/<path>
```

> **Security**: `root.key` contains the private signing key. Back it up securely
> and do not commit it to version control or make it accessible over HTTP.

---

## Step 2 — Add packages

Build your packages with `dpkbuild build` (see [Creating a Package](./creating-a-package.md)),
then add each `.dpk` to the repository:

```bash
dpkbuild repo add-package my-repo hello-1.0.0-amd64.dpk
```

This:

1. Reads the package manifest to extract name, version, arch, description, and
   dependencies.
2. Computes the SHA-256 hash and byte size of the `.dpk`.
3. Copies the `.dpk` to `my-repo/packages/`.
4. Updates `targets.json` with a new entry for the package.
5. Re-signs `snapshot.json` and `timestamp.json`.

Example output:

```
✓ Added hello-1.0.0-amd64.dpk to repository
  targets.json version: 2
```

Repeat for every package you want to publish.

---

## Step 3 — Serve the repository over HTTP

Any static HTTP server works. The server only needs to serve files from the
repository directory — no special software required.

**Example with Python** (development only):

```bash
cd my-repo
python3 -m http.server 8080
```

**Example with nginx** (production):

```nginx
server {
    listen 80;
    server_name packages.blueyos.example;
    root /srv/dimsim-repo;
    autoindex off;
    location / {
        try_files $uri =404;
    }
}
```

---

## Step 4 — Add the repository to dimsim

On the BlueyOS target machine, run as root:

```bash
dimsim repo add main http://packages.blueyos.example
```

dimsim downloads and trusts `root.json` from the URL. From that point forward,
all metadata and package downloads are verified against the key stored in
`root.json`.

Verify the repository was added:

```bash
dimsim repo list
```

```
NAME                 ENABLED  PRIORITY URL
----                 -------  -------- ---
main                 yes      100      http://packages.blueyos.example
```

---

## Step 5 — Fetch the package index

```bash
dimsim update
```

This runs the full TUF update sequence for every configured repository:

1. Download `timestamp.json` — verify signature and expiry.
2. Check if `snapshot.json` needs updating (via timestamp's hash).
3. Check if `targets.json` needs updating (via snapshot's hash).
4. Cache the current metadata in `/var/lib/dimsim/state.db`.

---

## Maintaining the repository

### Adding a new package version

```bash
dpkbuild repo add-package my-repo hello-1.1.0-amd64.dpk
```

Both old and new versions remain in the repository. Clients that run
`dimsim upgrade` will pick up the new version automatically.

### Keeping timestamp.json fresh

`timestamp.json` expires after **7 days**. Run this at least weekly (e.g. from a
cron job) to prevent clients from seeing an expiry error:

```bash
dpkbuild repo refresh my-repo
```

This re-signs `snapshot.json` and `timestamp.json` with fresh expiry dates
without modifying `targets.json` or the package list.

**Suggested cron entry** (runs every Sunday at 02:00):

```cron
0 2 * * 0  dpkbuild repo refresh /srv/dimsim-repo
```

### Removing a package

There is no automated remove command — simply delete the target entry from
`targets.json` by hand, then run `dpkbuild repo refresh my-repo` to re-sign.
The `.dpk` file under `packages/` can be deleted once no clients have it
installed.

---

## Security model

| Role | Signed by | Expires | Purpose |
|------|-----------|---------|---------|
| `root` | root key | 1 year | Public keys and role definitions |
| `targets` | root key | 1 year | Package hashes and metadata |
| `snapshot` | root key | 30 days | Hash of targets.json |
| `timestamp` | root key | 7 days | Hash of snapshot.json |

All four roles share a single key in this implementation. In production you may
rotate keys by issuing a new `root.json` that references new key IDs —
the old `root.json` (trusted by existing clients) must sign the new one to
establish the chain of trust.

---

## Troubleshooting

| Symptom | Cause | Fix |
|---------|-------|-----|
| `timestamp metadata expired` | `timestamp.json` is more than 7 days old | Run `dpkbuild repo refresh` |
| `insufficient valid signatures` | Wrong key or corrupted metadata | Ensure `keys/root.key` matches `root.json` |
| `package not found` | `dimsim update` not run after adding packages | Run `dimsim update` |
| `hash mismatch` | `.dpk` in `packages/` was modified after publishing | Re-add the package with `dpkbuild repo add-package` |
