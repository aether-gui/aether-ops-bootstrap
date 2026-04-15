# aether-onramp — Airgap integration findings

Findings from integrating `aether-onramp` into an airgapped single-node
deploy via `aether-ops-bootstrap` + `aether-ops`. Each finding lists
the observed behaviour, the affected files/lines, and a proposed
solution. Solutions are written to keep existing online-install
behaviour unchanged.

Line references are against `main` at the time of this report.

---

## 1. `apt: update_cache: yes` hard-fails in airgap

**Affected files**
- `deps/5gc/roles/router/tasks/install.yml:416–421` (iptables-persistent)
- `deps/srsran/roles/docker/tasks/install.yml:89–92, 186–188`
- `deps/ocudu/roles/docker/tasks/install.yml:89–92, 186–188`

**Observed behaviour**
In an airgapped environment the managed node's apt sources point at
unreachable upstream archives. Ansible's `apt` module with
`update_cache: yes` runs `apt-get update` before checking package
state, so the whole task fails with:

```
fatal: [host]: FAILED! => {"changed": false, "msg": "Failed to update apt cache: unknown reason"}
```

even when the package is already installed.

**Proposed solution**
Detect connectivity once, expose an `airgapped` fact, and gate
`update_cache` on it. Online installs see no change.

a. **Shared detection task** — e.g. `roles/common/tasks/detect_airgap.yml`,
   included via `pre_tasks:` or at the top of each role's
   `tasks/main.yml`:

   ```yaml
   - name: probe upstream apt archive reachability
     ansible.builtin.uri:
       url: http://archive.ubuntu.com/ubuntu/dists/{{ ansible_distribution_release }}/Release
       method: HEAD
       timeout: 5
       status_code: [200, 301, 302]
     register: _apt_probe
     failed_when: false
     changed_when: false
     when: airgapped is not defined

   - name: set airgapped fact
     ansible.builtin.set_fact:
       airgapped: "{{ _apt_probe.status is not defined or _apt_probe.status not in [200, 301, 302] }}"
     when: airgapped is not defined
   ```

b. **Operator override** documented in `vars/main.yml`:

   ```yaml
   # Skip network-dependent tasks (apt update_cache, helm OCI pulls,
   # etc.). Leave unset for auto-detect via connectivity probe.
   # airgapped: true
   ```

c. **Gate each call site** — one-character change:

   ```yaml
   - name: install iptables-persistent package
     ansible.builtin.apt:
       name: iptables-persistent
       state: present
       update_cache: "{{ not airgapped }}"
     when: inventory_hostname in groups['master_nodes']
     become: true
   ```

**Rationale**
- Zero behaviour change for online installs.
- Offline operators can set `airgapped: true` in inventory vars and
  skip the probe entirely.
- One detection task, one fact, reused at each call site — easy to
  review, easy to grep for.
- `uri` with `failed_when: false` tolerates DNS failure or no route;
  the status-code check catches "reached something that isn't actually
  the apt archive" (captive portals, internal mirrors mounted at `/`).
- HEAD on a concrete `dists/<release>/Release` path is cheaper than
  GET and fails fast on misdirected DNS.
- For sites with an internal apt mirror, exposing the probe URL as
  `airgap_probe_url` lets the operator point it at the local mirror.

---

## 2. `block/rescue` silently swallows helm-fetch failures in core deploy

**Affected file**
- `deps/5gc/roles/core/tasks/install.yml:186–229`

**Observed behaviour**
When `core.helm.chart_ref` can't be fetched (airgap + OCI default —
see finding #3), `kubernetes.core.helm` fails inside a `block:`. The
`rescue:` then runs:

1. `kubernetes.core.k8s_info` on namespace `aether-5gc` — which
   doesn't exist yet, so this returns an empty resources list, not an
   error.
2. A `loop` of `kubernetes.core.k8s state=absent` over that empty
   list — no-op.
3. `always: pause 60`.

All three succeed, so ansible considers the block handled and the
playbook exits 0. Callers (including the aether-ops API that drives
this playbook) report **deploy succeeded** even though nothing was
installed. The `aether-5gc` namespace is never created.

**Proposed solution**
Re-raise when the rescue didn't actually recover anything. Keep the
existing pod-restart logic for the genuine "some pods got stuck"
case, but fail loudly when there's nothing to restart:

