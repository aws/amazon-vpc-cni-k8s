#!/usr/bin/env bash

# Verifies and churns the CNI pods created by the ClusterLoader2 load module.
# It leaves the final pod set running so the caller can gather full-load
# metrics before invoking run-cni-scale-cleanup.sh.

set -euo pipefail

SCALE_TEST_NAMESPACE_PREFIX=${SCALE_TEST_NAMESPACE_PREFIX:-${CL2_CNI_NAMESPACE_PREFIX:-cni-scale}}
SCALE_TEST_PODS=${SCALE_TEST_PODS:-${CL2_CNI_POD_COUNT:-2000}}
SCALE_TEST_POD_THROUGHPUT=${SCALE_TEST_POD_THROUGHPUT:-${CL2_CNI_POD_THROUGHPUT:-20}}
SCALE_TEST_TIMEOUT_SECONDS=${SCALE_TEST_TIMEOUT_SECONDS:-1800}
SCALE_TEST_CHURN_ROUNDS=${SCALE_TEST_CHURN_ROUNDS:-12}
SCALE_TEST_CHURN_PODS=${SCALE_TEST_CHURN_PODS:-200}
SCALE_TEST_CHURN_INTERVAL_SECONDS=${SCALE_TEST_CHURN_INTERVAL_SECONDS:-300}
SCALE_TEST_POD_IMAGE=${SCALE_TEST_POD_IMAGE:-${CL2_CNI_POD_IMAGE:-public.ecr.aws/docker/library/busybox:1.36@sha256:73aaf090f3d85aa34ee199857f03fa3a95c8ede2ffd4cc2cdb5b94e566b11662}}
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
: "${CL2_EXPECTED_LINUX_NODES:?CL2_EXPECTED_LINUX_NODES must identify the scale cluster size}"
validate_positive_integer CL2_EXPECTED_LINUX_NODES "$CL2_EXPECTED_LINUX_NODES"
if ((SCALE_TEST_CHURN_PODS > SCALE_TEST_PODS)); then
  printf 'SCALE_TEST_CHURN_PODS must not exceed SCALE_TEST_PODS\n' >&2
  exit 2
fi
if [[ $WORKLOAD_PROFILE_ID != cni-scale-churn-2000-v1 ]]; then
  printf 'Unsupported CNI scale workload profile %q\n' "$WORKLOAD_PROFILE_ID" >&2
  exit 2
fi
for command in comm kubectl sort wc head sed; do
  command -v "$command" >/dev/null 2>&1 || {
    printf '%s is required\n' "$command" >&2
    exit 127
  }
done

script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
state_dir=${SCENARIO_STATE_DIR:-log}
CNI_WORKLOAD_STATE_FILE="${state_dir}/cni-scale-workload.state"
WORKLOAD_REPORT_PATH=${WORKLOAD_REPORT_PATH:-"${state_dir}/workload-report.json"}
source "${script_dir}/lib/workload-report.sh"

WORKLOAD_STARTED_AT=$(date -u +%Y-%m-%dT%H:%M:%SZ)
WORKLOAD_COMPLETED_AT=
WORKLOAD_REQUESTED_PODS=$SCALE_TEST_PODS
WORKLOAD_READY_PODS=0
WORKLOAD_CREATED_PODS=$SCALE_TEST_PODS
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
WORKLOAD_CLEANUP_STATUS=pending
WORKLOAD_EXPECTED_NODES=$CL2_EXPECTED_LINUX_NODES
WORKLOAD_COVERED_NODES=0
WORKLOAD_NODE_COVERAGE_STATUS=unknown

KUBECTL=(kubectl --kubeconfig "$KUBECONFIG")
namespace="${SCALE_TEST_NAMESPACE_PREFIX}-1"

