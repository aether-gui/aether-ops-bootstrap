---
id: building-locally
title: Building locally
sidebar_position: 5
---

# Building locally

How to build the launcher, the bundle builder, and an offline bundle from a
checked-out tree.

## Prerequisites

- **Go 1.22+** — the project pins 1.22 in CI; newer works.
- **Node.js 20+ and npm** — only needed if you're building aether-ops from
  source (i.e. using `ref:` / `repo:` under `aether_ops:` in `bundle.yaml`).
- **`golangci-lint`** — optional, for linting. Install via
  `make install-lint` if you don't have it.
- **Network access** — the build machine needs to reach Ubuntu mirrors,
  GitHub, and `get.helm.sh`. The *target* host doesn't; the *builder* does.

## The five Makefile targets you'll use most

```bash
make build              # → dist/aether-ops-bootstrap (launcher)
make build-bundle       # → dist/build-bundle (builder tool)
make bundle             # → dist/bundle.tar.zst + .sha256
make package            # → dist/aether-ops-bootstrap-<version>.tar.gz
make build-all          # both binaries, no bundle
```

## Step-by-step

### Build the launcher

```bash
make build
```

Produces `dist/aether-ops-bootstrap`, a static Linux amd64 binary built with
`CGO_ENABLED=0 -trimpath`, with the version injected via `-ldflags`.

The version string comes from:

```
$(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
```

So a clean tagged build reads `v0.1.43`; a clean `main` build reads something
like `v0.1.43-2-gabc1234`; an uncommitted edit reads `v0.1.43-2-gabc1234-dirty`.

### Build the bundle builder

```bash
make build-bundle
```

Produces `dist/build-bundle`. You usually don't run this directly — `make
bundle` depends on it.

### Build a bundle from `bundle.yaml`

```bash
make bundle
```

Internally:

```bash
./dist/build-bundle --spec bundle.yaml --output dist/bundle.tar.zst
```

This reads `bundle.yaml` and:

1. Resolves `.deb` transitive dependencies from Ubuntu Packages indexes.
2. Downloads RKE2 airgap artifacts, verifies SHA256 against the release's
   checksum file.
3. Downloads Helm from `get.helm.sh`, verifies SHA256.
4. Acquires aether-ops (from the local file, GitHub release, or source
   build, depending on the spec).
5. Writes or verifies `bundle.lock.json` (see
   [lockfile](./lockfile.md)).
6. Stages templates.
7. Generates `manifest.json`.
8. Archives everything into `tar.zst`, writes a `.sha256` sidecar.

First build on a fresh clone takes several minutes (most of that is
downloading). Subsequent builds — with the lockfile in place and the
content-addressed cache warm — are much faster.

### Package everything

```bash
make package
```

Produces `dist/aether-ops-bootstrap-<version>.tar.gz` containing the
launcher, the bundle, and the `.sha256`. This is the single artifact CI
attaches to GitHub releases alongside the individual files.

## Multi-bundle mode

The builder also accepts a directory of specs instead of a single file:

```bash
./dist/build-bundle --spec specs/ --output dist/
```

Every `.yaml` file in `specs/` is built into its own bundle named from the
spec's `bundle_version`. This is the foundation for per-role bundles (see
[roadmap](/bootstrap-guide/roadmap)) but is usable today for any case where
you want parallel bundle variants.

## Local aether-ops override

When iterating on aether-ops itself, point the spec at a local build:

```yaml
aether_ops:
  version: "v0.1.43-dev"
  source: ./artifacts/aether-ops_0.1.43-dev_linux_amd64.tar.gz
```

The CI release workflow strips `source:` lines that reference local paths
before running its own build, so this kind of spec is safe to commit for
use in development and is automatically neutralized at release time.

## Lint, vet, test

```bash
make vet          # go vet ./...
make lint         # golangci-lint run ./...
make test         # go test -race -cover ./...
```

All three run on every push and PR in `.github/workflows/launcher.yml`.

## End-to-end tests

Four DART-driven LXD VM suites exercise the full airgap install flow:

```bash
make test-e2e-bootstrap        # single-node bootstrap (~10-15 min)
make test-e2e-multi-bootstrap  # multi-node bootstrap
make test-e2e-deploy           # single-node full deploy (~30-45 min)
make test-e2e-multi-deploy     # multi-node full deploy
make test-e2e-quick            # both bootstrap suites
make test-e2e                  # all four suites
```

Requires the [DART CLI](https://github.com/bgrewell/dart) and an LXD install
with a `default` storage pool. Each suite's `setup/00_build-artifacts.yaml`
calls `make build bundle` on the host before pushing artifacts into the VM,
so e2e always tests the current tree.

## `dist/` layout after a full build

```
dist/
├── aether-ops-bootstrap                  # the launcher
├── build-bundle                          # the builder tool
├── bundle.tar.zst                        # the offline payload
├── bundle.tar.zst.sha256                 # integrity sidecar
└── aether-ops-bootstrap-<version>.tar.gz # everything packaged
```

`make clean` removes the whole `dist/` tree.
