# HA k0s Acceptance Runbook

This runbook covers HA bring-up and validation steps for CAPV and CAPK.

## CAPV HA bring-up

### Prerequisites
- vCenter access and CAPV installed in management cluster.
- Kairos VM template available in vSphere.
- Stable control-plane endpoint (VIP, LB, or DNS) reachable from management cluster.
- A Secret with vSphere credentials in `capv-system`.

### Steps
1) Apply the CAPV HA sample:
```
kubectl apply -f config/samples/capv/kairos_cluster_k0s_ha_controlplane.yaml
```

2) Verify control plane Machines are created:
```
kubectl get machines -n default
```
Expected:
- 3 Machines for the control plane.

3) Verify KairosConfig init and join modes:
```
kubectl get kairosconfigs -n default -o custom-columns=NAME:.metadata.name,ROLE:.spec.role,MODE:.spec.controlPlaneMode
```
Expected:
- 1 config with `control-plane` + `init`
- 2 configs with `control-plane` + `join`

4) Inspect init/join selection on Machines:
```
kubectl get machines -n default -o custom-columns=NAME:.metadata.name,MODE:.metadata.annotations.controlplane\\.cluster\\.x-k8s\\.io/kairos-control-plane-mode
```
Expected:
- Exactly one Machine marked `init`
- Remaining Machines marked `join`

5) Verify Cluster readiness:
```
kubectl get cluster kairos-cluster-ha -n default
kubectl describe cluster kairos-cluster-ha -n default
```
Expected:
- Cluster conditions move to Ready once control plane is up.

6) Validate k0s HA:
```
clusterctl get kubeconfig kairos-cluster-ha > kairos-ha.kubeconfig
kubectl --kubeconfig kairos-ha.kubeconfig get nodes
```
Expected:
- 3 control-plane nodes.

Optional (if k0s CLI is available inside nodes):
- Check etcd health using k0s tools.

### Repair test (CAPV)
1) Identify the init Machine:
```
kubectl get machines -n default -o custom-columns=NAME:.metadata.name,MODE:.metadata.annotations.controlplane\\.cluster\\.x-k8s\\.io/kairos-control-plane-mode
```

2) Delete a join Machine (do not delete the init Machine):
```
kubectl delete machine <join-machine-name> -n default
```

3) Verify a replacement Machine is created and marked as join:
```
kubectl get machines -n default -o custom-columns=NAME:.metadata.name,MODE:.metadata.annotations.controlplane\\.cluster\\.x-k8s\\.io/kairos-control-plane-mode
kubectl get kairosconfigs -n default -o custom-columns=NAME:.metadata.name,ROLE:.spec.role,MODE:.spec.controlPlaneMode
```
Expected:
- The deleted Machine is replaced.
- The new Machine is `join`, not `init`.
- Cluster remains Ready.

## CAPK HA bring-up (experimental)

CAPK HA is not yet validated. Use these steps for early testing only.

### Prerequisites
- KubeVirt installed in the management cluster.
- LoadBalancer implementation available (e.g., MetalLB).
- Kairos image uploaded to CDI.

### Steps
1) Apply the CAPK HA sample:
```
kubectl apply -f config/samples/capk/kubevirt_cluster_k0s_ha_controlplane.yaml
```

2) Verify Machines:
```
kubectl get machines -n default
```
Expected:
- 3 Machines for the control plane.

3) Verify init/join modes:
```
kubectl get kairosconfigs -n default -o custom-columns=NAME:.metadata.name,ROLE:.spec.role,MODE:.spec.controlPlaneMode
```

4) Validate k0s HA:
```
clusterctl get kubeconfig kairos-cluster-kv-ha > kairos-kv-ha.kubeconfig
kubectl --kubeconfig kairos-kv-ha.kubeconfig get nodes
```

## Notes
- If Cluster conditions remain NotReady, verify controlPlaneEndpoint and load balancer reachability.
- For CAPV, ensure the vSphere VM template is correct and networking is configured.
