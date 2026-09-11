#!/usr/bin/env bash

# Creates a paced, scheduler-distributed CNI scale workload on an existing
# cluster. The caller owns cluster creation, CNI installation, metric capture,
# workload cleanup, and cluster deletion.

set -euo pipefail

CL2_BIN=${CL2_BIN:-clusterloader2}
SCALE_TEST_NAMESPACE_PREFIX=${SCALE_TEST_NAMESPACE_PREFIX:-cni-scale}
SCALE_TEST_PODS=${SCALE_TEST_PODS:-2000}
SCALE_TEST_POD_THROUGHPUT=${SCALE_TEST_POD_THROUGHPUT:-20}
SCALE_TEST_TIMEOUT_SECONDS=${SCALE_TEST_TIMEOUT_SECONDS:-1800}
TEST_IMAGE_REGISTRY=${TEST_IMAGE_REGISTRY:-617930562442.dkr.ecr.us-west-2.amazonaws.com}
SCALE_TEST_POD_IMAGE=${SCALE_TEST_POD_IMAGE:-${TEST_IMAGE_REGISTRY}/networking-e2e-test-images/busybox:latest}

if [[ -n ${KUBE_CONFIG_PATH:-} && -z ${KUBECONFIG:-} ]]; then
  export KUBECONFIG=$KUBE_CONFIG_PATH
fi
KUBECONFIG=${KUBECONFIG:-"${HOME:?HOME must be set}/.kube/config"}
export KUBECONFIG

validate_positive_integer() {
  local name=$1
  local value=$2
  if [[ ! $value =~ ^[1-9][0-9]*$ ]]; then
    printf '%s must be a positive integer; got %q\n' "$name" "$value" >&2
    exit 2
  fi
}

for name in SCALE_TEST_PODS SCALE_TEST_POD_THROUGHPUT SCALE_TEST_TIMEOUT_SECONDS; do
  validate_positive_integer "$name" "${!name}"
done
for command in "$CL2_BIN" kubectl sha256sum; do
  command -v "$command" >/dev/null 2>&1 || {
    printf '%s is required\n' "$command" >&2
    exit 127
  }
done

script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
clusterloader_path=$(command -v "$CL2_BIN")
clusterloader_sha256=$(sha256sum "$clusterloader_path")
clusterloader_sha256=${clusterloader_sha256%% *}
safe_cluster_name=${CLUSTER_NAME:-local}
safe_cluster_name=${safe_cluster_name//[^a-zA-Z0-9_.-]/-}
report_dir=${SCALE_TEST_REPORT_DIR:-"log/clusterloader2-${safe_cluster_name}"}
mkdir -p "$report_dir"

print_diagnostics_on_failure() {
  local status=$?
  trap - EXIT
  if ((status != 0)); then
    printf 'ClusterLoader2 scale workload failed; collecting pod diagnostics\n' >&2
    kubectl --kubeconfig "$KUBECONFIG" get pods \
      --all-namespaces \
      --selector group=cni-scale \
      --output wide || true
  fi
  exit "$status"
}
trap print_diagnostics_on_failure EXIT

cat >"${report_dir}/metadata.txt" <<EOF
clusterloader2_path=${clusterloader_path}
clusterloader2_sha256=${clusterloader_sha256}
pod_count=${SCALE_TEST_PODS}
pod_throughput=${SCALE_TEST_POD_THROUGHPUT}
namespace=${SCALE_TEST_NAMESPACE_PREFIX}-1
EOF

export NAMESPACE_PREFIX=$SCALE_TEST_NAMESPACE_PREFIX
export OPERATION_TIMEOUT="${SCALE_TEST_TIMEOUT_SECONDS}s"
export POD_COUNT=$SCALE_TEST_PODS
export POD_IMAGE=$SCALE_TEST_POD_IMAGE
export POD_THROUGHPUT=$SCALE_TEST_POD_THROUGHPUT

printf 'Creating %s CNI scale pods at %s pods/s with ClusterLoader2 %s\n' \
  "$SCALE_TEST_PODS" "$SCALE_TEST_POD_THROUGHPUT" "$clusterloader_sha256"

"$CL2_BIN" \
  -v=2 \
  --testconfig="${script_dir}/scale/cl2-config.yaml" \
  --provider=eks \
  --enable-exec-service=false \
  --report-dir="$report_dir" \
  --kubeconfig="$KUBECONFIG" \
  2>&1 | tee "${report_dir}/clusterloader2.log"

printf 'CNI scale workload reached %s RunningAndReady pods\n' "$SCALE_TEST_PODS"
