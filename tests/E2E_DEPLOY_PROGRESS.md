# E2E Deploy Test — Progress Notes

Working log for getting `make test-e2e-deploy` (single-node deploy) green.
Pick up from here on the next machine.

## Status as of last session

`make test-e2e-bootstrap` (both single- and multi-node bootstrap) is green.

`make test-e2e-deploy` (single-node full deploy, ~40–50 min/run) was
mid-run at the **5GC install** step when the session paused. Last known
blocker: test `03_sdcore-pods.yaml` failed with 10 min wait + no
diagnostics. Commit `d0eeb5c` extends the wait to 15 min and routes
diagnostics through stdout so the next failure (if any) is actionable.

`make test-e2e-multi-deploy` has **not been exercised** yet. Same fixes
should apply but the 3-VM suite will likely surface additional issues
(srsran/ocudu docker apt tasks; per-node config_defaults edge cases).

## Fixes applied (chronological, all on main)

| # | SHA        | Problem                                                                                   | Fix                                                                                                                       |
|---|------------|-------------------------------------------------------------------------------------------|---------------------------------------------------------------------------------------------------------------------------|
| 1 | `6aacfe5`  | Onramp readiness wait timed out on a *healthy* provider                                   | `ProviderInfo.Degraded` is serialised `omitempty`; check `running==true && degraded!=true` (absent counts as not-degraded) |
| 2 | `bbd5360`  | Ansible SSH login failed with "Invalid/incorrect password"                                | Dropped cloud-init `aether` user block; bootstrap now creates the user with `password=aether` matching the registration   |
| 3 | `046484a`  | `sysctl net/ipv4/conf/ens18/forwarding: No such file`                                     | Call `POST /api/v1/onramp/config/defaults` after node registration; rewrites `core.data_iface` from node facts             |
| 4 | `c66ee59`  | 5gc router role's `apt: iptables-persistent update_cache=yes` failed in airgap             | Added `iptables-persistent` to `bundle.yaml`; test setup strips `/etc/apt/sources.list*` post-airgap so `apt update` no-ops |
| 5 | `1682565`  | helm "Kubernetes cluster unreachable"; ansible missing python `kubernetes` client          | rke2 component now writes `~/.kube/config` for the onramp user (owned by them, 0600); bundle ships `python3-kubernetes`    |
| 6 | `a4c3ee9`  | Pod-running check used wrong namespace (`omec` vs actual `aether-5gc`)                    | Fix namespace; poll up to 10 min                                                                                          |
| 7 | `d0eeb5c`  | 10 min wasn't enough; diag had gone to stderr so DART showed empty failure                | 15 min poll, periodic `kubectl get pods -A` snapshots, final events dump — all to stdout                                   |

## Outstanding questions / likely next failures

1. **SD-Core pods Running.** If the `d0eeb5c` run fails again, the new
   diagnostics will print pod state every 60s + final events. Expect
   either ImagePullBackOff (image not pre-staged in containerd) or init
   container hangs. Look at the output of the latest run first; no need
   to re-run blind.

2. **Upstream aether-onramp `update_cache: yes` issue.** Our test-side
   workaround (stripped apt sources) papers over it, but real airgap
   operators hit the same. Proposed upstream patch — drop `update_cache`
   from three tasks:
   - `deps/5gc/roles/router/tasks/install.yml:416–421` (iptables-persistent)
   - `deps/srsran/roles/docker/tasks/install.yml:89–92, 186–188`
   - `deps/ocudu/roles/docker/tasks/install.yml:89–92, 186–188`

   Ben will sync with the onramp developer on this.

3. **Multi-node deploy.** Not yet run. Extra surface area:
   - 3 VMs (`mgmt-vm`, `core-vm`, `gnb-vm`) instead of 1
   - srsran docker apt tasks (same `update_cache=yes` pattern)
   - gnb-role data_iface defaulting (`gnbsim.router.data_iface` rule covers it in configdefaults)

## How to resume

```bash
cd /home/ben/repos/bengrewell/aether-ops-bootstrap
git pull
make test-e2e-deploy 2>&1 | tee /tmp/deploy.log
```

If the 15-min pod wait expires again, inspect `/tmp/deploy.log` first
before re-running — the new diagnostics should show pod state and
recent events inline.

For interactive debugging: `dart -c tests/single-node-deploy/single-node-deploy.yaml -s -p`
adds pause-on-error so the VM stays up. Then `lxc exec sn-vm bash`.

## Running background task

Task `b8rsml57l` was running when the session paused. Log is at
`/tmp/claude-1000/-home-ben-repos-bengrewell-aether-ops-bootstrap/<session-id>/tasks/b8rsml57l.output`.
That file is session-scoped and will be gone on the next machine.
Re-run from scratch.

## Key files touched

- `bundle.yaml` — added `iptables-persistent`, `python3-kubernetes`
- `internal/components/rke2/rke2.go` — kubeconfig install action + `installOnrampKubeconfig`, `userLookup`, `chownPath`
- `tests/single-node-deploy/setup/02_disable-nat.yaml` — strip apt sources
- `tests/single-node-deploy/setup/05_configure-aether-ops.yaml` — fixed readiness check + diag
- `tests/single-node-deploy/setup/06_register-node.yaml` — added `apply config defaults` step
- `tests/single-node-deploy/single-node-deploy.yaml` — removed cloud-init `aether` user
- `tests/single-node-deploy/tests/03_sdcore-pods.yaml` — namespace + wait + diagnostics
- Parallel changes in `tests/multi-node-deploy/**` for the as-yet-unrun multi-node suite
- Parallel cloud-init cleanup in `tests/single-node-bootstrap/**` and `tests/multi-node-bootstrap/**` (bootstrap tests still green)
