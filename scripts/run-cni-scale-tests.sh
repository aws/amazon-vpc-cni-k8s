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
SCALE_TEST_CHURN_ROUNDS=${SCALE_TEST_CHURN_ROUNDS:-12}
SCALE_TEST_CHURN_PODS=${SCALE_TEST_CHURN_PODS:-200}
SCALE_TEST_CHURN_INTERVAL_SECONDS=${SCALE_TEST_CHURN_INTERVAL_SECONDS:-300}
TEST_IMAGE_REGISTRY=${TEST_IMAGE_REGISTRY:-617930562442.dkr.ecr.us-west-2.amazonaws.com}
SCALE_TEST_POD_IMAGE=${SCALE_TEST_POD_IMAGE:-${TEST_IMAGE_REGISTRY}/networking-e2e-test-images/busybox:latest}
WORKLOAD_PROFILE_ID=${WORKLOAD_PROFILE_ID:-cni-scale-churn-2000-v1}

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
for command in "$CL2_BIN" kubectl sha256sum sort wc head sed; do
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
state_dir=${SCENARIO_STATE_DIR:-$report_dir}
state_file="${state_dir}/cni-scale-workload.state"
mkdir -p "$state_dir"

WORKLOAD_STARTED_AT=$(date -u +%Y-%m-%dT%H:%M:%SZ)
WORKLOAD_REQUESTED_PODS=$SCALE_TEST_PODS
WORKLOAD_READY_PODS=0
WORKLOAD_CREATED_PODS=0
WORKLOAD_DELETED_PODS=0
WORKLOAD_POD_THROUGHPUT=$SCALE_TEST_POD_THROUGHPUT
WORKLOAD_CHURN_PODS_PER_ROUND=$SCALE_TEST_CHURN_PODS
WORKLOAD_CHURN_INTERVAL_SECONDS=$SCALE_TEST_CHURN_INTERVAL_SECONDS
WORKLOAD_DURATION_SECONDS=0
WORKLOAD_ROUNDS_REQUESTED=$SCALE_TEST_CHURN_ROUNDS
WORKLOAD_ROUNDS_COMPLETED=0
WORKLOAD_CONNECTIVITY_STATUS=unknown
WORKLOAD_UNIQUE_IP_STATUS=unknown
WORKLOAD_STATUS=running

persist_state() {
  local temporary="${state_file}.tmp.$$"
  {
    printf 'WORKLOAD_PROFILE_ID=%q\n' "$WORKLOAD_PROFILE_ID"
    printf 'TEST_SCENARIO_ID=%q\n' "${TEST_SCENARIO_ID:-}"
    printf 'WORKLOAD_REPORT_PATH=%q\n' "${WORKLOAD_REPORT_PATH:-${report_dir}/workload-report.json}"
    printf 'WORKLOAD_STARTED_AT=%q\n' "$WORKLOAD_STARTED_AT"
    printf 'WORKLOAD_REQUESTED_PODS=%q\n' "$WORKLOAD_REQUESTED_PODS"
    printf 'WORKLOAD_READY_PODS=%q\n' "$WORKLOAD_READY_PODS"
    printf 'WORKLOAD_CREATED_PODS=%q\n' "$WORKLOAD_CREATED_PODS"
    printf 'WORKLOAD_DELETED_PODS=%q\n' "$WORKLOAD_DELETED_PODS"
    printf 'WORKLOAD_POD_THROUGHPUT=%q\n' "$WORKLOAD_POD_THROUGHPUT"
    printf 'WORKLOAD_CHURN_PODS_PER_ROUND=%q\n' "$WORKLOAD_CHURN_PODS_PER_ROUND"
    printf 'WORKLOAD_CHURN_INTERVAL_SECONDS=%q\n' "$WORKLOAD_CHURN_INTERVAL_SECONDS"
    printf 'WORKLOAD_DURATION_SECONDS=%q\n' "$WORKLOAD_DURATION_SECONDS"
    printf 'WORKLOAD_ROUNDS_REQUESTED=%q\n' "$WORKLOAD_ROUNDS_REQUESTED"
    printf 'WORKLOAD_ROUNDS_COMPLETED=%q\n' "$WORKLOAD_ROUNDS_COMPLETED"
    printf 'WORKLOAD_CONNECTIVITY_STATUS=%q\n' "$WORKLOAD_CONNECTIVITY_STATUS"
    printf 'WORKLOAD_UNIQUE_IP_STATUS=%q\n' "$WORKLOAD_UNIQUE_IP_STATUS"
    printf 'WORKLOAD_STATUS=%q\n' "$WORKLOAD_STATUS"
    printf 'SCALE_TEST_NAMESPACE_PREFIX=%q\n' "$SCALE_TEST_NAMESPACE_PREFIX"
  } >"$temporary"
  mv -f -- "$temporary" "$state_file"
}

