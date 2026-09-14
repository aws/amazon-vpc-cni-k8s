#!/usr/bin/env bash

# Runs sustained CNI ADD/DEL and IP-allocation churn on an existing cluster.
# Cluster creation, CNI installation, metric capture, and cluster deletion are
# owned by the caller.

set -euo pipefail

NAMESPACE=${SOAK_TEST_NAMESPACE:-cni-soak-test}
TEST_IMAGE_REGISTRY=${TEST_IMAGE_REGISTRY:-617930562442.dkr.ecr.us-west-2.amazonaws.com}
NGINX_IMAGE=${NGINX_IMAGE:-${TEST_IMAGE_REGISTRY}/networking-e2e-test-images/nginx:1.25.2}
BUSYBOX_IMAGE=${BUSYBOX_IMAGE:-${TEST_IMAGE_REGISTRY}/networking-e2e-test-images/busybox:latest}
SOAK_DURATION_MINUTES=${SOAK_DURATION_MINUTES:-120}
SOAK_BASE_PODS=${SOAK_BASE_PODS:-6}
SOAK_CHURN_PODS=${SOAK_CHURN_PODS:-1}
SOAK_IP_PRESSURE_PODS=${SOAK_IP_PRESSURE_PODS:-20}
SOAK_IP_PRESSURE_STEP_PODS=${SOAK_IP_PRESSURE_STEP_PODS:-5}
SOAK_CYCLE_INTERVAL_SECONDS=${SOAK_CYCLE_INTERVAL_SECONDS:-300}
SOAK_HEALTH_INTERVAL_SECONDS=${SOAK_HEALTH_INTERVAL_SECONDS:-60}
SOAK_TIMEOUT_SECONDS=${SOAK_TIMEOUT_SECONDS:-600}
SOAK_PROBE_TIMEOUT_SECONDS=${SOAK_PROBE_TIMEOUT_SECONDS:-5}
NG_LABEL_KEY=${NG_LABEL_KEY:-kubernetes.io/os}
NG_LABEL_VAL=${NG_LABEL_VAL:-linux}
WORKLOAD_PROFILE_ID=${WORKLOAD_PROFILE_ID:-cni-functional-soak-v1}

script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
source "${script_dir}/lib/workload-report.sh"
WORKLOAD_REPORT_PATH=${WORKLOAD_REPORT_PATH:-"log/cni-soak-workload-report.json"}
WORKLOAD_STARTED_AT=$(date -u +%Y-%m-%dT%H:%M:%SZ)
WORKLOAD_REQUESTED_PODS=$((SOAK_BASE_PODS + SOAK_IP_PRESSURE_PODS + 1))
WORKLOAD_READY_PODS=0
WORKLOAD_CREATED_PODS=0
WORKLOAD_DELETED_PODS=0
WORKLOAD_POD_THROUGHPUT=0
WORKLOAD_CHURN_PODS_PER_ROUND=$SOAK_CHURN_PODS
WORKLOAD_CHURN_INTERVAL_SECONDS=$SOAK_CYCLE_INTERVAL_SECONDS
WORKLOAD_DURATION_SECONDS=0
WORKLOAD_ROUNDS_REQUESTED=0
WORKLOAD_ROUNDS_COMPLETED=0
WORKLOAD_CONNECTIVITY_STATUS=unknown
WORKLOAD_UNIQUE_IP_STATUS=unknown
WORKLOAD_STATUS=running
WORKLOAD_CLEANUP_STATUS=failed

if [[ -n ${KUBE_CONFIG_PATH:-} && -z ${KUBECONFIG:-} ]]; then
  export KUBECONFIG=$KUBE_CONFIG_PATH
fi
KUBECONFIG=${KUBECONFIG:-"${HOME:?HOME must be set}/.kube/config"}
export KUBECONFIG

KUBECTL=(kubectl --kubeconfig "$KUBECONFIG")
IP_PRESSURE_REPLICAS=0

validate_positive_integer() {
  local name=$1
  local value=$2
  if [[ ! $value =~ ^[1-9][0-9]*$ ]]; then
    printf '%s must be a positive integer; got %q\n' "$name" "$value" >&2
    exit 2
  fi
}

print_diagnostics() {
  "${KUBECTL[@]}" get all --namespace "$NAMESPACE" --output wide || true
  "${KUBECTL[@]}" get events --namespace "$NAMESPACE" --sort-by=.lastTimestamp || true
}

