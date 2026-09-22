#!/usr/bin/env bash

# Runs one CNI scale profile against an existing cluster. The workload module
# lives in this repository; Hydra may supply a component-owned ClusterLoader2
# profile and monitor set through CL2_PROFILE_PATH and CL2_MONITORS_PATH.

set -euo pipefail

CL2_BIN=${CL2_BIN:-clusterloader2}
CL2_EXPECTED_SHA256=${CL2_EXPECTED_SHA256:-}
CL2_UPSTREAM_REVISION=${CL2_UPSTREAM_REVISION:-unknown}
CL2_DRY_RUN=${CL2_DRY_RUN:-false}
SCALE_TEST_NAMESPACE_PREFIX=${SCALE_TEST_NAMESPACE_PREFIX:-cni-scale}
SCALE_TEST_PODS=${SCALE_TEST_PODS:-2000}
SCALE_TEST_POD_THROUGHPUT=${SCALE_TEST_POD_THROUGHPUT:-20}
SCALE_TEST_TIMEOUT_SECONDS=${SCALE_TEST_TIMEOUT_SECONDS:-1800}
SCALE_TEST_CHURN_ROUNDS=${SCALE_TEST_CHURN_ROUNDS:-12}
SCALE_TEST_CHURN_PODS=${SCALE_TEST_CHURN_PODS:-200}
SCALE_TEST_CHURN_INTERVAL_SECONDS=${SCALE_TEST_CHURN_INTERVAL_SECONDS:-300}
SCALE_TEST_BASELINE_SETTLE=${SCALE_TEST_BASELINE_SETTLE:-60s}
SCALE_TEST_SCRAPE_SETTLE=${SCALE_TEST_SCRAPE_SETTLE:-60s}
SCALE_TEST_RECOVERY_DELAY=${SCALE_TEST_RECOVERY_DELAY:-5m}
SCALE_TEST_WORKLOAD_TIMEOUT=${SCALE_TEST_WORKLOAD_TIMEOUT:-95m}
SCALE_TEST_CLEANUP_TIMEOUT=${SCALE_TEST_CLEANUP_TIMEOUT:-30m}
TEST_IMAGE_REGISTRY=${TEST_IMAGE_REGISTRY:-617930562442.dkr.ecr.us-west-2.amazonaws.com}
SCALE_TEST_POD_IMAGE=${SCALE_TEST_POD_IMAGE:-${TEST_IMAGE_REGISTRY}/networking-e2e-test-images/busybox:latest}
WORKLOAD_PROFILE_ID=${WORKLOAD_PROFILE_ID:-cni-scale-churn-2000-v1}

validate_positive_integer() {
  local name=$1
  local value=$2
  if [[ ! $value =~ ^[1-9][0-9]*$ ]]; then
    printf '%s must be a positive integer; got %q\n' "$name" "$value" >&2
    exit 2
  fi
}

for name in \
  SCALE_TEST_PODS \
  SCALE_TEST_POD_THROUGHPUT \
  SCALE_TEST_TIMEOUT_SECONDS \
  SCALE_TEST_CHURN_ROUNDS \
  SCALE_TEST_CHURN_PODS \
  SCALE_TEST_CHURN_INTERVAL_SECONDS; do
  validate_positive_integer "$name" "${!name}"
done
if ((SCALE_TEST_CHURN_PODS > SCALE_TEST_PODS)); then
  printf 'SCALE_TEST_CHURN_PODS must not exceed SCALE_TEST_PODS\n' >&2
  exit 2
fi
if [[ $WORKLOAD_PROFILE_ID != cni-scale-churn-2000-v1 ]]; then
  printf 'Unsupported CNI scale workload profile %q\n' "$WORKLOAD_PROFILE_ID" >&2
  exit 2
fi
if [[ $CL2_DRY_RUN != true && $CL2_DRY_RUN != false ]]; then
  printf 'CL2_DRY_RUN must be true or false; got %q\n' "$CL2_DRY_RUN" >&2
  exit 2
fi
for command in "$CL2_BIN" grep realpath sha256sum tee; do
  command -v "$command" >/dev/null 2>&1 || {
    printf '%s is required\n' "$command" >&2
    exit 127
  }
done

