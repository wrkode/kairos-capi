# Installation Guide

This guide covers installing the Kairos CAPI Provider in your management cluster.

## Prerequisites

- Kubernetes cluster (management cluster) running Kubernetes 1.28+
- `kubectl` configured to access the management cluster
- `kustomize` (v5+) for building manifests
- `docker` or `podman` for building images (optional, for development)

## Installation Methods

### Method 1: Using kustomize (Recommended for Development)

This method builds and deploys the provider directly from source.

#### Step 1: Build and Push Image

```bash
# Set your image registry and tag
export IMG=ghcr.io/wrkode/kairos-capi:latest

# Build the image
make docker-build

# Push the image (requires authentication to registry)
make docker-push
```

For development, you can use a local registry:

```bash
# Use kind's local registry
export IMG=localhost:5001/kairos-capi:latest
make docker-build
docker push ${IMG}
```

#### Step 2: Deploy Provider Components

```bash
# Set the image in kustomize
cd config/manager
kustomize edit set image controller=${IMG}

# Build and apply manifests
cd ../default
kustomize build . | kubectl apply -f -
```

Or use the Makefile target:

```bash
export IMG=ghcr.io/wrkode/kairos-capi:latest
make deploy
```

#### Step 3: Verify Installation

```bash
# Check controller is running
kubectl get pods -n kairos-capi-system

# Check CRDs are installed
kubectl get crds | grep kairos

# Check webhooks are registered
kubectl get validatingwebhookconfigurations | grep kairos
kubectl get mutatingwebhookconfigurations | grep kairos
```

### Method 2: Using clusterctl (Future)

Once releases are published, you'll be able to install using `clusterctl`:

```bash
# Install clusterctl
curl -L https://github.com/kubernetes-sigs/cluster-api/releases/download/v1.11.0/clusterctl-linux-amd64 -o clusterctl
chmod +x clusterctl
sudo mv clusterctl /usr/local/bin/

# Initialize with Kairos providers
clusterctl init --bootstrap kairos --control-plane kairos --infrastructure docker
```

**Note:** Full clusterctl integration requires publishing releases with `components.yaml` and `metadata.yaml`. See [Releases](#releases) section.

## Configuration

### Environment Variables

The controller manager supports the following environment variables:

| Variable | Description | Default |
|----------|-------------|---------|
| `WATCH_NAMESPACE` | Restrict controller to a single namespace. If empty, watches all namespaces. | `""` (all namespaces) |
| `LOG_LEVEL` | Logging verbosity: `debug`, `info`, `warn`, `error` | `info` |

#### Single-Namespace Mode

To run the controller in a single namespace (useful for multi-tenant setups):

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: kairos-capi-controller-manager
spec:
  template:
    spec:
      containers:
      - name: manager
        env:
        - name: WATCH_NAMESPACE
          value: "my-namespace"
```

#### Debug Logging

Enable debug logging:

```yaml
        env:
        - name: LOG_LEVEL
          value: "debug"
```

### Image Configuration

Override the default image:

```bash
# Set IMG before deploying
export IMG=my-registry.io/kairos-capi:v0.1.0
make deploy
```

Or edit `config/manager/kustomization.yaml`:

```yaml
images:
- name: controller
  newName: my-registry.io/kairos-capi
  newTag: v0.1.0
```

## Upgrading

### Upgrade Process

1. **Backup current configuration** (if needed):
   ```bash
   kubectl get kairosconfigs,kairoscontrolplanes -A -o yaml > backup.yaml
   ```

2. **Build and push new image**:
   ```bash
   export IMG=ghcr.io/wrkode/kairos-capi:v0.2.0
   make docker-build docker-push
   ```

3. **Update CRDs** (if changed):
   ```bash
   kubectl apply -f config/crd/bases
   ```

4. **Update deployment**:
   ```bash
   export IMG=ghcr.io/wrkode/kairos-capi:v0.2.0
   make deploy
   ```

5. **Restart controller** (if needed):
   ```bash
   kubectl rollout restart deployment/kairos-capi-controller-manager -n kairos-capi-system
   ```

## Uninstallation

### Remove Provider Components

```bash
# Delete all provider resources
kubectl delete -f config/default

# Or manually delete components
kubectl delete deployment kairos-capi-controller-manager -n kairos-capi-system
kubectl delete validatingwebhookconfigurations mutating-webhook-configuration
kubectl delete mutatingwebhookconfigurations mutating-webhook-configuration
kubectl delete clusterrole manager-role
kubectl delete clusterrolebinding kairos-capi-manager-rolebinding
kubectl delete serviceaccount kairos-capi-manager -n kairos-capi-system
```

**Warning:** Deleting CRDs will also delete all `KairosConfig` and `KairosControlPlane` resources. Ensure you've backed up or migrated any important data.

### Remove CRDs (Optional)

```bash
# Delete CRDs (this will delete all custom resources!)
kubectl delete crd kairosconfigs.bootstrap.cluster.x-k8s.io
kubectl delete crd kairosconfigtemplates.bootstrap.cluster.x-k8s.io
kubectl delete crd kairoscontrolplanes.controlplane.cluster.x-k8s.io
kubectl delete crd kairoscontrolplanetemplates.controlplane.cluster.x-k8s.io
```

## Troubleshooting

### Controller Not Starting

```bash
# Check pod logs
kubectl logs -n kairos-capi-system deployment/kairos-capi-controller-manager

# Check pod status
kubectl describe pod -n kairos-capi-system -l control-plane=controller-manager
```

### Webhook Issues

```bash
# Check webhook configuration
kubectl get validatingwebhookconfigurations mutating-webhook-configuration -o yaml
kubectl get mutatingwebhookconfigurations mutating-webhook-configuration -o yaml

# Check webhook service
kubectl get svc -n kairos-capi-system webhook-service
```

### RBAC Issues

```bash
# Check service account
kubectl get sa -n kairos-capi-system kairos-capi-manager

# Check cluster role binding
kubectl get clusterrolebinding kairos-capi-manager-rolebinding -o yaml
```

## Releases

### Building Release Artifacts

For clusterctl integration, releases need:

1. **components.yaml**: Kustomize-built manifest bundle
   ```bash
   cd config/default
   kustomize build . > ../../components.yaml
   ```

2. **metadata.yaml**: Provider metadata (see `config/clusterctl/metadata.yaml`)

3. **SHA256 checksums** for both files

### Release Checklist

- [ ] Update version in `config/clusterctl/metadata.yaml`
- [ ] Build and push Docker image with version tag
- [ ] Generate `components.yaml` from kustomize
- [ ] Calculate SHA256 checksums
- [ ] Update `metadata.yaml` with checksums and URLs
- [ ] Create GitHub release with both files
- [ ] Test clusterctl installation

## Development Installation

For local development:

```bash
# Run locally (not in cluster)
make run

# Or run in cluster with local image
export IMG=localhost:5001/kairos-capi:dev
make docker-build docker-push deploy
```

## Next Steps

After installation:

1. Review [QUICKSTART_CAPD.md](QUICKSTART_CAPD.md) for creating your first cluster
2. Check [API_REFERENCE.md](API_REFERENCE.md) for API details
3. See [VERSION_MATRIX.md](VERSION_MATRIX.md) for compatibility information