cleanup() {
  local status=$?
  trap - EXIT
  if ((status != 0)); then
    WORKLOAD_STATUS=failed
    printf 'CNI soak test failed; collecting diagnostics\n' >&2
    print_diagnostics
  else
    WORKLOAD_STATUS=pass
  fi
  local remaining_pods
  remaining_pods=$("${KUBECTL[@]}" get pods \
    --namespace "$NAMESPACE" \
    --output name 2>/dev/null |
    wc -l || true)
  if ! "${KUBECTL[@]}" delete namespace "$NAMESPACE" \
    --ignore-not-found=true \
    --wait=true \
    --timeout "${SOAK_TIMEOUT_SECONDS}s"; then
    printf 'Failed to delete namespace %s\n' "$NAMESPACE" >&2
    status=1
  else
    WORKLOAD_DELETED_PODS=$((WORKLOAD_DELETED_PODS + remaining_pods))
    WORKLOAD_CLEANUP_STATUS=pass
  fi
  WORKLOAD_COMPLETED_AT=$(date -u +%Y-%m-%dT%H:%M:%SZ)
  if ! cni_write_workload_report; then
    printf 'Failed to write CNI soak workload report\n' >&2
    status=1
  fi
  exit "$status"
}

verify_deployment_ready() {
  local deployment=$1
  local expected=$2
  local deadline=$((SECONDS + SOAK_TIMEOUT_SECONDS))
  local ready
  while ((SECONDS < deadline)); do
    ready=$("${KUBECTL[@]}" get deployment "$deployment" \
      --namespace "$NAMESPACE" \
      --output jsonpath='{.status.readyReplicas}')
    if [[ ${ready:-0} == "$expected" ]]; then
      return
    fi
    sleep 2
  done
  printf 'Deployment %s has %s ready replicas, want %s\n' \
    "$deployment" "${ready:-0}" "$expected" >&2
  return 1
}

verify_ip_pressure() {
  verify_deployment_ready cni-soak-ip-pressure "$IP_PRESSURE_REPLICAS"
}

wait_for_ip_pressure_pod_count() {
  local expected=$1
  local deadline=$((SECONDS + SOAK_TIMEOUT_SECONDS))
  local pods
  local count
  while ((SECONDS < deadline)); do
    pods=$("${KUBECTL[@]}" get pods \
      --namespace "$NAMESPACE" \
      --selector app=cni-soak-ip-pressure \
      --output name)
    if [[ -z $pods ]]; then
      count=0
    else
      count=$(printf '%s\n' "$pods" | wc -l)
    fi
    if ((count == expected)); then
      return
    fi
    sleep 2
  done
  printf 'IP-pressure workload has %s pods, want %s\n' "$count" "$expected" >&2
  return 1
}

probe_target() {
  "${KUBECTL[@]}" exec \
    --namespace "$NAMESPACE" \
    deployment/cni-soak-probe \
    -- wget -q -T "$SOAK_PROBE_TIMEOUT_SECONDS" -O /dev/null http://cni-soak-target/
  WORKLOAD_CONNECTIVITY_STATUS=pass
}

verify_unique_pod_ips() {
  local pod_count
  local unique_ip_count
  pod_count=$("${KUBECTL[@]}" get pods \
    --namespace "$NAMESPACE" \
    --output jsonpath='{range .items[*]}{.status.podIP}{"\n"}{end}' |
    sed '/^$/d' |
    wc -l)
  unique_ip_count=$("${KUBECTL[@]}" get pods \
    --namespace "$NAMESPACE" \
    --output jsonpath='{range .items[*]}{.status.podIP}{"\n"}{end}' |
    sed '/^$/d' |
    sort -u |
    wc -l)
  if ((pod_count == 0 || unique_ip_count != pod_count)); then
    printf 'CNI soak pod/IP count mismatch: pods=%s uniqueIPs=%s\n' \
      "$pod_count" "$unique_ip_count" >&2
    return 1
  fi
  if ((pod_count > WORKLOAD_READY_PODS)); then
    WORKLOAD_READY_PODS=$pod_count
  fi
  WORKLOAD_UNIQUE_IP_STATUS=pass
}

health_check() {
  verify_deployment_ready cni-soak-target "$SOAK_BASE_PODS"
  verify_deployment_ready cni-soak-probe 1
  verify_ip_pressure
  probe_target
  verify_unique_pod_ips
}

