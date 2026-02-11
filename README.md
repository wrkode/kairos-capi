# Kairos CAPI Provider

**Important: This repo is under active development and not ready for production or testing deployments.**

**Research for HA control-plane**

Cluster API providers for Kairos OS.

## Overview

This project provides two Cluster API (CAPI) providers for managing Kubernetes clusters on Kairos:

1. **Bootstrap Provider** (`bootstrap.cluster.x-k8s.io`) - Generates Kairos cloud-config bootstrap data
2. **Control Plane Provider** (`controlplane.cluster.x-k8s.io`) - Manages Kairos-based Kubernetes control plane machines

## Target Versions

- **Kubernetes**: v1.34+
- **Cluster API**: v1.11+ (v1beta2 contracts)
- **Kairos**: v3.6.0+
- **Distribution**: k0s (MVP), k3s (future)

## Status

**Early Development** - MVP implementation in progress

## Quickstarts

- `docs/QUICKSTART_CAPD.md`
- `docs/QUICKSTART_CAPV.md`
- `docs/QUICKSTART_CAPK.md`

## License

Apache-2.0

