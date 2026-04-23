---
id: cli-reference
title: CLI reference
sidebar_position: 2
---

# CLI reference

Every subcommand and flag of the `aether-ops-bootstrap` launcher.

## Invocation

```
aether-ops-bootstrap <command> [flags]
```

Bare `aether-ops-bootstrap` (no command) prints usage.

## Commands

### `install`

Full bootstrap from scratch. Refuses to run if a prior successful install
exists on this host (see `--force`).

```bash
sudo aether-ops-bootstrap install --bundle bundle.tar.zst
```

Flags:

| Flag | Required | Description |
|---|---|---|
| `--bundle <path>` | yes | Path to the bundle `.tar.zst` file. |
| `--force` | no | Allow re-running on a host with existing state. Existing state is updated in place, not wiped. |
| `--roles <csv>` | no | Comma-separated roles; restricts which components run. Default: all components. See [Roles](#--roles-csv). |

Exit codes:

- `0` — install succeeded, state file updated.
- non-zero — install failed; a diagnostic tarball was written to `/tmp`.

### `upgrade`

Compare the bundle's manifest to the state file and apply components whose
desired version differs from the recorded version. Behaves like
`install --force` in that it does not refuse on existing state.

```bash
sudo aether-ops-bootstrap upgrade --bundle bundle-new.tar.zst
```

Flags: same as `install`. `--force` is implied.

### `repair`

Re-run every component's `Apply` regardless of what the state file says.
Used to fix drift — a hand-edited config, a removed systemd unit, a file
deleted out from under the launcher.

```bash
sudo aether-ops-bootstrap repair --bundle bundle.tar.zst
```

Flags: same as `install`. `--force` is implied.

### `check`

Preflight and plan, then exit without applying component actions. Dry-run.

```bash
sudo aether-ops-bootstrap check --bundle bundle.tar.zst
```

Flags: same as `install`.

Output:

```
running preflight checks...
extracting bundle bundle.tar.zst...
bundle version 2026.04.1 (schema 1)
detected host suite: noble
[debs] would apply ( -> 2026.04.1, 1 actions)
  - install 42 .deb packages
[ssh] would apply ( -> 2026.04.1, 1 actions)
  - drop sshd password auth config
[sudoers] would apply ( -> 2026.04.1, 1 actions)
  - install sudoers drop-in aether.tmpl
...
```

Exit code `0` means the plan succeeded; `check` never reports failure from
a component applying because it stops before `Apply`. In 0.1.x, `check`
still writes state metadata and a `history` entry for the run.

### `diagnose`

Collect a diagnostic bundle for remote troubleshooting.

```bash
sudo aether-ops-bootstrap diagnose [--output <dir>]
```

Flags:

| Flag | Default | Description |
|---|---|---|
| `--output <dir>` | `/tmp` | Where to write the diagnostic tarball. |

Writes `aether-bootstrap-diag-<timestamp>.tar.gz` containing:

- `/var/lib/aether-ops-bootstrap/state.json`
- `/var/lib/aether-ops-bootstrap/bootstrap.log`
- Recent `rke2-server` and `aether-ops` journal entries
- Relevant config files installed by the launcher

The launcher **also collects diagnostics automatically** on any failed
install / upgrade / repair run.

### `state`

Print the current state file as pretty JSON.

```bash
sudo aether-ops-bootstrap state
```

Fails if no state file exists. `jq` works on the output:

```bash
sudo aether-ops-bootstrap state | jq '.components | to_entries[] | .key + " = " + .value.version'
```

### `version`

Print the launcher version (and installed bundle version if state exists).

```bash
aether-ops-bootstrap version
```

Output:

```
aether-ops-bootstrap v0.1.43
installed bundle: 2026.04.1
```

### `help` / `-h` / `--help`

Print usage summary.

## Flags reference

### `--bundle <path>`

Required for `install`, `upgrade`, `repair`, `check`. Path to the bundle
tarball. Can be relative or absolute.

### `--force`

Allow `install` to run on a host with existing state. Upgrades and repairs
imply `--force`.

What `--force` does **not** do:

- Delete the existing state file.
- Remove installed components (systemd units, users, etc.).
- Reset the history log — it's append-only.

For a true clean install, you must also manually stop / disable services
and remove `/var/lib/aether-ops-bootstrap/state.json`. Almost no one needs
this.

### `--roles <csv>`

Comma-separated list of roles. Restricts which components run.

Valid roles (case-insensitive, several aliases accepted):

| Canonical | Aliases | Components |
|---|---|---|
| `mgmt` | `management` | `debs`, `ssh`, `sudoers`, `service_account`, `onramp`, `aether_ops` |
| `core` | `sd-core` | `debs`, `ssh`, `sudoers`, `service_account`, `rke2`, `helm` |
| `ran` | `srs-ran`, `ocudu` | `debs`, `ssh`, `sudoers`, `service_account` |

Examples:

```bash
--roles mgmt
--roles management
--roles mgmt,core
--roles SD-Core          # normalized to "core"
```

If `--roles` is **omitted**, every registered component runs (the
single-node default). If `--roles` is **present**, the union of requested
roles' components runs — anything outside that set is skipped.

Roles are a transitional multi-node mechanism. See
[roadmap](./roadmap.md).

### `--output <dir>`

Only for `diagnose`. Defaults to `/tmp`.

### `--onramp-password <value>`

Sets the password for the onramp Ansible user (the account created on
the target host that Ansible SSHes into — `aether` by default). Valid on
`install`, `upgrade`, `repair`, and `check`.

The password is resolved in this order of precedence:

1. `--onramp-password <value>` — this flag.
2. `AETHER_ONRAMP_PASSWORD` environment variable.
3. `aether_ops.onramp_password` in the bundle spec (strongly discouraged
   in checked-in specs; useful for gitignored scratch specs).
4. A 24-character random password, generated by the installer and logged
   to stderr at the end of the install banner. The launcher prints it
   with an `IMPORTANT: record this password` header; it is not displayed
   again.

The password is set on the OS user at first install only. Subsequent
`upgrade` / `repair` runs leave an existing password untouched so
post-install rotations are not silently reverted. To rotate, change the
password out-of-band and then re-run `install` on a fresh host.

### `--verbose`, `-v`

Stream subprocess output (dpkg, useradd, etc.) live to stderr. Off by
default; output is still captured and surfaced on failure.

## Environment variables

- `AETHER_ONRAMP_PASSWORD` — sets the onramp user password. Sits between
  the `--onramp-password` CLI flag (higher precedence) and the bundle
  spec's `aether_ops.onramp_password` field (lower precedence). Useful
  for CI / orchestration contexts where flags are harder to pass cleanly
  than env vars.

## Exit codes

- `0` — success (or `check` completed with a plan).
- `1` — generic failure. A diagnostic bundle was written.
- Specific non-zero codes are not yet stable; rely on the log output, not
  the exact exit code.
