---
id: versioning
title: Versioning
sidebar_position: 7
---

# Versioning

Three things have versions. They are distinct and move on different cadences.

| Thing | Scheme | Example | Moves when |
|---|---|---|---|
| Launcher | semver | `v0.1.43` | Launcher code changes |
| Bundle | calver | `2026.04.1` | `bundle.yaml` / `bundle.lock.json` changes |
| Components | upstream | `v1.33.1+rke2r1`, `v3.17.3` | Upstream releases |

And one thing has a *schema* version, which is separate from all of the above:

| Thing | Current | Moves when |
|---|---|---|
| `manifest.json` schema | `1` | Breaking manifest shape changes (rare) |

## Launcher — semver

```
MAJOR.MINOR.PATCH
v0.1.43
```

- **MAJOR** — bumped on a breaking launcher interface change (flags, state
  schema, manifest schema). Stays `0` through the Alpha line.
- **MINOR** — bumped on a new feature or significant behavior change.
  Roughly, "the docs changed substantially."
- **PATCH** — bumped on bug fixes and small internal changes.

Launcher versions are **git-tagged**. The tag drives GoReleaser, which in
turn drives the release workflow. Tagging is the only way a launcher release
gets cut.

`-dirty` / `-N-gSHA` suffixes appear on non-tagged builds; they come straight
from `git describe` and indicate "this was built from a non-tagged
commit."

## Bundle — calver

```
YYYY.MM.N
2026.04.1
```

- **YYYY.MM** — the year and month of the release.
- **N** — an integer, starting at `1`, incremented for each bundle released
  in that month.

Bundle versions live in `bundle.yaml`'s `bundle_version:` field and are
written into `manifest.json`. **They are not git tags** — the bundle version
is a label attached to the bundle, not to the repo state.

In practice, the bundle version is bumped in the same commit that updates
`bundle.yaml` or `bundle.lock.json`, which is usually the same commit that
gets a launcher tag. But the two can move independently if needed:

- Launcher-only bug fix → launcher tag moves, bundle stays on the same
  version.
- Lockfile refresh with no launcher change → bundle version moves (manually
  bumped), new bundle ships with the existing launcher.

The in-tree spec uses `0.0.0-dev` as a placeholder during development — the
release workflow replaces this automatically at tag time.

## Component versions — upstream

Components are labeled with whatever the upstream project uses:

- RKE2 — `v1.33.1+rke2r1` (Kubernetes version + RKE2 revision).
- Helm — `v3.17.3`.
- aether-ops — `v0.1.43` (aether-ops' own semver, independent of this
  project's).

These are recorded in `manifest.json` exactly as upstream names them. The
bootstrap never invents component versions or normalizes them.

## Manifest schema version

A single integer in `manifest.json`:

```json
{ "schema_version": 1, ... }
```

The launcher compares this to its hardcoded `SchemaVersion` constant on
every read. A mismatch aborts preflight. The schema version moves only on
**breaking** shape changes — additive changes stay on the same version.

Same principle applies to `state.json` (`schema_version` in
`internal/state`) and `bundle.lock.json`.

## Which version should I bump?

| Change | Launcher | Bundle | Manifest schema |
|---|---|---|---|
| Fix a bug in the RKE2 component | ✓ PATCH | | |
| Add a new launcher subcommand | ✓ MINOR | | |
| Bump RKE2 to v1.34.0 | | ✓ new calver | |
| Add a new `.deb` to the bundle | | ✓ new calver | |
| Update the docs site only | | | |
| Add a new field to manifest (additive) | ✓ MINOR | ✓ new calver | |
| Rename a field in manifest | ✓ MAJOR | ✓ new calver | ✓ bump |
| Bump the state file schema | ✓ MINOR/MAJOR | | |

(Docs-only changes deploy via `docs.yml` without a tag.)

## Why this split

A monolithic version would be simpler to remember and harder to get wrong,
but it would couple two cadences that don't actually align. RKE2 bumps
happen on RKE2's cadence; launcher bug fixes happen on ours. Pretending they
share a schedule creates either:

- fake releases (a new launcher tag just to ship a lockfile refresh), or
- suppressed releases (a real RKE2 bump that can't ship because the launcher
  hasn't changed).

Separating the two means each can move when it's actually ready.

The same logic argues for keeping the **manifest schema** separate from
either of them: the manifest shape rarely changes, and when it does, neither
a launcher tag nor a bundle version is the right place to signal it.
