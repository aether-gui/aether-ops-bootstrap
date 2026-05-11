# Deploy the distribution site

How to publish a new bootstrap + bundle to the public download page at
`https://tools.jointpathfinding.com/aether-ops-bootstrap/`.

The site lives in the `www` container on LXD remote `datacenter`
(host `services-01.dc.lan`), under
`/var/www/tools.jointpathfinding.com/aether-ops-bootstrap/`.

> **Prefer the automated path.** `.github/workflows/release.yml`
> rebuilds, rotates `site/releases.yaml`, publishes to the www
> container, and opens a release PR — all on the self-hosted
> runner that lives inside the container. Trigger it via
> *Actions → release → Run workflow* on GitHub. The procedure below
> covers the manual fallback when the workflow can't run (runner
> offline, network issue, mid-development pre-flight, etc.). See
> [`setup-www-runner.md`](setup-www-runner.md) for the one-time
> runner registration.

## Layout (on the www container)

```
aether-ops-bootstrap/
  index.html                       # latest release page
  metadata.json                    # public JSON metadata of all releases
  releases/index.html              # release history page
  bootstrap/<version>/aether-ops-bootstrap{,.sha256}
  bundles/<version>/bundle.tar.zst{,.sha256}
```

Each release has its own `<version>` subdir under `bootstrap/` and `bundles/`.
Older release artifacts are preserved.

## Source of truth

`site/releases.yaml` is the source of truth for the site. It is consumed by
`cmd/build-release-site` (binary: `dist/build-release-site`), which:

- copies `source:` files into `dist/release-site/{bootstrap,bundles}/<path>/`,
- computes SHA256s (and verifies them against `sha256:` / `sha256_source:` if set),
- writes `dist/release-site/{index.html, releases/index.html, metadata.json}`,
- removes `dist/release-site/` first, so it only contains the *current* and
  any other non-`external` releases.

`external: true` releases are listed in `metadata.json` and `releases/index.html`
but no artifacts are copied — the SHA256 must be set in the YAML.

## Procedure

Today's date: use `YYYY.MM.DD.N` for `version`/`path` (e.g. `2026.04.29.1`).
Bump `.N` if you publish multiple times the same day.

### 1. Demote the previous current release to external

The build wipes `dist/release-site/` and re-copies `source:` files. The
previous release's `source: ../dist/aether-ops-bootstrap` and
`../dist/bundle.tar.zst` now point at the *new* artifacts, so leaving the
old release as `source:`-based would silently overwrite its files with
new content under the *old* path.

Convert the previous current release to `external: true` and inline its
existing `sha256:` values from the live `metadata.json` (or capture them
before overwriting `dist/`):

```bash
# Pull current metadata.json before rebuilding to grab old SHA256s if
# you did not save them.
/snap/bin/lxc exec datacenter:www -- cat \
  /var/www/tools.jointpathfinding.com/aether-ops-bootstrap/metadata.json
```

For an external release entry, replace `source:` / `sha256_source:` with a
literal `sha256:` field on both the bootstrap and bundle blocks, and add
`external: true` at the release level.

### 2. Add the new release entry at the top of `releases:`

Set `current: true` (and ensure no other release has it). Set `commit:` /
`build_commit:` to `git rev-parse --short HEAD`. Fill in the `components:`
list from `specs/bundle.yaml` (`aether_ops.version`, `onramp.ref`,
`rke2.version`, `helm.version`, `helm_charts[*]`).

### 3. Build the site

```bash
make build-release-site
./dist/build-release-site --metadata site/releases.yaml --output dist/release-site
```

Quick sanity check:

```bash
grep -E '"version"|"sha256"|"current"' dist/release-site/metadata.json | head
```

The `sha256` for the new bootstrap and bundle should match
`sha256sum dist/aether-ops-bootstrap` and the contents of
`dist/bundle.tar.zst.sha256`.

### 4. Push to the www container

`www-data` is uid/gid 33 inside the container. Use `--uid 33 --gid 33` so
nginx can read the files.

```bash
V=2026.04.29.1   # whatever was set in releases.yaml
DEST=datacenter:www/var/www/tools.jointpathfinding.com/aether-ops-bootstrap

# Make destination dirs (lxc file push does not auto-create parents
# reliably for files; pre-creating is more predictable).
/snap/bin/lxc exec datacenter:www -- bash -c \
  "mkdir -p /var/www/tools.jointpathfinding.com/aether-ops-bootstrap/bootstrap/$V \
            /var/www/tools.jointpathfinding.com/aether-ops-bootstrap/bundles/$V && \
   chown -R www-data:www-data /var/www/tools.jointpathfinding.com/aether-ops-bootstrap/bootstrap/$V \
                              /var/www/tools.jointpathfinding.com/aether-ops-bootstrap/bundles/$V"

# Push the small files first.
for f in \
  bootstrap/$V/aether-ops-bootstrap \
  bootstrap/$V/aether-ops-bootstrap.sha256 \
  bundles/$V/bundle.tar.zst.sha256 \
  index.html \
  metadata.json \
  releases/index.html; do
  /snap/bin/lxc file push --uid 33 --gid 33 dist/release-site/$f $DEST/$f
done

# Push the bundle (~2.6 GB) — slow over the LXD HTTPS API.
/snap/bin/lxc file push --uid 33 --gid 33 \
  dist/release-site/bundles/$V/bundle.tar.zst \
  $DEST/bundles/$V/bundle.tar.zst
```

For the large bundle, run the push in the background; it can take many
minutes over the LXD API.

### 5. Verify

```bash
/snap/bin/lxc exec datacenter:www -- bash -c "
  cd /var/www/tools.jointpathfinding.com/aether-ops-bootstrap
  ls -la bootstrap/$V bundles/$V
  sha256sum -c bundles/$V/bundle.tar.zst.sha256 || true
  sha256sum -c bootstrap/$V/aether-ops-bootstrap.sha256 || true
  jq '.releases[] | select(.current==true) | {version: .bundle.version, sha: .bundle.sha256}' metadata.json
"
```

The `current` release in `metadata.json` should match the new version,
and both SHA256 verifications should pass.

## Notes / gotchas

- `lxc file push` uses the LXD HTTPS API, which is slow for multi-GB
  files. If the upload becomes painful, `scp` directly to
  `services-01.dc.lan` and then push into the container with
  `lxc file push <local-on-host> <ctr>/<path>` from there.
- The container's nginx serves files as `www-data:www-data` (uid/gid 33).
  Files pushed without `--uid/--gid` end up owned by root and may be
  unreadable depending on perms; always pass `--uid 33 --gid 33`.
- `dist/release-site/` is wiped on each build, so do not stash anything
  you care about there.
- `Makefile`'s `release-site` target uses `site/releases.example.yaml`
  for testing. Real publishes use `site/releases.yaml` — invoke the
  binary directly with `--metadata site/releases.yaml`.
- Today's date should match the date stamp in `version`/`published_at`.
  When running on a different day from the build, prefer the build date.
