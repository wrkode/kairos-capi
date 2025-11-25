# Quick Start Guide - CAPV (vSphere)

This guide walks you through creating a single-node k0s cluster on Kairos using Cluster API with the vSphere provider (CAPV).

## Prerequisites

1. **vSphere Environment**:
   - vCenter Server access
   - Datacenter, Datastore, Network configured
   - VM Template with Kairos OS installed
   - Resource Pool (optional)

2. **Management Cluster**: A Kubernetes cluster with access to vSphere

3. **Cluster API**: CAPI v1.11+ installed

4. **CAPV**: Cluster API Provider vSphere installed and configured

5. **Kairos CAPI Provider**: This provider installed

### Installing Prerequisites

#### 1. Install Cluster API

```bash
# Install clusterctl
curl -L https://github.com/kubernetes-sigs/cluster-api/releases/download/v1.11.0/clusterctl-linux-amd64 -o clusterctl
chmod +x clusterctl
sudo mv clusterctl /usr/local/bin/

# Initialize CAPI with vSphere
clusterctl init --infrastructure vsphere
```

#### 2. Configure vSphere Credentials

Create a `VSphereClusterIdentity` and Secret with vSphere credentials:

**IMPORTANT**: The Secret **MUST** be created in the `capv-system` namespace (where the CAPV controller runs), not in your cluster namespace.

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: vsphere-credentials-secret
  namespace: capv-system  # IMPORTANT: MUST be in capv-system namespace
type: Opaque
stringData:
  username: administrator@vsphere.local
  password: <your-password>
---
apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
kind: VSphereClusterIdentity
metadata:
  name: vsphere-credentials
  # Note: VSphereClusterIdentity is cluster-scoped (no namespace)
spec:
  secretName: vsphere-credentials-secret  # References the Secret in capv-system namespace
  allowedNamespaces:
    # Use selector to control which namespaces can use this identity
    # Option A: Allow all namespaces (use with caution)
    selector:
      matchLabels: {}
    # Option B: Allow specific namespaces by label (RECOMMENDED)
    # First label namespaces: kubectl label namespace default vsphere-identity=allowed
    # Then use:
    # selector:
    #   matchLabels:
    #     vsphere-identity: allowed
```

**Apply the credentials**:
```bash
# Apply the Secret and VSphereClusterIdentity
kubectl apply -f - <<EOF
apiVersion: v1
kind: Secret
metadata:
  name: vsphere-credentials-secret
  namespace: capv-system
type: Opaque
stringData:
  username: administrator@vsphere.local
  password: <your-password>
---
apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
kind: VSphereClusterIdentity
metadata:
  name: vsphere-credentials
spec:
  secretName: vsphere-credentials-secret
  allowedNamespaces:
    selector:
      matchLabels:
        vsphere-identity: allowed
EOF

# Label your namespace to allow using this identity
kubectl label namespace default vsphere-identity=allowed
```

#### 3. Install Kairos CAPI Provider

```bash
# Apply CRDs
kubectl apply -f config/crd/bases

# Apply RBAC
kubectl apply -f config/rbac

# Deploy the manager
kubectl apply -f config/manager/manager.yaml
```

## Creating a Cluster

### Step 1: Prepare vSphere Resources

Before applying the manifest, ensure you have:

1. **VM Template**: A Kairos VM template in vSphere
   - Template name (e.g., "kairos-opensuse-leap-v3.6.0")
   - Template should have Kairos OS installed

2. **Network**: VM Network name in vSphere

3. **Datacenter**: Datacenter name

4. **Datastore**: Datastore name

### Step 2: Customize the Sample Manifest

Edit `config/samples/capv/kairos_cluster_k0s_single_node.yaml`:

#### Update Cluster

```yaml
apiVersion: cluster.x-k8s.io/v1beta2
kind: Cluster
metadata:
  name: kairos-cluster
  namespace: default
