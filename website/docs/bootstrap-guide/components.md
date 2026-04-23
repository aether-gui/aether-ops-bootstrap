---
id: components
title: Components
sidebar_position: 3
---

# Components

The launcher's install / upgrade / repair logic is a single loop over
ordered components. This page lists each one, what it does, and what it
touches on the host.

## The component interface

Every component implements the same four methods:

```go
type Component interface {
    Name() string
    DesiredVersion(b *bundle.Manifest) string
    CurrentVersion(s *state.State) string
    Plan(current, desired string) (Plan, error)
    Apply(ctx context.Context, plan Plan) error
}
```

- `Name()` — a stable identifier recorded in the state file.
- `DesiredVersion` — what the bundle wants. Read from `manifest.json`.
- `CurrentVersion` — what's on disk now. Read from `state.json`.
- `Plan(current, desired)` — returns a plan value. In 0.1.x, most components
  no-op when `current == desired`.
- `Apply(ctx, plan)` — actually do it.

Top-level commands walk the components in order, call `Plan`, and call
`Apply` unless in dry-run mode. **Idempotency falls out naturally.**

## Install order

The launcher runs components in this order. It's not configurable — it
reflects hard dependencies between components.

| # | Name | What it does |
|---|---|---|
| 1 | `debs` | Installs OS-level `.deb` prerequisites. |
| 2 | `ssh` | Writes `/etc/ssh/sshd_config.d/` drop-ins. |
| 3 | `sudoers` | Writes `/etc/sudoers.d/` drop-ins. |
| 4 | `service_account` | Creates the `aether-ops` service account and the `aether` onramp user. |
| 5 | `rke2` | Installs and starts RKE2. |
| 6 | `helm` | Installs the Helm binary. |
| 7 | `onramp` | Stages aether-onramp and Helm chart repositories for aether-ops. |
| 8 | `aether_ops` | Installs and starts the aether-ops daemon. |

```mermaid
flowchart LR
    debs --> ssh --> sudoers --> sa["service_account"]
    sa --> rke2 --> helm --> onramp --> aops["aether_ops"]

    style debs fill:#89b4fa,stroke:#1d7af3,color:#000
    style ssh fill:#89b4fa,stroke:#1d7af3,color:#000
    style sudoers fill:#89b4fa,stroke:#1d7af3,color:#000
    style sa fill:#a6e3a1,stroke:#40a02b,color:#000
    style onramp fill:#a6e3a1,stroke:#40a02b,color:#000
    style rke2 fill:#f9e2af,stroke:#df8e1d,color:#000
    style helm fill:#f9e2af,stroke:#df8e1d,color:#000
    style aops fill:#fab387,stroke:#fe640b,color:#000
```

## `debs`

Installs every `.deb` in `bundle/debs/` via `dpkg`.

**Touches:**

- Invokes `dpkg -i` on each file.
- System package database (`/var/lib/dpkg/`).
- Wherever the packages install (binaries in `/usr/bin`, etc.).

**Why first:** `git`, `make`, `ansible`, `curl`, and the rest are
prerequisites for everything downstream, including aether-ops' own
Ansible-driven operations.

**Exception:** `dpkg` is the one shelled-out tool. Reimplementing its
maintainer scripts, triggers, alternatives, and PAM hooks in Go is out of
scope. `dpkg` is part of Ubuntu's `Essential: yes` set and always present.

## `ssh`

Writes sshd drop-in files into `/etc/ssh/sshd_config.d/`.

**Touches:**

- `/etc/ssh/sshd_config.d/01-aether-password-auth.conf` — enables password
  authentication *only for the onramp user* via a `Match User` block.
- Other drop-ins as the templates dictate.

**Why before service accounts:** sshd is configured before the onramp user is
created, so the first time that user logs in, sshd already accepts password
auth for them.

The component restarts `ssh` or `sshd` after writing the drop-in so the new
configuration is active.

## `sudoers`

Writes drop-in files into `/etc/sudoers.d/`.

**Touches:**

- `/etc/sudoers.d/<onramp_user>` — `NOPASSWD: ALL` for the Ansible
  deployment user.

All drop-ins are validated with `visudo -cf` before being moved into place;
a broken sudoers file would lock out root access, so the launcher refuses
to install one that doesn't parse.

## `service_account`

Creates the `aether-ops` OS user and group, and creates the onramp deployment
user (default name `aether`).

**Touches:**

- Invokes `groupadd aether-ops` if the group doesn't exist.
- Invokes `useradd aether-ops` with system-account flags.
- Invokes `useradd <onramp_user>` with a login shell and home directory.
- Sets the onramp user's password on initial creation only.

