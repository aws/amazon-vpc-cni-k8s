#!/usr/bin/env bash

# Exercises CNI ADD/DEL and secondary-ENI allocation on an existing cluster.
# Cluster creation, CNI installation, metric capture, and cluster deletion are
# owned by the caller.

set -euo pipefail

NAMESPACE=${SCALE_TEST_NAMESPACE:-cni-scale-test}
TEST_IMAGE_REGISTRY=${TEST_IMAGE_REGISTRY:-617930562442.dkr.ecr.us-west-2.amazonaws.com}
NGINX_IMAGE=${NGINX_IMAGE:-${TEST_IMAGE_REGISTRY}/networking-e2e-test-images/nginx:1.25.2}
BUSYBOX_IMAGE=${BUSYBOX_IMAGE:-${TEST_IMAGE_REGISTRY}/networking-e2e-test-images/busybox:latest}
SCALE_TEST_PODS=${SCALE_TEST_PODS:-20}
SCALE_TEST_STEP_PODS=${SCALE_TEST_STEP_PODS:-5}
SCALE_TEST_CYCLES=${SCALE_TEST_CYCLES:-3}
SCALE_TEST_TIMEOUT_SECONDS=${SCALE_TEST_TIMEOUT_SECONDS:-600}
SCALE_TEST_PROBE_TIMEOUT_SECONDS=${SCALE_TEST_PROBE_TIMEOUT_SECONDS:-5}
NG_LABEL_KEY=${NG_LABEL_KEY:-kubernetes.io/os}
NG_LABEL_VAL=${NG_LABEL_VAL:-linux}

if [[ -n ${KUBE_CONFIG_PATH:-} && -z ${KUBECONFIG:-} ]]; then
  export KUBECONFIG=$KUBE_CONFIG_PATH
fi
KUBECONFIG=${KUBECONFIG:-"${HOME:?HOME must be set}/.kube/config"}
export KUBECONFIG

KUBECTL=(kubectl --kubeconfig "$KUBECONFIG")

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
    printf 'CNI scale test failed; collecting diagnostics\n' >&2
    print_diagnostics
  fi
  if ! "${KUBECTL[@]}" delete namespace "$NAMESPACE" \
    --ignore-not-found=true \
    --wait=true \
    --timeout "${SCALE_TEST_TIMEOUT_SECONDS}s"; then
    printf 'Failed to delete namespace %s\n' "$NAMESPACE" >&2
    status=1
  fi
  exit "$status"
}

wait_for_zero_target_pods() {
  local deadline=$((SECONDS + SCALE_TEST_TIMEOUT_SECONDS))
  local pods
  while ((SECONDS < deadline)); do
    pods=$("${KUBECTL[@]}" get pods \
      --namespace "$NAMESPACE" \
      --selector app=cni-scale-target \
      --output name)
    if [[ -z $pods ]]; then
      return
    fi
    sleep 2
  done
  printf 'Target pods did not scale to zero within %ss\n' "$SCALE_TEST_TIMEOUT_SECONDS" >&2
  return 1
}

verify_target_ready() {
  local expected=$1
  local ready
  "${KUBECTL[@]}" rollout status deployment/cni-scale-target \
    --namespace "$NAMESPACE" \
    --timeout "${SCALE_TEST_TIMEOUT_SECONDS}s"
  ready=$("${KUBECTL[@]}" get deployment cni-scale-target \
    --namespace "$NAMESPACE" \
    --output jsonpath='{.status.readyReplicas}')
  if [[ ${ready:-0} != "$expected" ]]; then
    printf 'Expected %s ready target pods, found %s\n' \
      "$expected" "${ready:-0}" >&2
    return 1
  fi
}

scale_up_target() {
  local replicas=0
  local next
  while ((replicas < SCALE_TEST_PODS)); do
    next=$((replicas + SCALE_TEST_STEP_PODS))
    if ((next > SCALE_TEST_PODS)); then
      next=$SCALE_TEST_PODS
    fi
    printf 'Scaling target workload to %s replicas\n' "$next"
    "${KUBECTL[@]}" scale deployment/cni-scale-target \
      --namespace "$NAMESPACE" \
      --replicas "$next"
    verify_target_ready "$next"
    replicas=$next
  done
}

probe_target() {
  "${KUBECTL[@]}" exec \
    --namespace "$NAMESPACE" \
    deployment/cni-scale-probe \
    -- wget -q -T "$SCALE_TEST_PROBE_TIMEOUT_SECONDS" -O /dev/null http://cni-scale-target/
}

for name in SCALE_TEST_PODS SCALE_TEST_STEP_PODS SCALE_TEST_CYCLES SCALE_TEST_TIMEOUT_SECONDS SCALE_TEST_PROBE_TIMEOUT_SECONDS; do
  validate_positive_integer "$name" "${!name}"
done
command -v kubectl >/dev/null 2>&1 || {
  printf 'kubectl is required\n' >&2
  exit 127
}

TARGET_NODE=${SCALE_TEST_NODE:-$("${KUBECTL[@]}" get nodes \
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
  --timeout "${SCALE_TEST_TIMEOUT_SECONDS}s"
"${KUBECTL[@]}" create namespace "$NAMESPACE"
trap cleanup EXIT

"${KUBECTL[@]}" apply --filename - <<EOF
apiVersion: apps/v1
kind: Deployment
metadata:
  name: cni-scale-target
  namespace: ${NAMESPACE}
spec:
  replicas: 0
  selector:
    matchLabels:
      app: cni-scale-target
  template:
    metadata:
      labels:
        app: cni-scale-target
    spec:
      nodeName: ${TARGET_NODE}
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
  name: cni-scale-probe
  namespace: ${NAMESPACE}
spec:
  replicas: 1
  selector:
    matchLabels:
      app: cni-scale-probe
  template:
    metadata:
      labels:
        app: cni-scale-probe
    spec:
      containers:
        - name: busybox
          image: ${BUSYBOX_IMAGE}
          imagePullPolicy: IfNotPresent
          command: ["sleep", "604800"]
---
apiVersion: v1
kind: Service
metadata:
  name: cni-scale-target
  namespace: ${NAMESPACE}
spec:
  selector:
    app: cni-scale-target
  ports:
    - port: 80
      targetPort: 80
EOF

"${KUBECTL[@]}" rollout status deployment/cni-scale-probe \
  --namespace "$NAMESPACE" \
  --timeout "${SCALE_TEST_TIMEOUT_SECONDS}s"

printf 'Running %s CNI scale cycles with %s pods pinned to %s\n' \
  "$SCALE_TEST_CYCLES" "$SCALE_TEST_PODS" "$TARGET_NODE"
for ((cycle = 1; cycle <= SCALE_TEST_CYCLES; cycle++)); do
  printf 'Scale cycle %s/%s: 0 -> %s -> 0\n' \
    "$cycle" "$SCALE_TEST_CYCLES" "$SCALE_TEST_PODS"
  scale_up_target
  probe_target
  "${KUBECTL[@]}" scale deployment/cni-scale-target \
    --namespace "$NAMESPACE" \
    --replicas 0
  wait_for_zero_target_pods
done

printf 'CNI scale test completed successfully\n'