spec:
  infrastructureRef:
    # Note: In v1beta2, use apiGroup instead of apiVersion
    apiGroup: infrastructure.cluster.x-k8s.io
    kind: VSphereCluster
    name: kairos-cluster
  controlPlaneRef:
    # Note: In v1beta2, use apiGroup instead of apiVersion
    apiGroup: controlplane.cluster.x-k8s.io
    kind: KairosControlPlane
    name: kairos-control-plane
```

#### Update VSphereCluster

```yaml
apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
kind: VSphereCluster
metadata:
  name: kairos-cluster
  namespace: default
spec:
  # IMPORTANT: Use only hostname or IP address, NOT a URL
  # Correct: "vcenter.example.com" or "172.16.56.10"
  # Wrong: "https://vcenter.example.com" or "https://172.16.56.10/sdk"
  server: "vcenter.example.com"  # TODO: Set your vCenter server
  # thumbprint: "..."             # Optional: SSL thumbprint
  identityRef:
    kind: VSphereClusterIdentity
    name: vsphere-credentials
    # Note: identityRef does NOT have an apiVersion field
  # Note: datacenter is NOT specified here - it's specified in VSphereMachineTemplate instead
```

#### Update VSphereMachineTemplate

```yaml
apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
kind: VSphereMachineTemplate
metadata:
  name: kairos-control-plane-template
  namespace: default
spec:
  template:
    spec:
      datacenter: "Datacenter"           # TODO: Set your datacenter
      datastore: "Datastore"             # TODO: Set your datastore
      folder: "Folder"                   # TODO: Set folder (optional)
      network:
        devices:
          - networkName: "VM Network"     # TODO: Set your network name
      resourcePool: "ResourcePool"       # TODO: Set resource pool (optional)
      numCPUs: 2                         # TODO: Adjust CPU count
      memoryMiB: 4096                    # TODO: Adjust memory
      diskGiB: 50                        # TODO: Adjust disk size
      template: "kairos-template"        # TODO: Set your Kairos template name
      cloneMode: "fullClone"             # or "linkedClone"
```

#### Update KairosControlPlane

```yaml
apiVersion: controlplane.cluster.x-k8s.io/v1beta2
kind: KairosControlPlane
metadata:
  name: kairos-control-plane
  namespace: default
spec:
  replicas: 1
  version: "v1.30.0+k0s.0"
  machineTemplate:
    infrastructureRef:
      apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
      kind: VSphereMachineTemplate
      name: kairos-control-plane-template
      namespace: default
  kairosConfigTemplate:
    name: kairos-config-template-control-plane
    # Note: namespace defaults to the same namespace as KairosControlPlane
```

#### Update KairosConfigTemplate (Optional)

```yaml
apiVersion: bootstrap.cluster.x-k8s.io/v1beta2
kind: KairosConfigTemplate
metadata:
  name: kairos-config-template-control-plane
  namespace: default
spec:
  template:
    spec:
      role: control-plane
      distribution: k0s
      kubernetesVersion: "v1.30.0+k0s.0"
      userName: kairos
      userPassword: kairos                    # TODO: Change for production
      userGroups:
        - admin
      githubUser: "your-github-username"      # TODO: Add your GitHub user
      # Or use sshPublicKey instead
      # sshPublicKey: "ssh-rsa AAAAB3..."
```

### Step 3: Apply the Manifest

```bash
kubectl apply -f config/samples/capv/kairos_cluster_k0s_single_node.yaml
```

### Step 4: Monitor Cluster Creation

```bash
# Watch cluster status
kubectl get cluster kairos-cluster -w

# Check control plane
kubectl get kairoscontrolplane kairos-control-plane

# Check machines
kubectl get machines

# Check VSphereMachines
kubectl get vspheremachines

# Check VSphereVMs (actual VMs in vSphere)
kubectl get vspherevms
```

### Step 5: Verify Cluster

```bash
# Get kubeconfig
clusterctl get kubeconfig kairos-cluster > kairos-kubeconfig.yaml