script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
CL2_PROFILE_PATH=${CL2_PROFILE_PATH:-"${script_dir}/scale/cl2-config.yaml"}
if [[ ! -f $CL2_PROFILE_PATH ]]; then
  printf 'ClusterLoader2 profile does not exist: %s\n' "$CL2_PROFILE_PATH" >&2
  exit 2
fi
CL2_PROFILE_PATH=$(realpath "$CL2_PROFILE_PATH")

if [[ -n ${CL2_MONITORS_PATH:-} ]]; then
  if [[ ! -d $CL2_MONITORS_PATH ]]; then
    printf 'ClusterLoader2 monitor directory does not exist: %s\n' \
      "$CL2_MONITORS_PATH" >&2
    exit 2
  fi
  CL2_MONITORS_PATH=$(realpath "$CL2_MONITORS_PATH")
fi

clusterloader_path=$(command -v "$CL2_BIN")
clusterloader_sha256=$(sha256sum "$clusterloader_path")
clusterloader_sha256=${clusterloader_sha256%% *}
if [[ -n $CL2_EXPECTED_SHA256 && $clusterloader_sha256 != "$CL2_EXPECTED_SHA256" ]]; then
  printf 'ClusterLoader2 SHA-256 mismatch: got %s, want %s\n' \
    "$clusterloader_sha256" "$CL2_EXPECTED_SHA256" >&2
  exit 2
fi
cl2_help=$("$CL2_BIN" --help 2>&1 || true)
for required_flag in \
  --dry-run \
  --enable-prometheus-server \
  --prometheus-additional-monitors-path \
  --tear-down-prometheus-server; do
  if ! grep -q -- "$required_flag" <<<"$cl2_help"; then
    printf '%s does not expose required flag %s\n' "$CL2_BIN" "$required_flag" >&2
    exit 2
  fi
done

if [[ -n ${KUBE_CONFIG_PATH:-} && -z ${KUBECONFIG:-} ]]; then
  export KUBECONFIG=$KUBE_CONFIG_PATH
fi
if [[ $CL2_DRY_RUN == false ]]; then
  KUBECONFIG=${KUBECONFIG:-"${HOME:?HOME must be set}/.kube/config"}
else
  KUBECONFIG=${KUBECONFIG:-"${script_dir}/scale/kubeconfig.dry-run.yaml"}
fi
export KUBECONFIG

profile_dir=$(dirname -- "$CL2_PROFILE_PATH")
export CL2_CNI_LOAD_MODULE_PATH
CL2_CNI_LOAD_MODULE_PATH=$(realpath --relative-to="$profile_dir" \
  "${script_dir}/scale/cni-load-module.yaml")
export CL2_CNI_WORKLOAD_SCRIPT
CL2_CNI_WORKLOAD_SCRIPT=$(realpath "${script_dir}/run-cni-scale-workload.sh")
export CL2_CNI_CLEANUP_SCRIPT
CL2_CNI_CLEANUP_SCRIPT=$(realpath "${script_dir}/run-cni-scale-cleanup.sh")
export CL2_CNI_POD_TEMPLATE_PATH
CL2_CNI_POD_TEMPLATE_PATH=$(realpath --relative-to="$profile_dir" \
  "${script_dir}/scale/pod.yaml")

export CL2_CNI_NAMESPACE_PREFIX=$SCALE_TEST_NAMESPACE_PREFIX
export CL2_CNI_OPERATION_TIMEOUT="${SCALE_TEST_TIMEOUT_SECONDS}s"
export CL2_CNI_POD_COUNT=$SCALE_TEST_PODS
export CL2_CNI_POD_IMAGE=$SCALE_TEST_POD_IMAGE
export CL2_CNI_POD_THROUGHPUT=$SCALE_TEST_POD_THROUGHPUT
export CL2_CNI_WORKLOAD_TIMEOUT=$SCALE_TEST_WORKLOAD_TIMEOUT
export CL2_CNI_CLEANUP_TIMEOUT=$SCALE_TEST_CLEANUP_TIMEOUT
export CL2_EXPECTED_LINUX_NODES=${CL2_EXPECTED_LINUX_NODES:-${EXPECTED_LINUX_NODES:-1}}
export CL2_CNI_BASELINE_SETTLE=$SCALE_TEST_BASELINE_SETTLE
export CL2_CNI_SCRAPE_SETTLE=$SCALE_TEST_SCRAPE_SETTLE
export CL2_CNI_RECOVERY_DELAY=$SCALE_TEST_RECOVERY_DELAY
export CL2_CNI_EXPECTED_SETUP_OPERATIONS=$((SCALE_TEST_PODS + SCALE_TEST_CHURN_ROUNDS * SCALE_TEST_CHURN_PODS))
export CL2_CNI_EXPECTED_CHURN_DELETES=$((SCALE_TEST_CHURN_ROUNDS * SCALE_TEST_CHURN_PODS))
export CL2_CNI_EXPECTED_TOTAL_DELETES=$CL2_CNI_EXPECTED_SETUP_OPERATIONS

