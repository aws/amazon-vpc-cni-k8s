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

: > "$calls"
result=$(run_lifecycle_case 0 23 "$calls")
assert_equal 23 "$result" "cleanup failure must fail a successful test"
assert_equal 1 "$(grep -c '^delete$' "$calls")" "successful test must clean up once"

: > "$calls"
result=$(run_lifecycle_case 7 23 "$calls")
assert_equal 7 "$result" "cleanup failure must not hide test failure"

if grep -qE 'aws s3 (rm|rb).*KOPS_STATE_STORE' "$REPO_ROOT/scripts/lib/cluster.sh"; then
    echo "FAIL: cleanup must not remove the shared state bucket" >&2
    exit 1
fi

grep -q 'return 1' "$REPO_ROOT/scripts/lib/integration.sh"

echo "PASS: kOps cleanup tests"
