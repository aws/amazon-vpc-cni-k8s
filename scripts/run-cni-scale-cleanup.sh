#!/usr/bin/env bash

# Deletes the final CNI scale workload and publishes its completion contract.
# Repeated calls are a no-op after the first successful cleanup.

set -euo pipefail

SCALE_TEST_NAMESPACE_PREFIX=${SCALE_TEST_NAMESPACE_PREFIX:-cni-scale}
SCALE_TEST_TIMEOUT_SECONDS=${SCALE_TEST_TIMEOUT_SECONDS:-1800}
namespace="${SCALE_TEST_NAMESPACE_PREFIX}-1"
state_dir=${SCENARIO_STATE_DIR:-log}
CNI_WORKLOAD_STATE_FILE="${state_dir}/cni-scale-workload.state"
script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
source "${script_dir}/lib/workload-report.sh"

if [[ -f $CNI_WORKLOAD_STATE_FILE ]]; then
  # The workload writes shell-escaped scalar assignments only.
  source "$CNI_WORKLOAD_STATE_FILE"
fi

WORKLOAD_PROFILE_ID=${WORKLOAD_PROFILE_ID:-cni-scale-churn-2000-v1}
WORKLOAD_GIT_REVISION=${WORKLOAD_GIT_REVISION:-}
WORKLOAD_REPORT_PATH=${WORKLOAD_REPORT_PATH:-"${state_dir}/workload-report.json"}
WORKLOAD_STARTED_AT=${WORKLOAD_STARTED_AT:-}
WORKLOAD_COMPLETED_AT=${WORKLOAD_COMPLETED_AT:-}
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
WORKLOAD_CLEANUP_STATUS=${WORKLOAD_CLEANUP_STATUS:-pending}

if [[ $WORKLOAD_CLEANUP_STATUS == pass ]]; then
  if [[ ! -s $WORKLOAD_REPORT_PATH ]]; then
    cni_write_workload_report
  fi
  printf 'CNI scale workload was already cleaned up\n'
  exit 0
fi

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

validation_failed=0
expected_churn_operations=$((WORKLOAD_ROUNDS_REQUESTED * WORKLOAD_CHURN_PODS_PER_ROUND))
expected_total_operations=$((WORKLOAD_REQUESTED_PODS + expected_churn_operations))
minimum_duration=$((WORKLOAD_ROUNDS_REQUESTED * WORKLOAD_CHURN_INTERVAL_SECONDS))

require_equal() {
  local name=$1
  local got=$2
  local want=$3
  if [[ $got != "$want" ]]; then
    printf 'Workload completion mismatch: %s=%s, want %s\n' \
      "$name" "$got" "$want" >&2
    validation_failed=1
  fi
}

require_equal workloadStatus "$WORKLOAD_STATUS" pass
require_equal readyPods "$WORKLOAD_READY_PODS" "$WORKLOAD_REQUESTED_PODS"
require_equal roundsCompleted "$WORKLOAD_ROUNDS_COMPLETED" "$WORKLOAD_ROUNDS_REQUESTED"
require_equal createdPods "$WORKLOAD_CREATED_PODS" "$expected_total_operations"
require_equal deletedPods "$WORKLOAD_DELETED_PODS" "$expected_total_operations"
require_equal connectivity "$WORKLOAD_CONNECTIVITY_STATUS" pass
require_equal uniquePodIPs "$WORKLOAD_UNIQUE_IP_STATUS" pass
if ((WORKLOAD_DURATION_SECONDS < minimum_duration)); then
  printf 'Workload duration %s seconds is below required %s seconds\n' \
    "$WORKLOAD_DURATION_SECONDS" "$minimum_duration" >&2
  validation_failed=1
fi

cni_write_workload_state
cni_write_workload_report

if ((validation_failed != 0)); then
  exit 1
fi
printf 'CNI scale workload cleanup and completion validation passed\n'
