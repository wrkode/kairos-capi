# Version Compatibility Matrix

This document tracks version compatibility between Kairos CAPI Provider and its dependencies.

## Current Version

**Kairos CAPI Provider**: v0.1.0

## Compatibility Matrix

| KairosCAPI Version | CAPI Version | Kairos Version | k0s Version | Kubernetes Version | Notes |
|---------------------|--------------|----------------|-------------|-------------------|-------|
| v0.1.0              | v1.8.0+      | ≥3.6.0         | ≥1.34.x     | v1.30+            | k0s only, single-node CP, no HA |

## Detailed Compatibility

### Cluster API (CAPI)

- **Minimum**: v1.8.0
- **Recommended**: v1.11.0+
- **Tested**: v1.8.0, v1.11.0

**Notes:**
- Uses CAPI v1beta1 core types (Cluster, Machine) as v1beta2 is not yet available
- Implements BootstrapConfig v1beta2 contract
- Implements ControlPlane v1beta2 contract

### Kairos OS

- **Minimum**: v3.6.0
- **Recommended**: Latest stable (v3.6.0+)
- **Tested**: v3.6.0

**Requirements:**
- Cloud-init support
- Native k0s integration (`k0s:` and `k0s-worker:` cloud-config keys)
- Support for `/var/lib/k0s/manifests/` directory

### k0s

- **Minimum**: v1.34.0
- **Recommended**: Latest stable
- **Tested**: v1.34.0+

**Features Used:**
- Single-node mode (`--single` flag)
- Worker join tokens
- Static manifests from `/var/lib/k0s/manifests/`

### Kubernetes

- **Target**: v1.30+
- **Supported via k0s**: Any version supported by k0s

**Note:** The Kubernetes version is determined by the k0s version used.

## Infrastructure Providers

### CAPD (Docker)

- **Version**: v1.8.0+ (matches CAPI version)
- **Status**: ✅ Tested and supported

### CAPV (vSphere)

- **Version**: v1.8.0+ (matches CAPI version)
- **Status**: ✅ Supported (requires configuration)

### Other Providers

- **Status**: ⚠️ Not yet tested
- **Compatibility**: Should work with any CAPI-compatible infrastructure provider

## Feature Support by Version

### v0.1.0

- ✅ Bootstrap provider (KairosConfig)
- ✅ Control plane provider (KairosControlPlane)
- ✅ Single-node control plane
- ✅ Worker nodes via MachineDeployment
- ✅ Native k0s integration
- ✅ Webhook validation
- ✅ CAPD support
- ✅ CAPV support
- ❌ Multi-node HA control plane
- ❌ k3s distribution
- ❌ ClusterClass support
- ❌ Upgrade support

## Breaking Changes

None yet (v0.1.0 is the first release).

## Upgrade Paths

### v0.1.0 → Future Versions

When upgrading, ensure:

1. **CAPI compatibility**: Management cluster CAPI version is compatible
2. **CRD updates**: Apply new CRD versions before upgrading controller
3. **Resource migration**: Check for any required resource migrations
4. **Webhook compatibility**: Ensure webhook configurations are updated

## Testing Matrix

| Test Scenario | CAPI v1.8.0 | CAPI v1.11.0 | Status |
|---------------|--------------|--------------|--------|
| Single-node CP (CAPD) | ✅ | ✅ | Tested |
| Worker nodes (CAPD) | ✅ | ✅ | Tested |
| Single-node CP (CAPV) | ⚠️ | ⚠️ | Manual testing needed |
| Webhook validation | ✅ | ✅ | Tested |
| Bootstrap data generation | ✅ | ✅ | Tested |

## Known Limitations

1. **CAPI v1beta2**: Core types (Cluster, Machine) still use v1beta1 as v1beta2 is not available in CAPI Go module
2. **HA Control Plane**: Multi-node control plane not yet implemented
3. **k3s Support**: Only k0s is currently supported
4. **Upgrade Support**: Rolling upgrades not yet implemented

## Future Compatibility Plans

- **CAPI v1beta2**: Will migrate when available
- **HA Control Plane**: Planned for v0.2.0
- **k3s Support**: Planned for v0.2.0
- **ClusterClass**: Planned for v0.3.0

## Reporting Compatibility Issues

If you encounter compatibility issues:

1. Check this matrix for known limitations
2. Verify versions match tested combinations
3. Open an issue with:
   - KairosCAPI version
   - CAPI version
   - Kairos version
   - k0s version
   - Infrastructure provider and version
   - Error logs and manifests

