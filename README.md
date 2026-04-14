# aether-ops-bootstrap

`aether-ops-bootstrap` takes a freshly installed Ubuntu Server host and produces a running aether-ops management plane on top of RKE2. It runs once per management node, requires no internet access, and hands off to aether-ops for all further configuration.

Two artifacts are produced: a statically linked Go launcher binary and an offline payload bundle (`aether-ops-bundle-<version>-linux-amd64.tar.zst`). Together they bring up the platform layer — RKE2, aether-ops, Helm, and all OS-level prerequisites — without touching the network.

See [DESIGN.md](DESIGN.md) for full architecture and design details.

## Building

### Prerequisites

- Go 1.22+
- Node.js 20+ and npm (only if building aether-ops from source)
- golangci-lint (optional, for linting: `make install-lint`)

### Build the launcher

```bash
make build
```

Produces `dist/aether-ops-bootstrap` — the static binary that runs on target hosts.

### Build the bundle tool

```bash
make build-bundle
```

Produces `dist/build-bundle` — the tool that assembles offline bundles from `bundle.yaml`.

### Build a bundle

```bash
./dist/build-bundle --spec bundle.yaml --output dist/bundle.tar.zst
```

This reads `bundle.yaml` and:

1. **Fetches RKE2** airgap artifacts (binary + container images) from GitHub releases, verifies SHA256 checksums
2. **Fetches Helm** binary from get.helm.sh, verifies SHA256
3. **Acquires aether-ops** binary (from source, GitHub release, or local path depending on spec)
4. **Resolves .deb dependencies** from Ubuntu Packages indexes (main + universe), downloads and SHA256-verifies each package
5. **Stages templates** (RKE2 config, SSH drop-ins, sudoers)
6. **Generates `manifest.json`** recording every artifact with version, hash, and size
7. **Archives** everything into a `tar.zst` bundle with a `.sha256` sidecar

The tool also writes/verifies `bundle.lock.json` to detect upstream dependency drift between builds.

#### Multi-bundle mode

Pass a directory of `.yaml` spec files to build multiple bundles:

```bash
./dist/build-bundle --spec specs/ --output dist/
```

### Bundle spec (`bundle.yaml`)

The spec declares what goes into a bundle:

```yaml
schema_version: 1
bundle_version: "2026.04.1"

ubuntu:
  suites: [noble]
  architectures: [amd64]
  # mirror: https://archive.ubuntu.com/ubuntu  # override for internal mirrors

debs:
  - name: ansible
  - name: git
  - name: make
  - name: curl
  - name: jq
  - name: ssh
  - name: sshpass
  - name: iptables

rke2:
  version: "v1.33.1+rke2r1"
  variants: [canal]
  image_mode: all-in-one

helm:
  version: "v3.17.3"

aether_ops:
  version: "v1.0.0"              # download pre-built from GitHub releases
  # ref: "main"                  # OR build from source at this git ref
  # source: ./build/aether-ops   # OR use a local pre-built binary

templates_dir: ./templates
```

## CI/CD Workflows

Two GitHub Actions workflows:

### `launcher.yml` — on push/PR to main

Runs on every push and pull request:
- `go vet ./...`
- `golangci-lint`
- `go test -race -cover ./...`
- `make build`

### `release.yaml` — on tag push (`v*`)

Triggered when a version tag is pushed. Builds the bundle and publishes release artifacts.

## End-to-end tests

VM-based tests under `tests/` use [DART](https://github.com/bgrewell/dart) to spin up LXD VMs, push freshly built artifacts, and exercise the full airgap install flow. Four suites:

- `tests/single-node-bootstrap` — bootstrap only, single node
- `tests/single-node-deploy` — bootstrap + SD-Core deployment, single node
- `tests/multi-node-bootstrap` — bootstrap only, three roles
- `tests/multi-node-deploy` — bootstrap + SD-Core deployment, three roles

Each suite's `setup/00_build-artifacts.yaml` runs `make build bundle` on the host before pushing, so artifacts are always rebuilt from the current tree. Run via the Makefile:

```bash
make test-e2e-bootstrap        # single-node bootstrap (~10-15 min)
make test-e2e-multi-bootstrap  # multi-node bootstrap
make test-e2e-deploy           # single-node full deploy (~30-45 min)
make test-e2e-multi-deploy     # multi-node full deploy
make test-e2e-quick            # both bootstrap suites
make test-e2e                  # all four suites
```

Requires the `dart` CLI and an LXD installation with a `default` storage pool.

## Development

```bash
make test          # run tests with race detector and coverage
make vet           # go vet
make lint          # golangci-lint (install with: make install-lint)
make build-all     # build both launcher and bundle tool
make clean         # remove build artifacts
```
