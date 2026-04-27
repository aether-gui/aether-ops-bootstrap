---
id: release-site
title: Download site generator
sidebar_position: 8
---

# Download site generator

`cmd/build-release-site` produces the static download page and versioned
artifact tree served from GitHub Pages. It reads a release-metadata YAML,
copies (and SHA256-verifies) the launcher and bundle artifacts into a
`bootstrap/<path>/` and `bundles/<path>/` layout, and renders an
`index.html` for the current release plus a `releases/index.html` listing
older entries.

If `cmd/build-bundle` produces the artifacts, `cmd/build-release-site`
publishes them.

## Build it

```bash
make build-release-site         # → dist/build-release-site
```

## Run it

```bash
./dist/build-release-site \
  --metadata site/releases.example.yaml \
  --output   dist/release-site
```

Or, with the example metadata:

```bash
make release-site
```

Both flags are required. The output directory is removed and recreated on
every run.

## What it generates

```
dist/release-site/
├── index.html              # current release landing page
├── releases/
│   └── index.html          # all releases, current first
├── metadata.json           # machine-readable manifest (schema_version 1)
├── bootstrap/
│   └── <path>/
│       └── aether-ops-bootstrap
└── bundles/
    └── <path>/
        ├── bundle.tar.zst
        └── bundle.tar.zst.sha256
```

`metadata.json` mirrors the input metadata but with public-facing fields
only (URLs resolved against `site.base_url_path`, computed SHA256s,
artifact paths). It's the contract for any consumer that wants to discover
releases without scraping HTML.

## Metadata schema

The input is a single YAML file. Schema version `1`:

```yaml
schema_version: 1

site:
  title: Aether Ops Bootstrap Downloads
  base_url_path: /aether-ops-bootstrap
  description: Public download page for the bootstrap launcher and offline bundle artifacts.

releases:
  - id: "2026-04-21-example"
    published_at: "2026-04-21"
    current: true                       # exactly one release should set this

    bootstrap:
      label: Bootstrap Launcher
      version: "0.1.43"
      path: "0.1.43"                    # subdirectory under bootstrap/
      filename: aether-ops-bootstrap
      source: ../dist/aether-ops-bootstrap
      commit: "341787b"
      release_notes: |
        Replace this with launcher-specific notes for the release.

    bundle:
      label: Offline Bundle
      version: "2026.04.1"
      path: "2026.04.1"                 # subdirectory under bundles/
      filename: bundle.tar.zst
      source: ../dist/bundle.tar.zst
      sha256_source: ../dist/bundle.tar.zst.sha256
      build_commit: "341787b"
      release_notes: |
        Replace this with bundle-specific notes for the release.
```

`source` paths are resolved relative to the metadata file's directory.
If `sha256_source` is provided, the value is verified against a freshly
computed hash of `source`; if omitted, the generator computes and emits
the hash itself.

The canonical example lives at
[`site/releases.example.yaml`](https://github.com/aether-gui/aether-ops-bootstrap/blob/main/site/releases.example.yaml).

## Typical workflow

1. `make package` — produce the launcher and bundle in `dist/`.
2. Update `site/releases.yaml` (your real, non-example metadata) with the
   new release block. Set `current: true` on it; clear `current` on the
   previous entry.
3. `./dist/build-release-site --metadata site/releases.yaml --output dist/release-site`.
4. Publish `dist/release-site/` (commit to the `gh-pages` branch, sync to
   a static host, etc.).

The generator is fully deterministic given the same inputs — re-running
it on the same metadata produces a byte-identical tree.
