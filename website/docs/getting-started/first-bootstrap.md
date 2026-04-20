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
preflight: ubuntu 24.04 (noble) OK
preflight: architecture amd64 OK
preflight: systemd present OK
preflight: state: fresh install
components: plan
  debs             install 42 packages
  ssh              write 2 drop-ins
  sudoers          write 2 drop-ins
  service_account  create user aether-ops
  onramp           create user aether, set password
  rke2             install v1.33.1+rke2r1
  helm             install v3.17.3
  aether_ops       install v0.1.43
check complete: no changes applied
```

Anything red at this stage means preflight will fail in the real install too.
Fix it first — see [common problems](./common-problems.md).

## 5. Run the install

```bash
sudo ./aether-ops-bootstrap install --bundle bundle.tar.zst
```

This will take **5 – 15 minutes** depending on disk speed and whether images
have to be unpacked. Expected stages in the log:

```
preflight: … OK
component debs:            installing 42 packages via dpkg
component ssh:             writing /etc/ssh/sshd_config.d/…
component sudoers:         writing /etc/sudoers.d/…
component service_account: creating user aether-ops
component onramp:          creating user aether
component rke2:            extracting airgap tarballs
component rke2:            waiting for rke2-server (up to 5m)
component rke2:            rke2-server ready
component helm:            installing /usr/local/bin/helm
component aether_ops:      starting aether-ops.service
component aether_ops:      waiting for health endpoint
component aether_ops:      ready
install complete: bundle 2026.04.1 applied
state: /var/lib/aether-ops-bootstrap/state.json
```

If it fails partway through, the launcher will write a diagnostic tarball
under `/tmp`. The name will be printed. Keep that tarball — it has the logs,
the state file, and relevant journals. See
[troubleshooting](/bootstrap-guide/troubleshooting).

## 6. Confirm aether-ops is reachable

```bash
sudo systemctl status aether-ops
```

You should see `active (running)`. Then check the HTTP endpoint — the exact
port depends on the aether-ops config bundled in your release; the launcher
prints a summary on exit.

```bash
curl -fsS http://localhost:8080/healthz   # or whatever the summary said
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