publish_workload_result_on_exit() {
  local status=$?
  local publication_failed=0
  trap - EXIT

  if ((status != 0)); then
    WORKLOAD_STATUS=failed
  fi
  if [[ -n ${churn_started+x} ]]; then
    WORKLOAD_DURATION_SECONDS=$((SECONDS - churn_started))
  fi

  if ! cni_write_workload_state; then
    printf 'Failed to persist CNI scale workload state\n' >&2
    publication_failed=1
  fi
  if ! cni_write_workload_report; then
    printf 'Failed to publish CNI scale workload report to %s\n' \
      "$WORKLOAD_REPORT_PATH" >&2
    publication_failed=1
  fi

  if ((status != 0)); then
    printf 'CNI scale workload failed; collecting pod diagnostics\n' >&2
    "${KUBECTL[@]}" get pods \
      --all-namespaces \
      --selector group=cni-scale \
      --output wide || true
  fi
  if ((status == 0 && publication_failed != 0)); then
    status=1
  fi
  exit "$status"
}
trap publish_workload_result_on_exit EXIT
cni_write_workload_state

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

  local expected_nodes=()
  local workload_nodes=()
  mapfile -t expected_nodes < <("${KUBECTL[@]}" get nodes \
    --selector kubernetes.io/os=linux \
    --output jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}' |
    sed '/^$/d' |
    sort -u)
  mapfile -t workload_nodes < <("${KUBECTL[@]}" get pods \
    --namespace "$namespace" \
    --selector group=cni-scale \
    --output jsonpath='{range .items[*]}{.spec.nodeName}{"\n"}{end}' |
    sed '/^$/d' |
    sort -u)
  if ((${#expected_nodes[@]} != CL2_EXPECTED_LINUX_NODES)); then
    printf 'CNI scale cluster has %s Linux nodes, want %s\n' \
      "${#expected_nodes[@]}" "$CL2_EXPECTED_LINUX_NODES" >&2
    return 1
  fi
  local node_diff
  node_diff=$(comm -3 \
    <(printf '%s\n' "${expected_nodes[@]}") \
    <(printf '%s\n' "${workload_nodes[@]}"))
  if [[ -n $node_diff ]]; then
    printf 'CNI scale workload did not cover the exact Linux node set:\n%s\n' \
      "$node_diff" >&2
    return 1
  fi
  WORKLOAD_READY_PODS=$pod_count
  WORKLOAD_UNIQUE_IP_STATUS=pass
  WORKLOAD_COVERED_NODES=${#workload_nodes[@]}
  WORKLOAD_NODE_COVERAGE_STATUS=pass
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
      whenUnsatisfiable: DoNotSchedule
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

verify_ready_and_unique_ips
verify_cross_node_connectivity
printf 'CNI scale workload reached %s RunningAndReady pods\n' "$SCALE_TEST_PODS"

churn_started=$SECONDS
for ((round = 1; round <= SCALE_TEST_CHURN_ROUNDS; round++)); do
  round_started=$SECONDS
  printf 'CNI scale churn round %s/%s: replacing %s pods\n' \
    "$round" "$SCALE_TEST_CHURN_ROUNDS" "$SCALE_TEST_CHURN_PODS"
  replace_scale_pods "$round"
  verify_ready_and_unique_ips
  verify_cross_node_connectivity
  WORKLOAD_ROUNDS_COMPLETED=$round
  cni_write_workload_state

  round_elapsed=$((SECONDS - round_started))
  if ((round_elapsed > SCALE_TEST_CHURN_INTERVAL_SECONDS)); then
    printf 'CNI scale churn round %s took %ss, exceeding the %ss cadence budget\n' \
      "$round" "$round_elapsed" "$SCALE_TEST_CHURN_INTERVAL_SECONDS" >&2
    exit 1
  fi
  remaining=$((SCALE_TEST_CHURN_INTERVAL_SECONDS - round_elapsed))
  if ((round < SCALE_TEST_CHURN_ROUNDS && remaining > 0)); then
    sleep "$remaining"
  fi
done

WORKLOAD_DURATION_SECONDS=$((SECONDS - churn_started))
WORKLOAD_STATUS=pass
printf 'CNI scale workload completed %s churn rounds successfully\n' \
  "$SCALE_TEST_CHURN_ROUNDS"
