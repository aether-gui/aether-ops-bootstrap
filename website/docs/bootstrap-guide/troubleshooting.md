---
id: troubleshooting
title: Troubleshooting
sidebar_position: 7
---

# Troubleshooting

A component-by-component fault tree for things that go wrong. For the most
common first-install issues, start with
[Getting Started → Common problems](/getting-started/common-problems).

## General approach

1. **Read the launcher's log.** `/var/lib/aether-ops-bootstrap/bootstrap.log`
   contains everything the last run printed. It's tee'd in real time so you
   can `tail -f` it during a run.
2. **Read the state file.** `sudo aether-ops-bootstrap state`. The `history`
   tells you what's been tried; the `components` map tells you which
   component last completed.
3. **Check systemd units.** `systemctl status <unit>` and `journalctl -u
   <unit>`. Most install failures end with a service failing to start.
4. **Collect diagnostics.** `sudo aether-ops-bootstrap diagnose --output /tmp`
   bundles everything above into one tarball for sharing.

## Preflight failures

These happen before any component runs. The launcher aborts cleanly — nothing
was touched on the host.

### `unsupported Ubuntu version`

**Cause.** `/etc/os-release` reports a version outside 22.04–26.04.

**Fix.** Use a supported Ubuntu release. The bundle's `.deb` dependency
closure is resolved per suite at build time; running on an unsupported
suite would at best install unrelated-version `.debs` and at worst refuse
to `dpkg -i` them.

### `must run as root`

**Cause.** The launcher was not invoked under `sudo` or as root.

**Fix.** `sudo aether-ops-bootstrap …`.

### `systemd not present`

**Cause.** `/run/systemd/system` doesn't exist — this host isn't running
systemd as PID 1.

**Fix.** The bootstrap requires systemd. Containers or non-systemd inits
are not supported.

### `manifest schema_version X not supported`

**Cause.** The bundle was built against a newer manifest schema than this
launcher understands.

**Fix.** Use a matching launcher version. The
[release process](/build-guide/release-process) ships launcher + bundle
together; mismatched pairs shouldn't normally exist.

### `prior install exists`

**Cause.** A state file is present at `/var/lib/aether-ops-bootstrap/state.json`.

**Fix.** Pick the right command — `upgrade`, `repair`, or `install --force`.
See [upgrades-and-repair](./upgrades-and-repair.md#when-to-use-which).

## Per-component failures

### `debs`

**Symptoms.** `dpkg: dependency problems …` or `dpkg: error processing
archive …`.

**Usual causes.**
- Ubuntu suite mismatch between bundle and host (e.g. bundle built for
  `noble`, host is `jammy`).
- A `.deb` was corrupted in transit (rare — the bundle hash check catches
  this, but if you extracted manually for troubleshooting, you may have
  bypassed it).
- An existing broken dpkg state on the host (`dpkg --configure -a`
  pending).

**Recovery.** Fix the host's dpkg state first:

```bash
sudo dpkg --configure -a
sudo apt-get -f install       # only if network is available
```

Then `repair`.

### `ssh`

**Symptoms.** sshd fails to reload, or the launcher logs `sshd -t` failure.

**Usual causes.**
- A pre-existing sshd drop-in conflicts with the one being written.
- The sshd binary isn't where the launcher expects.

**Recovery.**

```bash
sudo sshd -t                         # validates current config
ls /etc/ssh/sshd_config.d/           # see all drop-ins
sudo systemctl reload ssh            # reload after fixing
```

Then `repair`.

### `sudoers`

**Symptoms.** `visudo: parse error` or the drop-in wasn't moved into place.

**Usual causes.**
- A broken drop-in in `/etc/sudoers.d/` from a previous partial run.
- The bundle's template has a syntax error (unusual — this would fail CI).

**Recovery.** The launcher validates with `visudo -cf` before moving the
file in, so a broken drop-in never lands under `/etc/sudoers.d/` via the
launcher. If one is present, remove it manually:

```bash
sudo visudo -cf /etc/sudoers.d/<offending-file>
sudo rm /etc/sudoers.d/<offending-file>   # only if broken
```

Then `repair`.

### `service_account` / `onramp`

**Symptoms.** `useradd: user already exists` or similar.

**Usual causes.**
- A prior partial install left the user in place. This is usually benign —
  `useradd` with the right flags is idempotent-ish, and the launcher treats
  "user already exists, matches spec" as success.
- An actual different user with the same name exists (unusual).

**Recovery.** If the existing user isn't the one the launcher wants (wrong
UID, wrong groups), you'll need to remove it manually (`userdel`) before
the launcher can create it correctly.

