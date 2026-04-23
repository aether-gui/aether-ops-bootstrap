---
id: first-bootstrap
title: Your first bootstrap
sidebar_position: 2
---

# Your first bootstrap

Step-by-step: fresh Ubuntu box → aether-ops running. Assumes the prerequisites
on the [Getting Started](./index.md) page are met.

## 1. Get the artifacts onto the host

How the files get there is up to you — USB drive, `scp`, an internal artifact
store, a file server on the admin network. All three files should land in the
same directory, owned by a user with `sudo`.

```bash
# Example destination
mkdir -p ~/aether
cd ~/aether
# Copy these in from whatever medium:
#   aether-ops-bootstrap
#   bundle.tar.zst
#   bundle.tar.zst.sha256
ls -l
```

Expected:

```
-rwxr-xr-x 1 you you   22M Apr 18 14:23 aether-ops-bootstrap
-rw-r--r-- 1 you you  1.4G Apr 18 14:23 bundle.tar.zst
-rw-r--r-- 1 you you   83  Apr 18 14:23 bundle.tar.zst.sha256
```

If the launcher isn't executable:

```bash
chmod +x aether-ops-bootstrap
```

## 2. Verify the bundle integrity

Before trusting a multi-gigabyte opaque tarball, confirm it wasn't corrupted
in transit.

```bash
sha256sum -c bundle.tar.zst.sha256
```

Expected:

```
bundle.tar.zst: OK
```

If you see `FAILED`, **stop**. Re-copy the bundle from the source. Do not
proceed with a corrupted bundle — the launcher will catch it later, but it's
cheaper to catch it here.

## 3. Sanity-check the launcher

While you're still in a read-only mood, confirm the launcher works and prints
the version you expected.

```bash
./aether-ops-bootstrap version
```

Expected:

```
aether-ops-bootstrap v0.1.43
```

A bare `./aether-ops-bootstrap` (no args) prints usage with every subcommand.

## 4. Optional: dry-run first

If this is a production node or a regulated environment, run `check` first.
It does preflight and plans the install without changing anything.

```bash
sudo ./aether-ops-bootstrap check --bundle bundle.tar.zst
```

Expected (trimmed):

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

Anything red at this stage means preflight will fail in the real install too.
Fix it first — see [common problems](./common-problems.md).

## 5. Run the install

```bash
sudo ./aether-ops-bootstrap install --bundle bundle.tar.zst
```

The installer creates an onramp user (default name: `aether`) and sets
its password. If you do not supply a password, the installer generates a
random one and prints it to stderr inside an `IMPORTANT: record this
password` banner as soon as the password is resolved (early in the
install, before the long component work). This way the credential is
recoverable even if a later stage fails. To pin a known value up front:

```bash
sudo ./aether-ops-bootstrap install --bundle bundle.tar.zst \
    --onramp-password 'your-password-here'
```

Or via environment variable (handy in CI or when the value should not
show up in shell history):

```bash
sudo AETHER_ONRAMP_PASSWORD='your-password-here' \
    ./aether-ops-bootstrap install --bundle bundle.tar.zst
```

See the [CLI reference](/bootstrap-guide/cli-reference#--onramp-password-value)
for the full precedence (flag → env → spec → generated).

This will take **5 – 15 minutes** depending on disk speed and whether images
have to be unpacked. Expected stages in the log:

```
running preflight checks...
extracting bundle bundle.tar.zst...
bundle version 2026.04.1 (schema 1)
detected host suite: noble
[debs] applying ( -> 2026.04.1)...
  installing 42 packages via dpkg
[ssh] applying ( -> 2026.04.1)...
  wrote /etc/ssh/sshd_config.d/01-aether-password-auth.conf
[rke2] applying ( -> v1.33.1+rke2r1)...
  waiting for kubectl get nodes to succeed
[aether_ops] applying ( -> v0.1.43)...
  waiting for aether-ops at http://127.0.0.1:8186/healthz
  aether-ops healthy

========================================
  Bootstrap complete!
========================================

  aether-ops is running at http://127.0.0.1:8186
```

If it fails partway through, the launcher will write a diagnostic tarball
under `/tmp`. The name will be printed. Keep that tarball — it has the logs,
the state file, and relevant journals. See
[troubleshooting](/bootstrap-guide/troubleshooting).

## 6. Confirm aether-ops is reachable

```bash
sudo systemctl status aether-ops
```

You should see `active (running)`. Then check the HTTP endpoint:

```bash
curl -fsS http://localhost:8186/healthz
```

And check RKE2:

```bash
sudo /var/lib/rancher/rke2/bin/kubectl get nodes
```

Expected:

```
NAME              STATUS   ROLES                       AGE   VERSION
<hostname>        Ready    control-plane,etcd,master   3m    v1.33.1+rke2r1
```

If you added yourself to the `aether-ops` group (after logging out and back
in), plain `kubectl get nodes` works too thanks to the
`/etc/profile.d/rke2.sh` drop-in the launcher installs.

## 7. You're done

At this point the bootstrap's job is finished. The state file at
`/var/lib/aether-ops-bootstrap/state.json` records what was installed, and
the launcher will never run again on this host unless you explicitly invoke
`upgrade`, `repair`, or `install --force`.

Next: confirm the install looks right — [verifying](./verifying.md).
