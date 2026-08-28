#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
TMP_DIR=$(mktemp -d)
trap 'rm -rf "$TMP_DIR"' EXIT

assert_equal() {
    local expected=$1
    local actual=$2
    local message=$3
    if [[ "$expected" != "$actual" ]]; then
        echo "FAIL: $message: expected $expected, got $actual" >&2
        exit 1
    fi
}

run_lifecycle_case() {
    local exit_status=$1
    local delete_status=$2
    local calls_file=$3

    set +e
    (
        source "$REPO_ROOT/scripts/lib/cleanup.sh"
        RUN_KOPS_TEST=true
        RUN_BOTTLEROCKET_TEST=false
        RUN_PERFORMANCE_TESTS=false
        RUNNING_PERFORMANCE=false
        DEPROVISION=true
        __cluster_created=1
        __cluster_deprovisioned=0
        CLUSTER_NAME='test'

        FAKE_CALLS_FILE=$calls_file
        FAKE_DELETE_STATUS=$delete_status
        collect_kops_diagnostics() { echo diagnostics >> "$FAKE_CALLS_FILE"; }
        down-kops-cluster() {
            echo delete >> "$FAKE_CALLS_FILE"
            return "$FAKE_DELETE_STATUS"
        }

        trap cleanup_on_exit EXIT
        exit "$exit_status"
    ) > "${calls_file}.output" 2>&1
    local result=$?
    set -e
    echo "$result"
}

calls="$TMP_DIR/lifecycle-calls"
result=$(run_lifecycle_case 7 0 "$calls")
assert_equal 7 "$result" "test failure must be preserved"
assert_equal 1 "$(grep -c '^delete$' "$calls")" "failed test must clean up once"
assert_equal 1 "$(grep -c '^diagnostics$' "$calls")" "failed test must collect diagnostics"

: > "$calls"
result=$(run_lifecycle_case 0 23 "$calls")
assert_equal 23 "$result" "cleanup failure must fail a successful test"
assert_equal 1 "$(grep -c '^delete$' "$calls")" "successful test must clean up once"

: > "$calls"
result=$(run_lifecycle_case 7 23 "$calls")
assert_equal 7 "$result" "cleanup failure must not hide test failure"

fake_kops="$TMP_DIR/kops"
cat > "$fake_kops" <<'KOPS'
#!/usr/bin/env bash
echo "KOPS_STATE_STORE=${KOPS_STATE_STORE:-} $*" >> "$FAKE_KOPS_CALLS"
exit "${FAKE_KOPS_STATUS:-0}"
KOPS
chmod +x "$fake_kops"

state_file="$TMP_DIR/kops-state"
printf '%s\n%s\n' 'kops-cni-test-cluster-123-1.k8s.local' 's3://kops-cni-test-eks-443709043722' > "$state_file"
export KOPS_BIN="$fake_kops"
export FAKE_KOPS_CALLS="$TMP_DIR/kops-calls"
FAKE_KOPS_STATUS=0 "$REPO_ROOT/scripts/cleanup-kops-cluster.sh" "$state_file"
[[ ! -e "$state_file" ]] || { echo "FAIL: successful cleanup must remove state file" >&2; exit 1; }
grep -q '^KOPS_STATE_STORE=s3://kops-cni-test-eks-443709043722 delete cluster --name kops-cni-test-cluster-123-1.k8s.local --yes$' "$FAKE_KOPS_CALLS"

printf '%s\n%s\n' 'kops-cni-test-cluster-123-1.k8s.local' 's3://kops-cni-test-eks-443709043722' > "$state_file"
set +e
FAKE_KOPS_STATUS=42 "$REPO_ROOT/scripts/cleanup-kops-cluster.sh" "$state_file"
result=$?
set -e
assert_equal 42 "$result" "failed cleanup must propagate kOps status"
[[ -e "$state_file" ]] || { echo "FAIL: failed cleanup must preserve state file" >&2; exit 1; }

rm -f "$state_file"
"$REPO_ROOT/scripts/cleanup-kops-cluster.sh" "$state_file"

printf '%s\n%s\n' 'unexpected-cluster' 's3://kops-cni-test-eks-443709043722' > "$state_file"
set +e
"$REPO_ROOT/scripts/cleanup-kops-cluster.sh" "$state_file"
result=$?
set -e
assert_equal 2 "$result" "invalid cleanup state must be rejected"

printf '%s\n%s\n\n%s\n' 'kops-cni-test-cluster-123-1.k8s.local' 's3://kops-cni-test-eks-443709043722' 'unexpected' > "$state_file"
set +e
"$REPO_ROOT/scripts/cleanup-kops-cluster.sh" "$state_file"
result=$?
set -e
assert_equal 2 "$result" "cleanup state with extra lines must be rejected"

printf '%s\n%s\n%s' 'kops-cni-test-cluster-123-1.k8s.local' 's3://kops-cni-test-eks-443709043722' 'unterminated-extra' > "$state_file"
set +e
"$REPO_ROOT/scripts/cleanup-kops-cluster.sh" "$state_file"
result=$?
set -e
assert_equal 2 "$result" "unterminated extra cleanup state must be rejected"

if grep -qE 'aws s3 (rm|rb).*KOPS_STATE_STORE' "$REPO_ROOT/scripts/lib/cluster.sh"; then
    echo "FAIL: cleanup must not remove the shared state bucket" >&2
    exit 1
fi

grep -q 'return 1' "$REPO_ROOT/scripts/lib/integration.sh"

source "$REPO_ROOT/scripts/lib/aws.sh"
aws() {
    echo "$*" > "$TMP_DIR/aws-call"
    echo "${FAKE_IMAGE_COUNT:-1}"
}
ensure_ecr_image_exists '602401143452.dkr.ecr.us-west-2.amazonaws.com/amazon/aws-network-policy-agent:v1.4.1'
grep -q -- '--registry-id 602401143452 --repository-name amazon/aws-network-policy-agent --image-ids imageTag=v1.4.1 --region us-west-2' "$TMP_DIR/aws-call"
set +e
FAKE_IMAGE_COUNT=0 ensure_ecr_image_exists '602401143452.dkr.ecr.us-west-2.amazonaws.com/amazon/aws-network-policy-agent:missing'
result=$?
set -e
assert_equal 1 "$result" "missing external image must fail preflight"
set +e
ensure_ecr_image_exists 'not-an-image'
result=$?
set -e
assert_equal 1 "$result" "malformed external image must fail preflight"

echo "PASS: kOps cleanup tests"
