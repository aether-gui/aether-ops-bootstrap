---
id: release-process
title: Release process
sidebar_position: 6
---

# Release process

What happens when a `v*` git tag is pushed. Useful if you're cutting a
release, debugging a failed release, or adding a new check to the pipeline.

## CI workflows

Three workflows in `.github/workflows/`:

| Workflow | Trigger | Purpose |
|---|---|---|
| `launcher.yml` | push / PR to `main` | Vet, lint, test, build, vuln scan |
| `release.yaml` | `v*` tag push | GoReleaser + bundle build + SBOMs + scans + upload |
| `docs.yml` | push to `main`, `v*` tag, PR touching `website/` | Build + publish the docs site |

### `launcher.yml` — main / PR

Runs on every push and pull request to `main`:

- `go vet ./...`
- `golangci-lint run ./...`
- `go test -race -cover ./...`
- `make build`
- `grype dir:.` — source-tree vulnerability scan (report uploaded as
  artifact, not gated on severity).

This is the PR gate; failing here blocks merging.

### `release.yaml` — tag push

Triggered on `push: tags: ['v*']` or `workflow_dispatch`. Runs on a
self-hosted runner labeled `bundle-dist`. Steps:

1. **Checkout** with `fetch-depth: 0` (GoReleaser needs full history for
   changelog generation).
2. **Setup Go 1.22**.
3. **GoReleaser** — `goreleaser release --clean`. Builds the launcher
   binary via `.goreleaser.yaml`, creates the GitHub release, uploads the
   launcher archive.
4. **Prepare bundle spec for CI** — strips any local `source:` entries from
   `specs/bundle.yaml` so the builder fetches aether-ops from its GitHub release
   instead of from a local artifacts directory that doesn't exist in CI.
5. **Build bundle** — `go run ./cmd/build-bundle --spec specs/bundle.yaml
   --output dist/bundle.tar.zst`. Produces the offline payload.
6. **Install Syft** — SBOM generator from Anchore.
7. **Install Grype** — vulnerability scanner from Anchore.
8. **Locate launcher binary** — finds the GoReleaser output under `dist/`.
9. **Generate launcher SBOM** — `syft … -o spdx-json`.
10. **Generate bundle SBOM** — `syft dist/bundle.tar.zst -o spdx-json`.
11. **Scan launcher SBOM with Grype** — emits both JSON and a human-readable
    table.
12. **Scan bundle SBOM with Grype** — same.
13. **Upload bundle + scans to release** — `gh release upload` for
    `bundle.tar.zst`, `bundle.tar.zst.sha256`, both SBOMs, both Grype JSON
    reports.

The final GitHub release has:

- `aether-ops-bootstrap_<os>_<arch>.tar.gz` (from GoReleaser)
- `bundle.tar.zst`
- `bundle.tar.zst.sha256`
- `sbom-aether-ops-bootstrap-<tag>.spdx.json`
- `sbom-bundle-<tag>.spdx.json`
- `grype-launcher.json`
- `grype-bundle.json`

### `docs.yml` — docs site

Builds and publishes this site to GitHub Pages.

Triggers:

- Push to `main` touching `website/` or the workflow file.
- Push of a `v*` tag (rebuilds the site to match the new release).
- PRs touching `website/` (build-only — doesn't deploy, but catches broken
  links).

Concurrency: `group: pages, cancel-in-progress: false`. The main and tag
pushes serialize against each other so two deploys can't race.

## Cutting a release — checklist

1. **`main` is green.** Launcher workflow passing.
2. **Lockfile diff, if any, is reviewed.** See [lockfile](./lockfile.md).
3. **Decide version numbers.**
   - Launcher semver — see [versioning](./versioning.md).
   - Bundle calver — typically advances with the launcher but can move
     independently.
4. **Tag and push:**
   ```bash
   git tag v0.1.44
   git push origin v0.1.44
   ```
5. **Watch the Release workflow.** If anything fails, the release may be
   partially created. Delete the GitHub release + tag and re-push.
6. **Verify artifacts.** Download the tagged bundle and its `.sha256` on a
   test box; run `sha256sum -c`.
7. **Verify on a VM.** Run `install` against a clean Ubuntu VM as a final
   sanity check.

## Why the self-hosted runner?

`release.yaml` runs on `[self-hosted, bundle-dist]` rather than
`ubuntu-latest`. The bundle build can be large and slow; hosted runners can
time out or run into disk pressure. The `bundle-dist`-labeled runner has:

- More disk for the image extraction + tarball assembly,
- Access to any internal mirrors if needed,
- A warm content-addressed cache across builds.

For a fresh fork / community contribution, the workflow needs to be
re-targeted at `ubuntu-latest` with the understanding that release timing
may be less predictable.

## Signing (roadmap)

Bundle signing (GPG or cosign) is an open design question — see the Open
Questions section of `DESIGN.md` in the repository. The 0.1.x line ships
unsigned bundles, secured only by SHA256 integrity. A future release will
add a signing step to `release.yaml` and a verification step to the
launcher's preflight.