restart_base_pods() {
  local pods=()
  local pod
  while IFS= read -r pod; do
    [[ -n $pod ]] && pods+=("$pod")
  done < <("${KUBECTL[@]}" get pods \
    --namespace "$NAMESPACE" \
    --selector app=cni-soak-target \
    --output jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}' |
    head -n "$SOAK_CHURN_PODS")
  if ((${#pods[@]} != SOAK_CHURN_PODS)); then
    printf 'Found %s base pods to restart, want %s\n' \
      "${#pods[@]}" "$SOAK_CHURN_PODS" >&2
    return 1
  fi
  "${KUBECTL[@]}" delete pod "${pods[@]}" \
    --namespace "$NAMESPACE" \
    --wait=false
  WORKLOAD_DELETED_PODS=$((WORKLOAD_DELETED_PODS + ${#pods[@]}))
  for pod in "${pods[@]}"; do
    "${KUBECTL[@]}" wait "pod/$pod" \
      --namespace "$NAMESPACE" \
      --for delete \
      --timeout "${SOAK_TIMEOUT_SECONDS}s"
  done
  verify_deployment_ready cni-soak-target "$SOAK_BASE_PODS"
  WORKLOAD_CREATED_PODS=$((WORKLOAD_CREATED_PODS + ${#pods[@]}))
}

toggle_ip_pressure() {
  local target=0
  local next
  local previous
  if ((IP_PRESSURE_REPLICAS == 0)); then
    target=$SOAK_IP_PRESSURE_PODS
  fi

  while ((IP_PRESSURE_REPLICAS != target)); do
    previous=$IP_PRESSURE_REPLICAS
    if ((IP_PRESSURE_REPLICAS < target)); then
      next=$((IP_PRESSURE_REPLICAS + SOAK_IP_PRESSURE_STEP_PODS))
      if ((next > target)); then
        next=$target
      fi
    else
      next=$((IP_PRESSURE_REPLICAS - SOAK_IP_PRESSURE_STEP_PODS))
      if ((next < target)); then
        next=$target
      fi
    fi

    printf 'Scaling IP-pressure workload to %s replicas\n' "$next"
    "${KUBECTL[@]}" scale deployment/cni-soak-ip-pressure \
      --namespace "$NAMESPACE" \
      --replicas "$next"
    IP_PRESSURE_REPLICAS=$next
    verify_ip_pressure
    if ((next > previous)); then
      WORKLOAD_CREATED_PODS=$((WORKLOAD_CREATED_PODS + next - previous))
    else
      WORKLOAD_DELETED_PODS=$((WORKLOAD_DELETED_PODS + previous - next))
    fi
    if ((next < previous)); then
      wait_for_ip_pressure_pod_count "$next"
    fi
  done
}

for name in \
  SOAK_DURATION_MINUTES \
  SOAK_BASE_PODS \
  SOAK_CHURN_PODS \
  SOAK_IP_PRESSURE_PODS \
  SOAK_IP_PRESSURE_STEP_PODS \
  SOAK_CYCLE_INTERVAL_SECONDS \
  SOAK_HEALTH_INTERVAL_SECONDS \
  SOAK_TIMEOUT_SECONDS \
  SOAK_PROBE_TIMEOUT_SECONDS; do
  validate_positive_integer "$name" "${!name}"
done
if [[ $WORKLOAD_PROFILE_ID != cni-functional-soak-v1 ]]; then
  printf 'Unsupported CNI soak workload profile %q\n' "$WORKLOAD_PROFILE_ID" >&2
  exit 2
fi
if ((SOAK_CHURN_PODS > SOAK_BASE_PODS)); then
  printf 'SOAK_CHURN_PODS must not exceed SOAK_BASE_PODS\n' >&2
  exit 2
fi
command -v kubectl >/dev/null 2>&1 || {
  printf 'kubectl is required\n' >&2
  exit 127
}

TARGET_NODE=${SOAK_TEST_NODE:-$("${KUBECTL[@]}" get nodes \
  --field-selector spec.unschedulable!=true \
  --selector "${NG_LABEL_KEY}=${NG_LABEL_VAL}" \
  --output jsonpath='{.items[0].metadata.name}')}
if [[ -z $TARGET_NODE ]]; then
  printf 'No schedulable node matched %s=%s\n' "$NG_LABEL_KEY" "$NG_LABEL_VAL" >&2
  exit 1
fi

"${KUBECTL[@]}" delete namespace "$NAMESPACE" \
  --ignore-not-found=true \
  --wait=true \
  --timeout "${SOAK_TIMEOUT_SECONDS}s"
"${KUBECTL[@]}" create namespace "$NAMESPACE"
trap cleanup EXIT

"${KUBECTL[@]}" apply --filename - <<EOF
apiVersion: apps/v1
kind: Deployment
metadata:
  name: cni-soak-target
  namespace: ${NAMESPACE}
spec:
  replicas: ${SOAK_BASE_PODS}
  selector:
    matchLabels:
      app: cni-soak-target
  template:
    metadata:
      labels:
        app: cni-soak-target
    spec:
      containers:
        - name: nginx
          image: ${NGINX_IMAGE}
          imagePullPolicy: IfNotPresent
          readinessProbe:
            httpGet:
              path: /
              port: 80
            periodSeconds: 2
            timeoutSeconds: 2
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: cni-soak-probe
  namespace: ${NAMESPACE}
spec:
  replicas: 1
  selector:
    matchLabels:
      app: cni-soak-probe
  template:
    metadata:
      labels:
        app: cni-soak-probe
    spec:
      containers:
        - name: busybox
          image: ${BUSYBOX_IMAGE}
          imagePullPolicy: IfNotPresent
          command: ["sleep", "604800"]
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: cni-soak-ip-pressure
  namespace: ${NAMESPACE}
spec:
  replicas: 0
  selector:
    matchLabels:
      app: cni-soak-ip-pressure
  template:
    metadata:
      labels:
        app: cni-soak-ip-pressure
    spec:
      nodeName: ${TARGET_NODE}
      containers:
        - name: busybox
          image: ${BUSYBOX_IMAGE}
          imagePullPolicy: IfNotPresent
          command: ["sleep", "604800"]
---
apiVersion: v1
kind: Service
metadata:
  name: cni-soak-target
  namespace: ${NAMESPACE}
spec:
  selector:
    app: cni-soak-target
  ports:
    - port: 80
      targetPort: 80
EOF

health_check
WORKLOAD_CREATED_PODS=$((SOAK_BASE_PODS + 1))

DURATION_SECONDS=$((SOAK_DURATION_MINUTES * 60))
START_SECONDS=$SECONDS
DEADLINE=$((START_SECONDS + DURATION_SECONDS))
WORKLOAD_ROUNDS_REQUESTED=$(((DURATION_SECONDS + SOAK_CYCLE_INTERVAL_SECONDS - 1) / SOAK_CYCLE_INTERVAL_SECONDS))
NEXT_CYCLE=$START_SECONDS
NEXT_HEALTH=$START_SECONDS
printf 'Running CNI soak workload for %s minutes; pressure node=%s\n' \
  "$SOAK_DURATION_MINUTES" "$TARGET_NODE"

while ((SECONDS < DEADLINE)); do
  if ((SECONDS >= NEXT_CYCLE)); then
    restart_base_pods
    toggle_ip_pressure
    WORKLOAD_ROUNDS_COMPLETED=$((WORKLOAD_ROUNDS_COMPLETED + 1))
    NEXT_CYCLE=$((START_SECONDS + WORKLOAD_ROUNDS_COMPLETED * SOAK_CYCLE_INTERVAL_SECONDS))
  fi
  if ((SECONDS >= NEXT_HEALTH)); then
    health_check
    completed_health_intervals=$(((SECONDS - START_SECONDS) / SOAK_HEALTH_INTERVAL_SECONDS + 1))
    NEXT_HEALTH=$((START_SECONDS + completed_health_intervals * SOAK_HEALTH_INTERVAL_SECONDS))
  fi

  NEXT_EVENT=$NEXT_CYCLE
  if ((NEXT_HEALTH < NEXT_EVENT)); then
    NEXT_EVENT=$NEXT_HEALTH
  fi
  if ((DEADLINE < NEXT_EVENT)); then
    NEXT_EVENT=$DEADLINE
  fi
  SLEEP_SECONDS=$((NEXT_EVENT - SECONDS))
  if ((SLEEP_SECONDS > 0)); then
    sleep "$SLEEP_SECONDS"
  fi
done

if ((IP_PRESSURE_REPLICAS > 0)); then
  toggle_ip_pressure
fi
health_check
WORKLOAD_DURATION_SECONDS=$((SECONDS - START_SECONDS))
printf 'CNI soak test completed successfully\n'
