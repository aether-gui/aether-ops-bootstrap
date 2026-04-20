---
id: manifest
title: manifest.json
sidebar_position: 4
---

# `manifest.json`

`manifest.json` lives **inside** the bundle. It's the contract between the
builder (which writes it) and the launcher (which reads it at install time).

If `bundle.yaml` is the *input* describing what to build, `manifest.json` is
the *output* describing what was actually assembled.

## Where it comes from

Written by `cmd/build-bundle` at the end of a successful build. Packed into
the tarball as the top-level entry:

```
bundle.tar.zst
├── manifest.json          ← this file
├── debs/
├── rke2/
├── helm/
├── aether-ops/
└── templates/
```

## Where it goes

Read by the launcher at the start of every `install`, `upgrade`, `repair`,
and `check` run. The launcher:

1. Extracts the tarball.
2. Parses `manifest.json`.
3. Compares `schema_version` to the launcher's hardcoded supported version.
4. Uses the manifest to drive the component loop — `DesiredVersion()` on
   each component returns a value pulled from the manifest.

## Why it exists (and why it's separate from `bundle.yaml`)

`bundle.yaml` contains *intent* ("give me RKE2 v1.33.1"). `manifest.json`
contains *reality* ("here are the exact files packed, with hashes and source
URLs"). Two reasons this matters:

1. **The launcher never sees `bundle.yaml`.** The bundle ships without it.
   The launcher relies on the manifest to know what's inside.
2. **Intent and reality can diverge** — `bundle.yaml` says "ansible";
   `manifest.json` says "ansible 2.14.16-1 with these 38 transitive deps."
   The builder's job is to produce a complete manifest; the launcher's job
   is to apply it faithfully.

## The shared Go types

`internal/bundle` defines Go types for the manifest schema. **Both the
launcher and the builder import those types.** A schema change is a single
PR that touches both sides, and the compiler refuses to let them drift.

```go
// internal/bundle (illustrative — see the package for exact types)
type Manifest struct {
    SchemaVersion int                     `json:"schema_version"`
    BundleVersion string                  `json:"bundle_version"`
    BundleHash    string                  `json:"bundle_hash"`
    CreatedAt     time.Time               `json:"created_at"`
    Components    map[string]Component    `json:"components"`
    Debs          []DebEntry              `json:"debs"`
    BuildInfo     BuildInfo               `json:"build_info"`
}
```

## Schema version handling

The launcher checks `schema_version` before reading anything else. A mismatch
aborts preflight with a clear error:

```
preflight: manifest schema_version 2 not supported by launcher (supports 1)
```

This is the same safety mechanism as `bundle.lock.json` and the state file —
refuse ambiguity at load time, not halfway through.

Compatibility rules:

- **Additive changes** (new optional fields) are fine within a major schema
  version.
- **Breaking changes** (removed fields, renamed fields, changed types) bump
  the schema version.
- **The launcher can support multiple schema versions** by branching on
  `schema_version` at parse time — this is how we intend to roll over
  without forcing lockstep upgrades.

## The bundle hash

The manifest records a `bundle_hash` — a content hash computed by the builder
over the staged tree *before* `manifest.json` itself is written. This lets
the launcher verify bundle integrity independently of the `.sha256` sidecar
the operator used at the filesystem level.

`build_info` (Go version, builder hostname, git SHA, timestamp) is recorded
in the manifest but **excluded** from the bundle hash. That keeps the hash
stable across rebuilds from the same spec + lockfile even if the builder
machine's hostname changes.

## What auditors should look for

The manifest is the primary auditor-facing artifact. A good review process:

1. **Parse `manifest.json`** out of the tarball (`tar -xOf bundle.tar.zst manifest.json`).
2. **Cross-check every `sha256`** against the upstream authoritative source
   — Ubuntu's Packages index, GitHub release checksums, `get.helm.sh`.
3. **Cross-check every `source` URL** against your organization's allowlist
   of upstreams.
4. **Diff against the previous release's manifest** to see exactly what
   changed — the same information that appears in the changelog.

The `BUNDLE_CONTENTS.md` generated doc (see
[release process](./release-process.md)) is a rendered, human-readable view
of the same data for people who don't want to parse JSON.
