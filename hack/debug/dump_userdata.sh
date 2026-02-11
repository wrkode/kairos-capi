#!/usr/bin/env bash
set -euo pipefail

if [ "$#" -lt 2 ]; then
  echo "Usage: $0 <cluster-name> <namespace>"
  exit 1
fi

CLUSTER_NAME="$1"
NAMESPACE="$2"

echo "Cluster: ${CLUSTER_NAME}"
echo "Namespace: ${NAMESPACE}"
echo

machines=$(kubectl get machines -n "${NAMESPACE}" -l "cluster.x-k8s.io/cluster-name=${CLUSTER_NAME}" -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}' || true)

if [ -z "${machines}" ]; then
  echo "No Machines found for cluster ${CLUSTER_NAME} in ${NAMESPACE}"
  exit 0
fi

for machine in ${machines}; do
  echo "=== Machine: ${machine} ==="
  data_secret=$(kubectl get machine "${machine}" -n "${NAMESPACE}" -o jsonpath='{.spec.bootstrap.dataSecretName}' || true)
  config_ref=$(kubectl get machine "${machine}" -n "${NAMESPACE}" -o jsonpath='{.spec.bootstrap.configRef.name}' || true)

  echo "dataSecretName: ${data_secret:-<empty>}"
  echo "KairosConfig: ${config_ref:-<empty>}"

  if [ -n "${config_ref}" ]; then
    kc_data_secret=$(kubectl get kairosconfig "${config_ref}" -n "${NAMESPACE}" -o jsonpath='{.status.dataSecretName}' || true)
    echo "KairosConfig.status.dataSecretName: ${kc_data_secret:-<empty>}"
  fi

  if [ -n "${data_secret}" ]; then
    echo "--- cloud-config (first 200 lines) ---"
    kubectl get secret "${data_secret}" -n "${NAMESPACE}" -o jsonpath='{.data.value}' | base64 -d | sed -n '1,200p'
    echo "--- end cloud-config ---"
  else
    echo "No dataSecretName set on Machine."
  fi

  echo
done
