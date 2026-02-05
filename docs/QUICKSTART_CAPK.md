# Quickstart: CAPK (KubeVirt)

This guide sets up a local KubeVirt environment and provisions a single-node Kairos+k0s cluster using CAPK.

## Prerequisites
- `docker`
- `kind`
- `kubectl`
- `clusterctl`
- `helm`
- `virtctl` (from KubeVirt releases)
- Go toolchain (for building `kubevirt-env`)

## Build the local helper
```
go build -o bin/kubevirt-env ./cmd/kubevirt-env
```

## Create the local KubeVirt environment
```
./bin/kubevirt-env setup
```

Notes:
- `kubevirt-env setup` creates a kind cluster, installs a default StorageClass, Calico, CDI, KubeVirt, CAPI, CAPK, osbuilder, cert-manager, and Kairos CAPI.
- KubeVirt emulation is enabled by default (set `KUBEVIRT_USE_EMULATION=false` to disable).
- To pin CAPK to a specific version, set `CAPK_VERSION` (e.g., `CAPK_VERSION=v0.1.x`).
- The control-plane API is exposed via a mandatory LoadBalancer Service named `<cluster>-control-plane-lb`. Ensure a LoadBalancer implementation is available (e.g., MetalLB in kind environments).

## Build and upload a Kairos image
```
./bin/kubevirt-env build-kairos-image
./bin/kubevirt-env upload-kairos-image
```

## Create a test cluster
```
kubectl apply -f config/samples/capk/kairos_cluster_k0s_single_node.yaml
```

## Check status
```
./bin/kubevirt-env test-cluster-status
```

## Optional: Run scripted flow
```
make kubevirt-env
make test-kubevirt
```

## Cleanup
```
./bin/kubevirt-env delete-test-cluster
./bin/kubevirt-env cleanup
```

## Troubleshooting
- If VMs do not start, confirm KubeVirt is `Available` and that `local-path` is the default StorageClass.
- If you have `/dev/kvm` available and want hardware acceleration, set `KUBEVIRT_USE_EMULATION=false` before `kubevirt-env setup`.
- If you use bridged/multus networking and the management cluster stays NotReady, ensure `spec.controlPlaneEndpoint.host` is reachable from the CAPI controllers; KubevirtMachine status may not report VM IPs in this mode.