KUBECTL=(kubectl --kubeconfig "$KUBECONFIG")
namespace="${SCALE_TEST_NAMESPACE_PREFIX}-1"

print_diagnostics_on_failure() {
  local status=$?
  trap - EXIT
  if ((status != 0)); then
    WORKLOAD_STATUS=failed
    persist_state
    printf 'ClusterLoader2 scale workload failed; collecting pod diagnostics\n' >&2
    "${KUBECTL[@]}" get pods \
      --all-namespaces \
      --selector group=cni-scale \
      --output wide || true
  fi
  exit "$status"
}
trap print_diagnostics_on_failure EXIT
persist_state

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

verify_ready_and_unique_ips() {
  "${KUBECTL[@]}" wait pod \
    --namespace "$namespace" \
    --selector group=cni-scale \
    --for=condition=Ready \
    --timeout="${SCALE_TEST_TIMEOUT_SECONDS}s"

  local pod_count
  local unique_ip_count
  pod_count=$("${KUBECTL[@]}" get pods \
    --namespace "$namespace" \
    --selector group=cni-scale \
    --output jsonpath='{range .items[*]}{.status.podIP}{"\n"}{end}' |
    sed '/^$/d' |
    wc -l)
  unique_ip_count=$("${KUBECTL[@]}" get pods \
    --namespace "$namespace" \
    --selector group=cni-scale \
    --output jsonpath='{range .items[*]}{.status.podIP}{"\n"}{end}' |
    sed '/^$/d' |
    sort -u |
    wc -l)
  if ((pod_count != SCALE_TEST_PODS || unique_ip_count != SCALE_TEST_PODS)); then
    printf 'CNI scale pod/IP count mismatch: pods=%s uniqueIPs=%s expected=%s\n' \
      "$pod_count" "$unique_ip_count" "$SCALE_TEST_PODS" >&2
    return 1
  fi
  WORKLOAD_READY_PODS=$pod_count
  WORKLOAD_UNIQUE_IP_STATUS=pass
}

verify_cross_node_connectivity() {
  local source_pod=
  local source_node=
  local target_ip=
  local pod
  local node
  local ip
  while IFS=$'\t' read -r pod node ip; do
    [[ -n $pod && -n $node && -n $ip ]] || continue
    if [[ -z $source_pod ]]; then
      source_pod=$pod
      source_node=$node
      continue
    fi
    if [[ $node != "$source_node" ]]; then
      target_ip=$ip
      break
    fi
  done < <("${KUBECTL[@]}" get pods \
    --namespace "$namespace" \
    --selector group=cni-scale \
    --output jsonpath='{range .items[*]}{.metadata.name}{"\t"}{.spec.nodeName}{"\t"}{.status.podIP}{"\n"}{end}')

  if [[ -z $source_pod || -z $target_ip ]]; then
    printf 'Could not find ready CNI scale pods on two different nodes\n' >&2
    return 1
  fi
  "${KUBECTL[@]}" exec \
    --namespace "$namespace" \
    "$source_pod" \
    -- ping -c 1 -W 3 "$target_ip" >/dev/null
  WORKLOAD_CONNECTIVITY_STATUS=pass
}

