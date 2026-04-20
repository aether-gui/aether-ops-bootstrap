---
id: common-problems
title: Common problems
sidebar_position: 4
---

# Common problems

The handful of things that trip up operators the first time, and the recovery
commands for each.

## "Preflight failed: unsupported Ubuntu version"

The launcher supports Ubuntu Server **22.04, 24.04, and 26.04** only. If you
see this, you're running on 20.04, a derivative, or a newer release that
hasn't been tested.

**Fix:** reinstall Ubuntu at a supported version. The bootstrap intentionally
does not try to support arbitrary distros — the matrix of `.deb` transitive
dependencies is resolved at bundle-build time for specific suites.

## "Preflight failed: must run as root"

The launcher writes to `/etc/ssh/sshd_config.d/`, installs `.debs` via
`dpkg`, creates users, and writes systemd units. It needs root.

**Fix:**

```bash
sudo ./aether-ops-bootstrap install --bundle bundle.tar.zst
```

If your environment disallows `sudo` for the file's current user, log in
directly as root (or change ownership / `sudoers` to permit it).

## "Prior install exists"

The launcher refuses to run `install` on a host that already has a
successful state file. This is a safety feature — stomping an existing
install would wipe configuration.

**Fix:** pick the intent you actually had.

```bash
# Apply a delta from a newer bundle:
sudo ./aether-ops-bootstrap upgrade --bundle bundle.tar.zst

# Fix drift in-place (re-apply every component):
sudo ./aether-ops-bootstrap repair --bundle bundle.tar.zst

# Actually do want to reinstall from scratch:
sudo ./aether-ops-bootstrap install --bundle bundle.tar.zst --force
```

`--force` with `install` keeps the existing state file's history and
overwrites the rest. If you truly want a clean slate, also remove
`/var/lib/aether-ops-bootstrap/state.json` *and* stop and disable the
systemd units first — but almost nobody needs this.

## "Bundle hash mismatch"

The bundle on disk doesn't match its `.sha256`. Either:

1. The copy was corrupted in transit (retry the transfer), or
2. The bundle was modified (don't install it; verify provenance).

**Fix:** re-copy the file from the source and re-run
`sha256sum -c bundle.tar.zst.sha256`.

## "rke2-server did not become ready"

RKE2 started but its API didn't come up within the wait window. Usual causes:

- **Firewall** dropping traffic on port 6443 between the node and itself
  (yes, really — check `iptables -L` and `ufw status`).
- **Disk pressure** during image extraction — the image tarball expands to
  several GB in `/var/lib/rancher/rke2/`. Ensure you have 40 GB+ free.
- **Clock skew** causing TLS validation to fail. `timedatectl` to check.

**Fix:** look at the RKE2 journal to see the actual error.

```bash
sudo journalctl -u rke2-server --no-pager -n 200
```

Then:

```bash
sudo ./aether-ops-bootstrap repair --bundle bundle.tar.zst
```

to resume from where `install` bailed out.

## "aether-ops did not become ready"

RKE2 is up but aether-ops' health endpoint never responded. Usually a config
rendering issue or a port conflict.

```bash
sudo journalctl -u aether-ops --no-pager -n 200
sudo ss -tlnp | grep -E '8080|aether'
```

**Fix:** `repair` after resolving the underlying issue.

## "Nothing happened — exit 0 but no install"

Almost always means you passed `check` (or `--dry-run`) instead of `install`.
Verify the subcommand:

```bash
sudo ./aether-ops-bootstrap install --bundle bundle.tar.zst
```

## When in doubt: collect diagnostics

The launcher can package everything a support engineer needs into a single
tarball:

```bash
sudo ./aether-ops-bootstrap diagnose --output /tmp
```

This collects:

- `/var/lib/aether-ops-bootstrap/state.json`
- `/var/lib/aether-ops-bootstrap/bootstrap.log`
- Recent `rke2-server` and `aether-ops` journal entries
- Relevant config files the launcher installed

Ship the resulting `.tar.gz` to whoever is helping you. A diagnostic bundle
is also written automatically to `/tmp` on any failed install.

See the [troubleshooting reference](/bootstrap-guide/troubleshooting) for a
deeper per-component fault tree.
