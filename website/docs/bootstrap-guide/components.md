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
- `Plan(current, desired)` — returns a plan value. If `current == desired`
  and the component's config hash hasn't changed, the plan is a no-op.
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
| 4 | `service_account` | Creates the `aether-ops` OS user and group. |
| 5 | `onramp` | Creates the `aether` onramp user for Ansible deployments. |
| 6 | `rke2` | Installs and starts RKE2. |
| 7 | `helm` | Installs the Helm binary. |
| 8 | `aether_ops` | Installs and starts the aether-ops daemon. |

```mermaid
flowchart LR
    debs --> ssh --> sudoers --> sa["service_account"]
    sa --> onramp --> rke2 --> helm --> aops["aether_ops"]

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

**Why before service accounts:** sshd needs to be configured before the
onramp user (step 5) exists, so the first time the onramp user logs in,
sshd already accepts password auth for them.

sshd is reloaded (not restarted) at the end of the component so existing
connections aren't dropped.

## `sudoers`

Writes drop-in files into `/etc/sudoers.d/`.

**Touches:**

- `/etc/sudoers.d/aether-ops` — sudo rules for the service account.
- `/etc/sudoers.d/<onramp_user>` — `NOPASSWD: ALL` for the Ansible
  deployment user.

All drop-ins are validated with `visudo -cf` before being moved into place;
a broken sudoers file would lock out root access, so the launcher refuses
to install one that doesn't parse.

## `service_account`

Creates the `aether-ops` OS user and group.

**Touches:**

- Invokes `groupadd aether-ops` if the group doesn't exist.
- Invokes `useradd aether-ops` with system-account flags.
- `/home/aether-ops` if the user template expects it.

The group membership is what lets `aether-ops` read RKE2's kubeconfig at
mode `0640` in the `rke2` step.

**Exception:** `useradd` and `groupadd` are shelled out for the same reason
as `dpkg` — they're part of Ubuntu's required package set and handle PAM,
shadow, and group semantics correctly.

## `onramp`

Creates the onramp user (default name `aether`) used by aether-ops as the
Ansible SSH identity for reaching other nodes.

**Touches:**

- Creates the user with `useradd`.
- Sets the password (default `aether`; a future release will allow override
  via `AETHER_ONRAMP_PASSWORD`).
- Relies on the `sudoers` step to have dropped `NOPASSWD: ALL` for this
  user.
- Relies on the `ssh` step to have enabled password auth for this user.

The onramp user is **distinct from the service account**:

| | Service account (`aether-ops`) | Onramp user (`aether`) |
|---|---|---|
| Runs | The aether-ops daemon | Nothing (it's an identity, not a service) |
| Auth | N/A | SSH password |
| Sudo | Limited rules | `NOPASSWD: ALL` |
| Used by | The daemon | Ansible connecting *into* the node |

The default onramp password must be changed immediately after setup — this
is called out in [Getting Started](/getting-started/next-steps).

## `rke2`

Installs Rancher's Kubernetes distribution from the airgap tarballs in
`bundle/rke2/`.

**Touches:**

- Extracts `rke2.linux-<arch>.tar.gz` under `/usr/local` (or `/opt/rke2`
  if `/usr/local` is read-only).
- Stages the airgap image tarball to
  `/var/lib/rancher/rke2/agent/images/`.
- Writes `/etc/rancher/rke2/config.yaml` from a template. Notable entries:
  - `write-kubeconfig-mode: "0640"` — group-readable kubeconfig.
  - `write-kubeconfig-group: "aether-ops"` — service account can read it.
- Writes `/etc/systemd/system/rke2-server.service` (or uses the stock one
  shipped with RKE2).
- Writes `/etc/profile.d/rke2.sh` — adds RKE2's bin dir to `PATH` and sets
  `KUBECONFIG` for interactive users.
- Enables and starts `rke2-server.service` via systemd's D-Bus API.
- Waits (with timeout) for `https://localhost:6443/readyz` to return `ok`.

**Why after service_account and before helm:** RKE2 needs the `aether-ops`
group to exist so the kubeconfig mode `0640` / `group = aether-ops`
permissions land on a real group. Helm and aether-ops both need RKE2 up
before they're useful.

**Checksum verification:** every tarball fetched at build time has its
SHA256 verified against the `sha256sum-<arch>.txt` from the RKE2 release.
The builder re-verifies at bundle assembly; the launcher re-verifies at
extraction via the manifest.

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
- Writes `/etc/aether-ops/config.yaml` from the template.
- Stages the aether-onramp Ansible repo to
  `/var/lib/aether-ops/aether-onramp/`.
- Stages any bundled Helm charts to `/var/lib/aether-ops/helm-charts/`.
- Reloads systemd, enables and starts `aether-ops.service`.
- Waits for the daemon's health endpoint to respond.

**Why last:** every other component is a prerequisite. When this one
reports ready, the bootstrap's job is done and the launcher exits.

## Role filtering

The `--roles` flag restricts the set of components that run. When roles
are selected, the launcher walks the same ordered list but skips
components not required by the union of requested roles. See the
[CLI reference](./cli-reference.md#--roles-csv).

## Drift detection

Each component records a **config hash** in the state file, not just a
version. The hash covers the rendered template contents the component
wrote. On a later `upgrade`, the component compares:

- current version vs. desired version — if different, `Plan` is non-empty.
- current config hash vs. rendered-template hash — if different, `Plan` is
  non-empty *even if versions match*.

This is how a template change between bundle versions triggers re-apply of
the affected components, independent of whether the underlying binary moved.

`repair` ignores the state and re-applies everything regardless.

See [state file](./state-file.md) for the schema.