```yaml
rescue:
  - name: Get Pods Status
    kubernetes.core.k8s_info:
      api_version: v1
      kind: Pod
      namespace: aether-5gc
    register: pod_status
    changed_when: false
    when: inventory_hostname in groups['master_nodes']

  - name: fail loudly if helm deploy produced no pods to recover
    ansible.builtin.fail:
      msg: >-
        sd-core helm deploy failed and no pods exist in aether-5gc —
        nothing to recover. See the preceding task output for the
        helm error (common causes: unreachable chart_ref, missing
        kubeconfig, cluster API down).
    when:
      - inventory_hostname in groups['master_nodes']
      - (pod_status.resources | default([])) | length == 0

  # existing restart loop unchanged …
```

Alternatively, drop the rescue entirely and let operators see the
helm error — the current recovery behaviour is narrow enough that
losing it may be acceptable.

---

## 3. `core.helm.chart_ref` defaults to an OCI registry; no local chart shipped

**Affected files**
- `deps/5gc/vars/main.yml:9` — `chart_ref: oci://ghcr.io/omec-project/sd-core`
- `deps/5gc/roles/core/defaults/main.yml:9` — same default

**Observed behaviour**
The `omec-project/sdcore-helm-charts` repository contains per-component
charts (`5g-control-plane`, `bess-upf`, `omec-control-plane`,
`omec-sub-provision`, `5g-ran-sim`, …) but **no top-level `sd-core`
umbrella chart**. `local_charts: true` mode (see install.yml:103–105)
derives:

- `local_sd_core_chart_root = dirname(chart_ref)`
- `local_sd_core_chart_path = /tmp/sdcore-helm-charts/basename(chart_ref)`

…and then reads `…/sd-core/Chart.yaml`, which does not exist in the
source repo. Airgap operators have no documented way to deploy
without reaching `ghcr.io`.

**Proposed solution**
Ship an `sd-core` umbrella chart alongside the per-component charts
in `sdcore-helm-charts`. Its `Chart.yaml` lists the existing component
charts as dependencies:

```yaml
apiVersion: v2
name: sd-core
version: <matches current published OCI tag>
dependencies:
  - name: 5g-control-plane
    version: <…>
    repository: file://../5g-control-plane
  - name: bess-upf
    version: <…>
    repository: file://../bess-upf
  # …
```

With this chart in place, `local_charts: true` + a local `chart_ref`
pointing at the cloned repo path works out of the box.

Alternative (lighter): add a section to the README describing the
expected local layout so operators can package their own umbrella
chart reproducibly, and include a `make local-chart` target that
produces it.

---

## 4. `core.data_iface: ens18` default fails on most modern Ubuntu installs

**Affected files**
- `deps/5gc/vars/main.yml:3` — `data_iface: ens18`
- `deps/5gc/roles/core/defaults/main.yml:3` — `data_iface: data`
- `deps/5gc/roles/router/defaults/main.yml:2` — `data_iface: data`

**Observed behaviour**
Modern systemd network naming yields `enp*`/`ens*` interface names
derived from PCI slot. `ens18` is specific to certain hypervisors
(proxmox VMs, some QEMU configs). LXD VMs see `enp5s0`; bare-metal
and most cloud VMs see varying names. The 5gc router role fails
early reading:

```
sysctl: cannot stat /proc/sys/net/ipv4/conf/ens18/forwarding: No such file or directory
```

**Proposed solution**
Default `data_iface` empty and derive it from facts when unset,
before any task references it:

```yaml
- name: derive data_iface from default-route interface when unset
  ansible.builtin.set_fact:
    data_iface: "{{ ansible_default_ipv4.interface }}"
  when:
    - core.data_iface is not defined or core.data_iface in ['', 'ens18', 'data']
```

The `'ens18'/'data'` guard preserves the common case where a user
inherited the stock `main.yml` verbatim and didn't intend those
values literally — a smoother upgrade than a pure "unset" default.

This is in addition to documenting that operators **should** override
`data_iface` explicitly in their `vars/main.yml` for reproducibility.