# Check nodes
kubectl --kubeconfig=kairos-kubeconfig.yaml get nodes

# Verify k0s is running
kubectl --kubeconfig=kairos-kubeconfig.yaml get pods -n kube-system
```

## Field Reference

### Required Fields to Customize

| Field | Location | Description |
|-------|----------|-------------|
| `server` | VSphereCluster.spec | vCenter server FQDN/IP (hostname or IP only, NOT a URL) |
| `datacenter` | VSphereCluster.spec, VSphereMachineTemplate.spec | Datacenter name |
| `datastore` | VSphereMachineTemplate.spec | Datastore name |
| `networkName` | VSphereMachineTemplate.spec.network.devices | VM Network name |
| `template` | VSphereMachineTemplate.spec | Kairos VM template name |

### Optional Fields

| Field | Location | Description |
|-------|----------|-------------|
| `thumbprint` | VSphereCluster.spec | SSL thumbprint for vCenter |
| `identityRef` | VSphereCluster.spec | Reference to VSphereClusterIdentity |
| `folder` | VSphereMachineTemplate.spec | VM folder path |
| `resourcePool` | VSphereMachineTemplate.spec | Resource pool path |
| `numCPUs` | VSphereMachineTemplate.spec | Number of CPUs |
| `memoryMiB` | VSphereMachineTemplate.spec | Memory in MiB |
| `diskGiB` | VSphereMachineTemplate.spec | Disk size in GiB |
| `cloneMode` | VSphereMachineTemplate.spec | "fullClone" or "linkedClone" |

## Troubleshooting

### VSphere Connection Issues

```bash
# Check VSphereCluster status
kubectl describe vspherecluster kairos-cluster

# Verify VSphereClusterIdentity is ready
kubectl get vsphereclusteridentity vsphere-credentials -o yaml

# Verify credentials (must be in capv-system namespace)
kubectl get secret vsphere-credentials-secret -n capv-system -o yaml

# Check CAPV controller logs
kubectl logs -n capv-system deployment/capv-controller-manager

# Common issues:
# 1. Secret not in capv-system namespace → Move it: kubectl get secret vsphere-credentials-secret -n default -o yaml | kubectl apply -n capv-system -f -
# 2. Server URL includes protocol/path → Use only hostname/IP: "172.16.56.10" not "https://172.16.56.10/sdk"
# 3. Namespace not labeled → Label it: kubectl label namespace default vsphere-identity=allowed
```

### VM Creation Fails

```bash
# Check VSphereMachine status
kubectl describe vspheremachine <machine-name>

# Check VSphereVM status
kubectl describe vspherevm <vm-name>

# Verify template exists in vSphere
# Verify network/datastore names are correct
```

### Bootstrap Issues

```bash
# Check KairosConfig
kubectl describe kairosconfig <config-name>

# Check bootstrap secret
kubectl get secret <dataSecretName> -o yaml

# Check Kairos CAPI controller logs
kubectl logs -n kairos-capi-system deployment/kairos-capi-controller-manager
```

## Security Considerations

1. **Credentials**: Use `VSphereClusterIdentity` instead of inline credentials
2. **User Password**: Change default `userPassword` from "kairos" for production
3. **SSH Access**: Use `githubUser` or `sshPublicKey` instead of password-based access
4. **Worker Tokens**: Use `WorkerTokenSecretRef` instead of inline `WorkerToken`

## Next Steps

- Configure additional worker nodes
- Set up HA control plane (replicas > 1)
- Add custom Kubernetes manifests via `spec.manifests`
- Integrate with your CI/CD pipeline

## Cleanup

```bash
# Delete the cluster
kubectl delete -f config/samples/capv/kairos_cluster_k0s_single_node.yaml

# Or use clusterctl
clusterctl delete cluster kairos-cluster

# Note: This will delete VMs in vSphere
```