### `rke2`

**Symptoms.** "waiting for rke2-server" times out; `rke2-server` reports
not ready.

**Usual causes.**
- **Disk pressure.** The airgap image tarball expands to several GB under
  `/var/lib/rancher/rke2/`. `df -h /var/lib/rancher` — you need 40 GB+ free.
- **Firewall.** `ufw` or `iptables` blocking 6443 to localhost is rare but
  happens on hardened images.
- **Clock skew.** RKE2's TLS certs are minted at install; significant clock
  skew breaks them. `timedatectl` to check.
- **Previous RKE2 install.** `/var/lib/rancher/rke2/` already populated by
  a different version/config. Run RKE2's uninstall script:
  ```bash
  sudo /usr/local/bin/rke2-uninstall.sh
  ```
  then `repair`.

**Recovery.**

```bash
sudo journalctl -u rke2-server --no-pager -n 200
sudo systemctl status rke2-server
sudo aether-ops-bootstrap repair --bundle bundle.tar.zst
```

### `helm`

**Symptoms.** Rare to fail — this component is just writing one binary.

**Usual causes.** `/usr/local/bin` doesn't exist or isn't writable by root
(extremely unusual on Ubuntu).

### `aether_ops`

**Symptoms.** `aether-ops.service` starts but health endpoint never
responds; or the service crashes on start.

**Usual causes.**
- **Misconfigured daemon.** `/etc/aether-ops/config.yaml` was hand-edited
  in a previous session and is invalid. `repair` re-renders it from the
  template.
- **Port already in use.** Something else is listening on the port
  aether-ops wants. `sudo ss -tlnp | grep <port>`.
- **Missing dependency on RKE2.** If the `rke2` component didn't complete,
  aether-ops will start but fail its health check because it can't reach
  the Kubernetes API. Fix `rke2` first.

**Recovery.**

```bash
sudo journalctl -u aether-ops --no-pager -n 200
sudo systemctl status aether-ops
# after resolving the issue:
sudo aether-ops-bootstrap repair --bundle bundle.tar.zst
```

## "The launcher exited cleanly but something's wrong"

Sometimes the bootstrap reports success but downstream users report
aether-ops misbehaving. Sequence:

1. **State file says everything's installed.** Confirm:
   ```bash
   sudo aether-ops-bootstrap state | jq '.components'
   ```
2. **All services are active.**
   ```bash
   systemctl is-active rke2-server aether-ops ssh
   ```
3. **`check` reports no changes.**
   ```bash
   sudo aether-ops-bootstrap check --bundle bundle.tar.zst
   ```
   If `check` reports changes, drift has happened. `repair` fixes it.
4. **aether-ops' own logs don't show errors.**
   ```bash
   sudo journalctl -u aether-ops --since '5 minutes ago'
   ```

If all four look healthy, the issue is inside aether-ops itself, not in
the bootstrap — escalate there.

## When to ship a diagnostic tarball

Always, for any failure you can't resolve in a minute or two. Running:

```bash
sudo aether-ops-bootstrap diagnose --output /tmp
```

produces `/tmp/aether-ops-bootstrap-diagnostics-<timestamp>.tar.gz`. Ship
that one file. It contains the state, the bootstrap log, recent service
journals, and the configs the launcher installed — everything a support
engineer needs to reproduce and diagnose without a second trip to the
airgap site.

The launcher also emits a diagnostic tarball **automatically** on any
failed run. The path is printed in the log:

```
diagnostic bundle saved to /tmp/aether-ops-bootstrap-diagnostics-20260418T143211.tar.gz
please send this file for troubleshooting support
```
