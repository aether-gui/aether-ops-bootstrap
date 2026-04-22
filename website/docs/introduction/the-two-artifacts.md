---
id: the-two-artifacts
title: The two artifacts
sidebar_position: 4
---

# The two artifacts

The most important architectural decision in this project is that **code and
data ship separately**. The launcher is code. The bundle is data. They are
versioned, built, released, and reasoned about independently.

## `aether-ops-bootstrap` — the launcher

A statically linked Go binary. Single file. 10 – 30 MB. Contains every bit
of logic the project has: preflight checks, archive extraction, `.deb`
handling, systemd unit writing, RKE2 install, aether-ops install, state
management, reconciliation, diagnostics collection.

Key properties:

- **`CGO_ENABLED=0`.** No dynamic dependencies; runs on any glibc, any musl.
- **Self-auditable.** `--help`, `version`, `state`, and `check` all work
  without a bundle present.
- **Versioned with semver.** `v0.1.43` means the first public alpha line.
  Breaking launcher changes bump the minor; patch bumps are bug fixes.
- **Git-tagged.** Each release tag triggers GoReleaser, which produces the
  signed binary.

## `bundle.tar.zst` — the offline payload

An opaque tarball compressed with zstd. The launcher extracts it at install
time; nothing else ever reads it directly.

Layout inside the tarball:

```
manifest.json                  # the contract — hashes, versions, sources
debs/
  ansible_*.deb
  git_*.deb
  make_*.deb
  ... (transitive deps)
rke2/
  rke2.linux-<arch>.tar.gz
  rke2-images-<variant>.linux-<arch>.tar.zst
  sha256sum-<arch>.txt
helm/
  helm-v3.17.3-linux-<arch>.tar.gz
aether-ops/
  aether-ops
  aether-ops.service
  config.yaml.tmpl
templates/
  sshd_config.d/
  sudoers.d/
  rke2-config.yaml.tmpl
```

Key properties:

- **Versioned with calver.** `2026.04.1` means "the first bundle of April
  2026" — a *snapshot of the world at a point in time*.
- **Described by a manifest.** `manifest.json` records the component versions,
  file paths, and hashes the launcher needs after extraction.
- **Checksummed as an archive.** The builder emits a `.sha256` sidecar for the
  final `bundle.tar.zst`.

## `manifest.json` — the contract

The manifest is the *only* thing both sides care about. The launcher reads
it to learn what the bundle contains; the builder writes it to declare what
it built. A minimal shape:

```json
{
  "schema_version": 1,
  "bundle_version": "2026.04.1",
  "bundle_sha256": "",
  "build_info": {
    "go_version": "go1.22.x",
    "git_sha": "abc1234",
    "builder": "build-bundle",
    "timestamp": "2026-04-18T14:22:03Z"
  },
  "components": {
    "debs": [{ "name": "ansible", "version": "...", "sha256": "..." }],
    "rke2": { "version": "v1.33.1+rke2r1", "artifacts": [] },
    "helm": { "version": "v3.17.3", "files": [] },
    "aether_ops": { "version": "v0.1.43", "files": [] }
  }
}
```

`internal/bundle` in the repo defines the Go types for this schema. Both
`cmd/aether-ops-bootstrap` (launcher) and `cmd/build-bundle` (builder) import
those types, so it is physically impossible for the two sides to disagree on
the shape of the manifest without the compiler complaining.

Schema changes are a single PR:

1. Update the Go types in `internal/bundle`.
2. Bump `SchemaVersion`.
3. Update reader code on the launcher side to accept the old schema when
   possible.
4. Ship both new launcher and new bundle on the next release.

The launcher **refuses** to read a bundle with a `schema_version` it doesn't
recognize. Mismatched pairs fail loudly at preflight instead of silently
producing broken installs.

## Why the split

Shipping code and data together would simplify two things:

1. Versioning — one number to track.
2. Distribution — one file to download.

But it would complicate several others, in exchange:

- **Rebuilding code to bump RKE2 is silly.** RKE2 pins change faster than
  launcher logic. Code and data cycling on the same tag would mean a new
  launcher binary every time an upstream `.deb` moved.
- **Auditing would be harder.** Right now you can hash the manifest and get
  a signed statement about what's in a release. A monolithic artifact is
  opaque.
- **Airgap distribution is easier with one data blob.** The bundle is the
  only thing that needs curation by the security team; the launcher is
  source-available code that auditors can reason about on its own.
- **Per-site bundles become possible.** A customer can build their own
  bundle with locally mirrored `.debs` without changing the launcher.

## Pairing rules of thumb

- A **newer bundle** is generally usable with an **older launcher** as long
  as `schema_version` hasn't moved. The launcher just gets fresher upstream
  pins.
- A **newer launcher** is generally usable with an **older bundle** as long
  as the bundle still contains everything the launcher needs. Preflight
  fails fast if it doesn't.
- The integration test matrix
  ([CI — integration](/build-guide/release-process#ci-workflows)) exercises
  these combinations explicitly.