# Avoid requiring a cluster-specific persistent volume for an ephemeral test.
export CL2_PROMETHEUS_PVC_ENABLED=false
export PROMETHEUS_STORAGE_CLASS_PROVISIONER=ebs.csi.aws.com
export PROMETHEUS_STORAGE_CLASS_VOLUME_TYPE=gp3
export CL2_PROMETHEUS_KUBELET_MEMORY_SCALE_FACTOR=${CL2_PROMETHEUS_KUBELET_MEMORY_SCALE_FACTOR:-4}

safe_cluster_name=${CLUSTER_NAME:-local}
safe_cluster_name=${safe_cluster_name//[^a-zA-Z0-9_.-]/-}
artifact_root=${ARTIFACT_DIR:-log}
report_dir=${SCALE_TEST_REPORT_DIR:-"${artifact_root}/clusterloader2-${safe_cluster_name}"}
mkdir -p "$report_dir"

cat >"${report_dir}/metadata.txt" <<EOF
scenario_id=${TEST_SCENARIO_ID:-manual}
workload_profile_id=${WORKLOAD_PROFILE_ID}
workload_revision=${WORKLOAD_GIT_REVISION:-unknown}
clusterloader2_path=${clusterloader_path}
clusterloader2_sha256=${clusterloader_sha256}
clusterloader2_upstream_revision=${CL2_UPSTREAM_REVISION}
profile_path=${CL2_PROFILE_PATH}
monitors_path=${CL2_MONITORS_PATH:-none}
pod_count=${SCALE_TEST_PODS}
pod_throughput=${SCALE_TEST_POD_THROUGHPUT}
churn_rounds=${SCALE_TEST_CHURN_ROUNDS}
churn_pods=${SCALE_TEST_CHURN_PODS}
churn_interval_seconds=${SCALE_TEST_CHURN_INTERVAL_SECONDS}
EOF

cl2_args=(
  -v=2
  "--testconfig=${CL2_PROFILE_PATH}"
  --provider=eks
  "--nodes=${CL2_EXPECTED_LINUX_NODES}"
  --enable-exec-service=false
  "--report-dir=${report_dir}"
  "--kubeconfig=${KUBECONFIG}"
)
if [[ -n ${CL2_MONITORS_PATH:-} ]]; then
  cl2_args+=(
    --enable-prometheus-server=true
    --tear-down-prometheus-server=true
    --prometheus-scrape-kube-proxy=false
    --prometheus-scrape-kubelets=true
    "--prometheus-additional-monitors-path=${CL2_MONITORS_PATH}"
  )
fi
if [[ $CL2_DRY_RUN == true ]]; then
  cl2_args+=(
    --dry-run=true
    --skip-cluster-verification=true
  )
fi

printf 'Running CNI scale profile %s with ClusterLoader2 %s\n' \
  "$CL2_PROFILE_PATH" "$clusterloader_sha256"
if [[ $CL2_DRY_RUN == true ]]; then
  set +e
  "$CL2_BIN" "${cl2_args[@]}" 2>&1 | tee "${report_dir}/clusterloader2.log"
  clusterloader_status=${PIPESTATUS[0]}
  set -e
  generated_configs=("${report_dir}"/generatedConfig_*.yaml)
  if [[ ! -s ${generated_configs[0]} ]]; then
    printf 'ClusterLoader2 dry run produced no generated config (status %s)\n' \
      "$clusterloader_status" >&2
    exit 1
  fi
  printf 'ClusterLoader2 compiled the CNI scale profile: %s\n' \
    "${generated_configs[0]}"
  exit 0
fi

"$CL2_BIN" "${cl2_args[@]}" 2>&1 | tee "${report_dir}/clusterloader2.log"

printf 'CNI scale profile passed; reports: %s\n' "$report_dir"
