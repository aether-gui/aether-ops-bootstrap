---
id: bundle-yaml-reference
title: bundle.yaml reference
sidebar_position: 2
---

# `bundle.yaml` reference

`bundle.yaml` is the single source of truth for what goes into a bundle.
Human-edited. The builder refuses to proceed without it.

## Minimal example

```yaml
schema_version: 1
bundle_version: "2026.04.1"

ubuntu:
  suites: [noble]
  architectures: [amd64]

debs:
  - name: ansible
  - name: git
  - name: make

rke2:
  version: "v1.33.1+rke2r1"
  variants: [canal]
  image_mode: all-in-one

helm:
  version: "v3.17.3"

aether_ops:
  version: "v0.1.43"
  source: ./artifacts/aether-ops_0.1.43_linux_amd64.tar.gz

templates_dir: ./templates
```

## Field reference

### Top-level

| Field | Required | Description |
|---|---|---|
| `schema_version` | yes | Spec schema version. Must be `1` for 0.1.x. |
| `bundle_version` | yes | Calver string (e.g. `2026.04.1`). Written into the manifest. |
| `ubuntu` | yes | Ubuntu suite/arch targets for `.deb` resolution. |
| `debs` | yes | Top-level `.deb` packages to vendor. Transitive deps resolved automatically. |
| `rke2` | yes | RKE2 version and image variants to fetch. |
| `helm` | yes | Helm version to fetch. |
| `aether_ops` | yes | aether-ops source (local file, URL, or git ref). |
| `onramp` | no | aether-onramp git repo to bundle. Cloned at build time. |
| `helm_charts` | no | Helm chart repositories to bundle. |
| `images` | no | Container image pre-pull behavior. |
| `templates_dir` | no | Path to templates. Defaults to `./templates`. |

### `ubuntu`

```yaml
ubuntu:
  suites: [noble]              # Ubuntu codenames: jammy, noble, etc.
  architectures: [amd64]       # Only amd64 supported in 0.1.x
  # mirror: https://archive.ubuntu.com/ubuntu   # override for internal mirror
```

Resolution happens per `(suite × arch)` pair. Multiple suites multiply the
bundle size.

### `debs`

```yaml
debs:
  - name: ansible
  - name: git
  - name: make
  - name: curl
  - name: jq
  - name: ssh
  - name: sshpass
  - name: iptables
  - name: iptables-persistent
  - name: python3-kubernetes
```

Each entry is `name:` (and optionally `version:` if you need to constrain
it). The builder resolves the **transitive closure** of dependencies from
Ubuntu's `Packages.gz` indexes (main + universe), downloads each, and
verifies SHA256 against the index.

You do not list dependencies yourself — only top-level packages.

### `rke2`

```yaml
rke2:
  version: "v1.33.1+rke2r1"
  variants: [canal]
  image_mode: all-in-one
  # source: https://github.com/rancher/rke2/releases/download
```

| Field | Description |
|---|---|
| `version` | RKE2 release tag, including the `+rke2rN` suffix. |
| `variants` | CNI variants to bundle. `canal` is the default used in 0.1.x. |
| `image_mode` | `all-in-one` (single image tarball, includes canal) or `core+variant` (core + per-variant tarballs). Default `all-in-one`. |
| `source` | Optional override of the GitHub releases base URL (for internal mirrors). |

### `helm`

```yaml
helm:
  version: "v3.17.3"
```

Fetched from `https://get.helm.sh`. SHA256 verified against the published
checksum file.

### `aether_ops`

Three mutually exclusive modes:

**Mode 1 — local pre-built binary** (what the in-tree spec uses):

```yaml
aether_ops:
  version: "v0.1.43"
  source: ./artifacts/aether-ops_0.1.43_linux_amd64.tar.gz
```

**Mode 2 — GitHub release** (CI default; the release workflow rewrites
`source:` out of the spec to hit GitHub instead):

```yaml
aether_ops:
  version: "v0.1.43"
  # no source: — builder fetches from aether-gui/aether-ops GitHub releases
```

**Mode 3 — build from source**:

```yaml
aether_ops:
  ref: "main"
  repo: aether-gui/aether-ops
  # frontend_ref: ""   # optional frontend submodule override
```

Optional onramp user settings (applied by the launcher, not the builder):

```yaml
aether_ops:
  version: "v0.1.43"
  onramp_user: aether        # default: aether
  # onramp_password: ...     # do NOT commit; set via launcher env at runtime
```

The onramp password is never written to `bundle.yaml` in checked-in specs.
Override it at install time (future enhancement; see
[roadmap](/bootstrap-guide/roadmap)).

### `onramp`

```yaml
onramp:
  repo: https://github.com/opennetworkinglab/aether-onramp.git
  ref: main
  recurse_submodules: true
```

The aether-onramp Ansible toolchain is cloned at build time, its resolved
commit SHA is pinned in the manifest, and the launcher extracts it to
`/var/lib/aether-ops/aether-onramp` on install.

### `helm_charts`

```yaml
helm_charts:
  - name: sdcore-helm-charts
    repo: https://github.com/omec-project/sdcore-helm-charts.git
    ref: main
```

Each entry is cloned to `/var/lib/aether-ops/helm-charts/<name>` on the
target host.

### `images`

```yaml
images:
  auto_extract: true
  # list: []         # when auto_extract is false — must be complete
  # extra: []        # standalone images unioned with auto-extracted set
  exclude:
    - quay.io/stackanetes/kubernetes-entrypoint:v0.3.1
```

Container images are pre-pulled and staged for RKE2's airgap image loader.

- `auto_extract: true` — the builder scans the cloned Helm charts for image
  references and unions them with `extra`.
- `auto_extract: false` — only `list` is used, which **must** be the
  complete set of images needed (no scanning).
- `exclude` — images to skip. Legacy Docker v1 manifests (which
  go-containerregistry cannot pull) are a common entry here.

### `templates_dir`

```yaml
templates_dir: ./templates
```

Path to the directory containing systemd units, `sshd_config.d/` drop-ins,
`sudoers.d/` drop-ins, and RKE2 config templates. Defaults to `./templates`
relative to the spec file.

## The spec in `main`

The canonical `bundle.yaml` in the repo is the reference. Read it to see
current conventions and inline comments explaining each choice.
