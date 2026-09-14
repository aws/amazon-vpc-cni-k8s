#!/usr/bin/env bash

# Deletes the workload left running by run-cni-scale-tests.sh so the caller can
# capture full-load metrics before cleanup and recovery metrics afterwards.

set -euo pipefail

SCALE_TEST_NAMESPACE_PREFIX=${SCALE_TEST_NAMESPACE_PREFIX:-cni-scale}
SCALE_TEST_TIMEOUT_SECONDS=${SCALE_TEST_TIMEOUT_SECONDS:-1800}
namespace="${SCALE_TEST_NAMESPACE_PREFIX}-1"
state_dir=${SCENARIO_STATE_DIR:-log}
state_file="${state_dir}/cni-scale-workload.state"
script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
source "${script_dir}/lib/workload-report.sh"

if [[ -f $state_file ]]; then
  # The workload writes shell-escaped scalar assignments only.
  source "$state_file"
fi

WORKLOAD_PROFILE_ID=${WORKLOAD_PROFILE_ID:-cni-scale-churn-2000-v1}
WORKLOAD_REPORT_PATH=${WORKLOAD_REPORT_PATH:-"${state_dir}/workload-report.json"}
WORKLOAD_STARTED_AT=${WORKLOAD_STARTED_AT:-}
WORKLOAD_REQUESTED_PODS=${WORKLOAD_REQUESTED_PODS:-0}
WORKLOAD_READY_PODS=${WORKLOAD_READY_PODS:-0}
WORKLOAD_CREATED_PODS=${WORKLOAD_CREATED_PODS:-0}
WORKLOAD_DELETED_PODS=${WORKLOAD_DELETED_PODS:-0}
WORKLOAD_POD_THROUGHPUT=${WORKLOAD_POD_THROUGHPUT:-0}
WORKLOAD_CHURN_PODS_PER_ROUND=${WORKLOAD_CHURN_PODS_PER_ROUND:-0}
WORKLOAD_CHURN_INTERVAL_SECONDS=${WORKLOAD_CHURN_INTERVAL_SECONDS:-0}
WORKLOAD_DURATION_SECONDS=${WORKLOAD_DURATION_SECONDS:-0}
WORKLOAD_ROUNDS_REQUESTED=${WORKLOAD_ROUNDS_REQUESTED:-0}
WORKLOAD_ROUNDS_COMPLETED=${WORKLOAD_ROUNDS_COMPLETED:-0}
WORKLOAD_CONNECTIVITY_STATUS=${WORKLOAD_CONNECTIVITY_STATUS:-unknown}
WORKLOAD_UNIQUE_IP_STATUS=${WORKLOAD_UNIQUE_IP_STATUS:-unknown}
WORKLOAD_STATUS=${WORKLOAD_STATUS:-unknown}
WORKLOAD_CLEANUP_STATUS=failed

if [[ -n ${KUBE_CONFIG_PATH:-} && -z ${KUBECONFIG:-} ]]; then
  export KUBECONFIG=$KUBE_CONFIG_PATH
fi
KUBECONFIG=${KUBECONFIG:-"${HOME:?HOME must be set}/.kube/config"}
export KUBECONFIG

if [[ ! $SCALE_TEST_TIMEOUT_SECONDS =~ ^[1-9][0-9]*$ ]]; then
  printf 'SCALE_TEST_TIMEOUT_SECONDS must be a positive integer; got %q\n' \
    "$SCALE_TEST_TIMEOUT_SECONDS" >&2
  exit 2
fi
command -v kubectl >/dev/null 2>&1 || {
  printf 'kubectl is required\n' >&2
  exit 127
}

remaining_pods=$(
  {
    kubectl --kubeconfig "$KUBECONFIG" get pods \
      --namespace "$namespace" \
      --selector group=cni-scale \
      --output name 2>/dev/null || true
  } | wc -l
)

printf 'Deleting CNI scale workload namespace %s\n' "$namespace"
kubectl --kubeconfig "$KUBECONFIG" delete namespace "$namespace" \
  --ignore-not-found=true \
  --wait=true \
  --timeout="${SCALE_TEST_TIMEOUT_SECONDS}s"

if kubectl --kubeconfig "$KUBECONFIG" get namespace "$namespace" >/dev/null 2>&1; then
  printf 'Namespace %s still exists after cleanup\n' "$namespace" >&2
  exit 1
fi

WORKLOAD_DELETED_PODS=$((WORKLOAD_DELETED_PODS + remaining_pods))
WORKLOAD_COMPLETED_AT=$(date -u +%Y-%m-%dT%H:%M:%SZ)
WORKLOAD_CLEANUP_STATUS=pass
cni_write_workload_report

printf 'CNI scale workload cleanup completed\n'