The group membership is what lets `aether-ops` read RKE2's kubeconfig at
mode `0640` in the `rke2` step.

**Exception:** `useradd` and `groupadd` are shelled out for the same reason
as `dpkg` — they're part of Ubuntu's required package set and handle PAM,
shadow, and group semantics correctly.

## `onramp`

Stages the aether-onramp Ansible toolchain and any bundled Helm chart
repositories so aether-ops can deploy workloads fully offline.

**Touches:**

- `/var/lib/aether-ops/aether-onramp/`.
- `/var/lib/aether-ops/helm-charts/<name>/`.
- Ownership on those trees, set to `aether-ops:aether-ops`.

The onramp user (created by `service_account`) is the identity Ansible uses
over SSH. The onramp component itself installs the content that aether-ops
runs.

The onramp user is **distinct from the service account**:

| | Service account (`aether-ops`) | Onramp user (`aether`) |
|---|---|---|
| Shell | `/usr/sbin/nologin` | `/bin/bash` |
| Home | None (system account) | `/home/aether` |
| Password | None | Set from the resolved onramp password |
| Sudo | None | `NOPASSWD: ALL` (via `/etc/sudoers.d/` drop-in) |
| Runs | The aether-ops daemon via systemd | Nothing — it's an identity, not a service |
| Used by | The daemon | Ansible connecting *into* the node |

The onramp password is resolved from `--onramp-password`,
`AETHER_ONRAMP_PASSWORD`, the bundle spec, or — if none of those is set —
a random string the installer generates and logs at the end of the run.
See the [CLI reference](./cli-reference.md#--onramp-password-value) for
the exact precedence.

## `rke2`

Installs Rancher's Kubernetes distribution from the airgap tarballs in
`bundle/rke2/`.

**Touches:**

- Extracts `rke2.linux-<arch>.tar.gz` under `/usr/local` (or `/opt/rke2`
  if `/usr/local` is read-only).
- Stages the airgap image tarball to
  `/var/lib/rancher/rke2/agent/images/`.
- Stages any bundled application image tarballs to the same airgap image
  directory.
- Writes `/etc/rancher/rke2/config.yaml` from a template. Notable entries:
  - `write-kubeconfig-mode: "0640"` — group-readable kubeconfig.
  - `write-kubeconfig-group: "aether-ops"` — service account can read it.
- Writes `/etc/profile.d/rke2.sh` — adds RKE2's bin dir to `PATH` and sets
  `KUBECONFIG` for interactive users.
- Enables and starts `rke2-server.service` via `systemctl`.
- Waits for `kubectl get nodes --no-headers` against
  `/etc/rancher/rke2/rke2.yaml` to return at least one node.
- Symlinks `/usr/local/bin/kubectl` to RKE2's bundled kubectl.
- Copies the kubeconfig to the onramp user's `~/.kube/config`.

**Why after service_account and before helm:** RKE2 needs the `aether-ops`
group to exist so the kubeconfig mode `0640` / `group = aether-ops`
permissions land on a real group. Helm and aether-ops both need RKE2 up
before they're useful.

**Checksum verification:** every tarball fetched at build time has its
SHA256 verified against the `sha256sum-<arch>.txt` from the RKE2 release.

## `helm`

Installs the Helm binary from `bundle/helm/`.

**Touches:**

- Extracts `helm-v*-linux-<arch>.tar.gz`.
- Writes `/usr/local/bin/helm` (executable, `0755`).

Helm is just a client binary here; it doesn't run as a service.

## `aether_ops`

Installs and starts the aether-ops daemon.

**Touches:**

- Writes the daemon binary (typically `/usr/local/bin/aether-ops`).
- Writes `/etc/systemd/system/aether-ops.service`.
- Creates `/etc/aether-ops/` and `/var/lib/aether-ops/`.
- Reloads systemd, enables and starts `aether-ops.service`.
- Waits for `http://127.0.0.1:8186/healthz` to return HTTP 200.

**Why last:** every other component is a prerequisite. When this one
reports ready, the bootstrap's job is done and the launcher exits.

## Role filtering

The `--roles` flag restricts the set of components that run. When roles
are selected, the launcher walks the same ordered list but skips
components not required by the union of requested roles. See the
[CLI reference](./cli-reference.md#--roles-csv).

## Drift detection

In 0.1.x, components decide whether to apply primarily by comparing the
component version recorded in state with the desired version from the bundle.
Template-only drift is not detected by `check` or `upgrade` unless the
component's desired version also changes.

Use `repair` when you need to re-apply component actions regardless of what
the state file says.

See [state file](./state-file.md) for the schema.
