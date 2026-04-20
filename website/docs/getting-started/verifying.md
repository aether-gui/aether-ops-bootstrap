---
id: verifying
title: Verifying the install
sidebar_position: 3
---

# Verifying the install

After `install` exits `0`, these checks confirm the host is in a healthy
state. Treat this page as a post-install checklist.

## The state file

```bash
sudo cat /var/lib/aether-ops-bootstrap/state.json | jq .
```

A successful install produces something like:

```json
{
  "schema_version": 1,
  "launcher_version": "v0.1.43",
  "bundle_version": "2026.04.1",
  "bundle_hash": "sha256:abcd…",
  "components": {
    "debs":            { "version": "42",             "installed_at": "2026-04-18T14:27:11Z" },
    "ssh":             { "version": "1",              "installed_at": "2026-04-18T14:27:13Z" },
    "sudoers":         { "version": "1",              "installed_at": "2026-04-18T14:27:13Z" },
    "service_account": { "version": "aether-ops",     "installed_at": "2026-04-18T14:27:14Z" },
    "onramp":          { "version": "aether",         "installed_at": "2026-04-18T14:27:14Z" },
    "rke2":            { "version": "v1.33.1+rke2r1", "installed_at": "2026-04-18T14:31:02Z" },
    "helm":            { "version": "v3.17.3",        "installed_at": "2026-04-18T14:31:03Z" },
    "aether_ops":      { "version": "v0.1.43",        "installed_at": "2026-04-18T14:32:44Z" }
  },
  "history": [
    { "action": "install", "timestamp": "2026-04-18T14:32:44Z", "launcher_version": "v0.1.43", "bundle_version": "2026.04.1" }
  ]
}
```

Every component should appear with a non-empty `version`. Missing entries are
a red flag and mean the component either wasn't registered or wasn't selected
by `--roles`.

A shortcut:

```bash
sudo ./aether-ops-bootstrap state
```

## Systemd units

```bash
systemctl is-active rke2-server aether-ops ssh
```

All three should print `active`. To see what the launcher installed, look
for the units it owns:

```bash
systemctl list-unit-files | grep -E '^(rke2-server|aether-ops)\.service'
```

## RKE2 health

```bash
sudo /var/lib/rancher/rke2/bin/kubectl get nodes -o wide
sudo /var/lib/rancher/rke2/bin/kubectl get pods -A
```

On a single-node install the node's `STATUS` should be `Ready`, and every
pod in the `kube-system` namespace should be `Running` or `Completed`.

RKE2's readiness endpoint:

```bash
curl -fsS --cacert /var/lib/rancher/rke2/server/tls/server-ca.crt \
     https://localhost:6443/readyz
```

Expected: `ok`.

## aether-ops health

```bash
systemctl status aether-ops
journalctl -u aether-ops --no-pager -n 40
```

Look for its `started` / `ready` log line. The final lines of the bootstrap's
own log printed the URL and any initial credential; find them in:

```bash
sudo tail -n 30 /var/lib/aether-ops-bootstrap/bootstrap.log
```

## Profile drop-in

Log out and back in (or `source /etc/profile`) to pick up the launcher's
`/etc/profile.d/rke2.sh` drop-in. After that:

```bash
which kubectl        # /var/lib/rancher/rke2/bin/kubectl
echo $KUBECONFIG     # /etc/rancher/rke2/rke2.yaml
```

Users in the `aether-ops` group can read that kubeconfig (mode `0640`, group
`aether-ops`) without `sudo`. Add yourself:

```bash
sudo usermod -aG aether-ops "$USER"
# log out + back in
```

## Idempotency check

Run the launcher again with the same bundle:

```bash
sudo ./aether-ops-bootstrap install --bundle bundle.tar.zst
```

It should either:

- refuse (because prior successful install exists — this is the default
  safety behaviour), or
- if you passed `--force`, detect that every component's `current` equals
  `desired`, skip every step, and exit cleanly in a few seconds.

Either answer is "pass." The point is that re-running doesn't break the host.

A cleaner way to test idempotency without `--force`:

```bash
sudo ./aether-ops-bootstrap check --bundle bundle.tar.zst
```

Expected: "no changes" across every component.

## What "done" looks like

- `state.json` has every expected component with a recent `installed_at`.
- `rke2-server.service` is active; `kubectl get nodes` shows the node `Ready`.
- `aether-ops.service` is active; its health endpoint returns successfully.
- `./aether-ops-bootstrap check --bundle bundle.tar.zst` plans zero changes.

Anything else is worth investigating — see
[troubleshooting](/bootstrap-guide/troubleshooting).
