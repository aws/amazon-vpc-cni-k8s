#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
REPO_SCRIPT_DIR=$(cd -- "${SCRIPT_DIR}/.." && pwd)
TEST_DIR=$(mktemp -d)
trap 'rm -rf -- "$TEST_DIR"' EXIT

mkdir -p "${TEST_DIR}/bin" "${TEST_DIR}/state"
cat >"${TEST_DIR}/bin/kubectl" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

case "$*" in
  *"get pods"*)
    for ((pod = 1; pod <= 2000; pod++)); do
      printf 'pod/cni-scale-%s\n' "$pod"
    done
    ;;
  *"delete namespace"*)
    ;;
  *"get namespace"*)
    exit 1
    ;;
  *)
    printf 'unexpected kubectl invocation: %s\n' "$*" >&2
    exit 1
    ;;
esac
EOF
chmod +x "${TEST_DIR}/bin/kubectl"

cat >"${TEST_DIR}/state/cni-scale-workload.state" <<EOF
WORKLOAD_PROFILE_ID=cni-scale-churn-2000-v1
WORKLOAD_GIT_REVISION=test
TEST_SCENARIO_ID=cni-scale-default-v1
WORKLOAD_REPORT_PATH=${TEST_DIR}/workload-report.json
WORKLOAD_STARTED_AT=2026-09-23T00:00:00Z
WORKLOAD_REQUESTED_PODS=2000
WORKLOAD_READY_PODS=2000
WORKLOAD_CREATED_PODS=4400
WORKLOAD_DELETED_PODS=2400
WORKLOAD_POD_THROUGHPUT=20
WORKLOAD_CHURN_PODS_PER_ROUND=200
WORKLOAD_CHURN_INTERVAL_SECONDS=300
WORKLOAD_DURATION_SECONDS=3300
WORKLOAD_ROUNDS_REQUESTED=12
WORKLOAD_ROUNDS_COMPLETED=12
WORKLOAD_CONNECTIVITY_STATUS=pass
WORKLOAD_UNIQUE_IP_STATUS=pass
WORKLOAD_STATUS=pass
WORKLOAD_CLEANUP_STATUS=pending
EOF

PATH="${TEST_DIR}/bin:${PATH}" \
  KUBECONFIG="${TEST_DIR}/kubeconfig" \
  SCENARIO_STATE_DIR="${TEST_DIR}/state" \
  bash "${REPO_SCRIPT_DIR}/run-cni-scale-cleanup.sh"

grep -q '"durationSeconds": 3300' "${TEST_DIR}/workload-report.json"
grep -q '"cleanupStatus": "pass"' "${TEST_DIR}/workload-report.json"
echo PASS