(FYI: the aether-ops controller already exposes a
`POST /api/v1/onramp/config/defaults` endpoint that rewrites
`core.data_iface` and related fields from per-node facts before
running a playbook. Fixing this upstream would still be valuable for
operators driving ansible directly.)

---

## 5. `python3-kubernetes` prerequisite is undocumented

**Affected sites**
- `deps/5gc/roles/core/tasks/install.yml:205` — `kubernetes.core.k8s_info`
- `deps/5gc/roles/core/tasks/install.yml:214` — `kubernetes.core.k8s`
- Similar in any other playbook using the `kubernetes.core` collection

**Observed behaviour**
On a fresh Ubuntu 24.04 host with `ansible` installed from apt,
`python3-kubernetes` is NOT pulled in as a dependency. First
invocation of `kubernetes.core.k8s_info` / `kubernetes.core.k8s`
fails with:

```
Failed to import the required Python library (kubernetes) on <host>'s
Python /usr/bin/python3.
```

In the core role this failure is additionally masked by finding #2's
`block/rescue`, so the playbook appears to succeed.

**Proposed solution**
Install the prerequisite as a `pre_tasks:` at the top of any role
that uses the `kubernetes.core` collection, using the same `airgapped`
gate from finding #1:

```yaml
- name: install kubernetes.core python prerequisite
  ansible.builtin.apt:
    name: python3-kubernetes
    state: present
    update_cache: "{{ not airgapped }}"
  become: true
```

Alternatively, add a dedicated role (`roles/common/prereqs`) that
installs all required apt packages plus ansible-galaxy collections
in one place, and document the full prerequisite list in the README.

---

## 6. Kubeconfig-for-user setup lives only inside onramp's own k8s role

**Affected file**
- `deps/k8s/roles/rke2/tasks/install.yml:326–336` (`copy /etc/rancher/rke2/rke2.yaml {{ ansible_env.HOME }}/.kube/config`)

**Observed behaviour**
When operators bring a pre-existing RKE2/K3s cluster (e.g. from an
external bootstrap flow, a managed Kubernetes, or a prior onramp
run that only ran the k8s role), `~/.kube/config` for the ansible
deployment user is never populated. `kubernetes.core.helm` then
defaults to `http://localhost:8080` and fails:

```
Error: Kubernetes cluster unreachable: Get "http://localhost:8080/version": dial tcp 127.0.0.1:8080: connect: connection refused
```

`aether-ops-bootstrap` worked around this by re-implementing the
kubeconfig copy; any other integrator that skips onramp's k8s role
will hit the same wall.

**Proposed solution**
Extract the current tasks (lines 326–336) into a small standalone
role, e.g. `roles/kubeconfig_for_user`, that:

1. Probes for a valid kubeconfig on disk in a fixed priority order:
   `/etc/rancher/rke2/rke2.yaml`, `/etc/rancher/k3s/k3s.yaml`, then
   the value of `$KUBECONFIG` if set.
2. Copies it to `{{ ansible_env.HOME }}/.kube/config` owned by the
   deployment user at mode `0600`.
3. Is idempotent (skip if the destination is already a valid
   kubeconfig pointing at the same cluster).

Invoke from both the 5gc and k8s playbooks as a `pre_tasks:` so it
runs whether or not onramp's k8s role is responsible for standing up
the cluster. Operators bringing their own cluster just skip the k8s
role and still get a working deployment-user kubeconfig.

---

## Summary

| # | Area                  | Severity | Online installs affected?          |
|---|-----------------------|----------|------------------------------------|
| 1 | `apt update_cache`    | High     | No (probe always reports reachable)|
| 2 | block/rescue masking  | High     | Rarely — but mask is unsafe either way |
| 3 | OCI-only chart_ref    | High     | No                                 |
| 4 | `data_iface` default  | High     | Only if operator doesn't override  |
| 5 | python3-kubernetes    | Medium   | Yes on fresh Ubuntu                |
| 6 | kubeconfig-for-user   | Medium   | Yes when bringing your own cluster |

Findings #1, #2, #3 together fully explain why an airgapped 5gc
install currently "succeeds" without deploying anything. #4 and #5
surface even on online installs; #6 surfaces any time onramp's k8s
role isn't the source of the cluster.

Happy to follow up with patches against any of these — let me know
which you'd like to take in which order.
