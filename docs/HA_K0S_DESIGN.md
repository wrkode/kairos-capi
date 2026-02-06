# HA k0s Control Plane (Milestone 1)

This document describes the first milestone for HA control planes using k0s on Kairos.

## Target topology
- 3 control-plane nodes with embedded etcd (k0s controllers).
- Single-node control plane remains supported and unchanged.

## Control-plane endpoint assumptions
### CAPV (vSphere)
Users must provide a stable endpoint for the workload cluster API:
- A VIP, external load balancer, or DNS name that resolves to the control-plane service.
- The endpoint must be set in `Cluster.spec.controlPlaneEndpoint.host` and `Cluster.spec.controlPlaneEndpoint.port`.

The provider does not create or manage this endpoint in Milestone 1.

## Init and join flow
Exactly one control-plane node performs cluster init. All other control-plane nodes join.

### Init controller
- `k0s: enabled: true`
- Uses `--single` only when `singleNode=true` (replicas=1).
- No join token is used for init.

### Join controller
- `k0s: enabled: true`
- Requires a controller join token written to `/etc/k0s/controller-token`.
- Uses `--token-file /etc/k0s/controller-token`.

### Worker nodes
- Unchanged behavior: `k0s-worker: enabled: true` with `/etc/k0s/token`.

## Token management
### Control-plane join token
Provided via a Secret reference in the cluster namespace:
- `controlPlaneJoinTokenSecretRef` on `KairosControlPlaneSpec`
- Controller copies it into each `KairosConfig` for join controllers
- Key defaults to `token` when omitted

### Worker token
Unchanged:
- `workerTokenSecretRef` (preferred) or legacy fields on `KairosConfigSpec`

## Init selection and stability
- The controller deterministically selects exactly one init machine.
- Selection rule: lowest ordinal name (e.g., `<kcp-name>-0`) or oldest by creation timestamp.
- The selected init machine is persisted via a Machine annotation to prevent role flipping.

## Failure handling (basic)
- If init machine fails before any controller is ready, the controller can re-elect an init machine using the deterministic rule.
- No scale-down safety or reconfiguration logic in this milestone.

## Future work (not in scope)
- Scale-down safety rules for control-plane nodes.
- Automated upgrades and rolling update orchestration for HA.
- Control-plane endpoint creation/management.
- Multi-region or stacked topology support.
- k3s HA support.

## Out of scope (Milestone 1)
- Upgrades and version skew handling.
- Scale-down safety.
- New infrastructure providers.
- Automated LB/VIP provisioning.