create_replacement_batch() {
  local round=$1
  local first=$2
  local last=$3
  {
    printf 'apiVersion: v1\nkind: List\nitems:\n'
    local sequence
    for ((sequence = first; sequence <= last; sequence++)); do
      cat <<EOF
- apiVersion: v1
  kind: Pod
  metadata:
    name: cni-scale-r${round}-${sequence}
    namespace: ${namespace}
    labels:
      group: cni-scale
      cni-scale-round: "${round}"
  spec:
    automountServiceAccountToken: false
    nodeSelector:
      kubernetes.io/os: linux
    terminationGracePeriodSeconds: 0
    topologySpreadConstraints:
    - labelSelector:
        matchLabels:
          group: cni-scale
      maxSkew: 1
      topologyKey: kubernetes.io/hostname
      whenUnsatisfiable: ScheduleAnyway
    containers:
    - name: workload
      image: ${SCALE_TEST_POD_IMAGE}
      imagePullPolicy: IfNotPresent
      command: ["sleep", "604800"]
      resources:
        requests:
          cpu: 5m
          memory: 8Mi
EOF
    done
  } | "${KUBECTL[@]}" create --filename -
}

replace_scale_pods() {
  local round=$1
  local victims=()
  local victim
  while IFS= read -r victim; do
    [[ -n $victim ]] && victims+=("$victim")
  done < <("${KUBECTL[@]}" get pods \
    --namespace "$namespace" \
    --selector group=cni-scale \
    --sort-by=.metadata.name \
    --output name |
    head -n "$SCALE_TEST_CHURN_PODS")
  if ((${#victims[@]} != SCALE_TEST_CHURN_PODS)); then
    printf 'Found %s churn victims, want %s\n' \
      "${#victims[@]}" "$SCALE_TEST_CHURN_PODS" >&2
    return 1
  fi
  "${KUBECTL[@]}" delete \
    --namespace "$namespace" \
    --wait=true \
    --timeout="${SCALE_TEST_TIMEOUT_SECONDS}s" \
    "${victims[@]}"
  WORKLOAD_DELETED_PODS=$((WORKLOAD_DELETED_PODS + SCALE_TEST_CHURN_PODS))

  local first=1
  while ((first <= SCALE_TEST_CHURN_PODS)); do
    local last=$((first + SCALE_TEST_POD_THROUGHPUT - 1))
    if ((last > SCALE_TEST_CHURN_PODS)); then
      last=$SCALE_TEST_CHURN_PODS
    fi
    create_replacement_batch "$round" "$first" "$last"
    WORKLOAD_CREATED_PODS=$((WORKLOAD_CREATED_PODS + last - first + 1))
    first=$((last + 1))
    if ((first <= SCALE_TEST_CHURN_PODS)); then
      sleep 1
    fi
  done
}

WORKLOAD_CREATED_PODS=$SCALE_TEST_PODS
verify_ready_and_unique_ips
verify_cross_node_connectivity
printf 'CNI scale workload reached %s RunningAndReady pods\n' "$SCALE_TEST_PODS"

churn_started=$SECONDS
for ((round = 1; round <= SCALE_TEST_CHURN_ROUNDS; round++)); do
  printf 'CNI scale churn round %s/%s: replacing %s pods\n' \
    "$round" "$SCALE_TEST_CHURN_ROUNDS" "$SCALE_TEST_CHURN_PODS"
  replace_scale_pods "$round"
  verify_ready_and_unique_ips
  verify_cross_node_connectivity
  WORKLOAD_ROUNDS_COMPLETED=$round
  persist_state

  next_round=$((churn_started + round * SCALE_TEST_CHURN_INTERVAL_SECONDS))
  remaining=$((next_round - SECONDS))
  if ((remaining > 0)); then
    sleep "$remaining"
  fi
done

WORKLOAD_DURATION_SECONDS=$((SECONDS - churn_started))
WORKLOAD_STATUS=pass
persist_state
printf 'CNI scale workload completed %s churn rounds successfully\n' \
  "$SCALE_TEST_CHURN_ROUNDS"
