#!/usr/bin/env bash

# Test cases intentionally isolate shared lifecycle globals in subshells.
# shellcheck disable=SC2030,SC2031

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

assert_call_count() {
    local expected=$1
    local call=$2
    local calls_file=$3
    local message=$4
    local actual

    actual=$(grep -Fxc -- "$call" "$calls_file" || true)
    assert_equal "$expected" "$actual" "$message"
}

assert_prefix_count() {
    local expected=$1
    local prefix=$2
    local calls_file=$3
    local message=$4
    local actual

    actual=$(grep -c "^$prefix" "$calls_file" || true)
    assert_equal "$expected" "$actual" "$message"
}

run_cleanup_case() {
    local cluster_type=$1
    local trigger=$2
    local trigger_status=$3
    local delete_status=$4
    local cluster_created=$5
    local cluster_deprovisioned=$6
    local deprovision=$7
    local signal_during_delete=$8
    local calls_file=$9
    local cleanup_attempted=${10:-0}

    : > "$calls_file"
    set +e
    (
        set -Euo pipefail
        source "$REPO_ROOT/scripts/lib/cleanup.sh"

        RUN_KOPS_TEST=false
        RUN_BOTTLEROCKET_TEST=false
        RUN_PERFORMANCE_TESTS=false
        DEPROVISION=$deprovision
        __cluster_created=$cluster_created
        __cluster_deprovisioned=$cluster_deprovisioned
        __cluster_cleanup_attempted=$cleanup_attempted
        CLUSTER_NAME=performance-test-cluster

        case "$cluster_type" in
            performance)
                RUN_PERFORMANCE_TESTS=true
                # The historical in-progress marker must not suppress EXIT cleanup.
                export RUNNING_PERFORMANCE=true
                ;;
            kops)
                RUN_KOPS_TEST=true
                CLUSTER_NAME=kops-cluster
                ;;
            *)
                echo "Unknown cluster type: $cluster_type" >&2
                exit 2
                ;;
        esac

        FAKE_DELETE_STATUS=$delete_status
        FAKE_CALLS_FILE=$calls_file
        FAKE_SIGNAL_DURING_DELETE=$signal_during_delete
        # deprovision_cluster invokes these mocks indirectly.
        # shellcheck disable=SC2317
        eksctl() {
            echo "performance-delete" >> "$FAKE_CALLS_FILE"
            if [[ "$FAKE_SIGNAL_DURING_DELETE" == true ]]; then
                FAKE_SIGNAL_DURING_DELETE=false
                kill -TERM "$BASHPID"
            fi
            return "$FAKE_DELETE_STATUS"
        }
        # shellcheck disable=SC2317
        down-kops-cluster() {
            echo "kops-delete" >> "$FAKE_CALLS_FILE"
            return "$FAKE_DELETE_STATUS"
        }
        # install_cleanup_traps invokes this handler indirectly.
        # shellcheck disable=SC2317
        on_error() {
            exit "$1"
        }

        if [[ "$trigger" == exit ]]; then
            trap cleanup_on_exit EXIT
            exit "$trigger_status"
        fi

        install_cleanup_traps
        case "$trigger" in
            error)
                bash -c 'exit "$1"' _ "$trigger_status"
                ;;
            INT | TERM)
                kill "-$trigger" "$BASHPID"
                ;;
            deprovision)
                deprovision_cluster
                ;;
            *)
                echo "Unknown trigger: $trigger" >&2
                exit 2
                ;;
        esac
    ) > "${calls_file}.output" 2>&1
    local result=$?
    set -e

    echo "$result"
}

run_generic_provision_case() {
    local failure_stage=$1
    local calls_file=$2

    : > "$calls_file"
    set +e
    TEST_REPO_ROOT=$REPO_ROOT \
        TEST_TMP_DIR=$TMP_DIR \
        FAKE_CALLS_FILE=$calls_file \
        FAKE_FAILURE_STAGE=$failure_stage \
        bash -c '
        set -Euo pipefail

        SCRIPT_DIR="$TEST_REPO_ROOT/scripts"
        source "$SCRIPT_DIR/lib/cluster.sh"
        source "$SCRIPT_DIR/lib/cleanup.sh"

        RUN_KOPS_TEST=false
        RUN_BOTTLEROCKET_TEST=false
        RUN_PERFORMANCE_TESTS=false
        RUN_CONFORMANCE=false
        RUN_CNI_INTEGRATION_TESTS=false
        EKS_CLUSTER_VERSION=1.36
        ROLE_ARN=""
        DEPROVISION=true
        __cluster_created=0
        __cluster_deprovisioned=0
        __cluster_cleanup_attempted=0
        CLUSTER_NAME=generic-test-cluster
        CLUSTER_CONFIG="$TEST_TMP_DIR/generic-cluster.yaml"
        KUBECONFIG_PATH="$TEST_TMP_DIR/generic-kubeconfig"
        CLUSTER_MANAGE_LOG_PATH="$TEST_TMP_DIR/generic-cluster.log"

        cp() {
            if [[ "$FAKE_FAILURE_STAGE" == pre-create ]]; then
                return 41
            fi
            command cp "$@"
        }
        eksctl() {
            if [[ "$1" == create ]]; then
                if [[ $__cluster_created -ne 1 ]]; then
                    return 99
                fi
                echo "generic-create-armed" >> "$FAKE_CALLS_FILE"
                return 41
            fi
            if [[ "$1" == delete ]]; then
                echo "generic-delete" >> "$FAKE_CALLS_FILE"
                return 0
            fi
            return 2
        }
        on_error() {
            exit "$1"
        }

        install_cleanup_traps
        provision_cluster
    ' "$REPO_ROOT/scripts/run-integration-tests.sh" > "${calls_file}.output" 2>&1
    local result=$?
    set -e

    echo "$result"
}

run_kops_provision_case() {
    local calls_file=$1
    local case_dir="$TMP_DIR/kops-provision"
    local fake_kops_template="$case_dir/fake-kops"

    mkdir -p "$case_dir/home/.ssh" "$case_dir/work"
    : > "$case_dir/home/.ssh/devopsinuse"
    : > "$case_dir/home/.ssh/devopsinuse.pub"
    : > "$calls_file"
    # Generate a fake executable whose variables are evaluated when it runs.
    # shellcheck disable=SC2016
    printf '%s\n' \
        '#!/usr/bin/env bash' \
        'set -eu' \
        'if [[ "$1" == create ]]; then' \
        '    [[ -f "$FAKE_STATE_FILE" ]] || exit 91' \
        '    [[ "$(stat -c "%a" "$FAKE_STATE_FILE")" == 600 ]] || exit 92' \
        '    expected=$(printf "%s\n%s" "$FAKE_EXPECTED_CLUSTER" "$FAKE_EXPECTED_STORE")' \
        '    [[ "$(cat "$FAKE_STATE_FILE")" == "$expected" ]] || exit 93' \
        'fi' \
        'if [[ "$1" == validate ]]; then' \
        '    echo "cluster is ready"' \
        'fi' \
        'echo "$*" >> "$FAKE_KOPS_CALLS"' > "$fake_kops_template"
    chmod +x "$fake_kops_template"

    set +e
    (
        set -Euo pipefail
        export HOME="$case_dir/home"
        cd "$case_dir/work"

        SCRIPT_DIR="$REPO_ROOT/scripts"
        # shellcheck disable=SC1091
        source "$REPO_ROOT/scripts/lib/cluster.sh"

        export AWS_ACCOUNT_ID=123456789012
        export AWS_DEFAULT_REGION=us-west-2
        export KOPS_VERSION=v1.36.0
        export TEST_ID=123-1
        export K8S_VERSION=1.36.2
        export MANIFEST_CNI_VERSION=master
        export KOPS_CLEANUP_STATE_FILE="$case_dir/cleanup-state"
        export FAKE_STATE_FILE=$KOPS_CLEANUP_STATE_FILE
        export FAKE_EXPECTED_CLUSTER=kops-cni-test-cluster-123-1.k8s.local
        export FAKE_EXPECTED_STORE=s3://kops-cni-test-eks-123456789012
        export FAKE_KOPS_CALLS=$calls_file
        export FAKE_KOPS_TEMPLATE=$fake_kops_template
        __cluster_created=0

        # up-kops-cluster invokes these mocks indirectly.
        # shellcheck disable=SC2317
        aws() {
            return 0
        }
        # shellcheck disable=SC2317
        curl() {
            cp "$FAKE_KOPS_TEMPLATE" kops-linux-amd64
        }
        # shellcheck disable=SC2317
        sleep() {
            return 0
        }
        # shellcheck disable=SC2317
        kubectl() {
            return 0
        }

        up-kops-cluster
        [[ $__cluster_created -eq 1 ]]
    ) > "${calls_file}.output" 2>&1
    local result=$?
    set -e

    echo "$result"
}

run_cluster_delete_case() {
    local cluster_type=$1
    local delete_status=$2
    local calls_file=$3
    local cluster_absent=${4:-false}

    : > "$calls_file"
    set +e
    (
        SCRIPT_DIR="$REPO_ROOT/scripts"
        # shellcheck disable=SC1091
        source "$REPO_ROOT/scripts/lib/cluster.sh"

        FAKE_DELETE_STATUS=$delete_status
        FAKE_CALLS_FILE=$calls_file
        case "$cluster_type" in
            generic)
                CLUSTER_NAME=generic-test-cluster
                export CLUSTER_MANAGE_LOG_PATH="$TMP_DIR/generic-delete.log"
                # down-test-cluster invokes this mock indirectly.
                # shellcheck disable=SC2317
                eksctl() {
                    return "$FAKE_DELETE_STATUS"
                }
                down-test-cluster
                ;;
            kops)
                CLUSTER_NAME=kops-cluster
                export KOPS_BIN=record_kops
                export KOPS_DELETE_DELAY_SECONDS=240
                export KOPS_DELETE_ATTEMPTS=2
                export KOPS_DELETE_RETRY_DELAY_SECONDS=10
                FAKE_CLUSTER_ABSENT=$cluster_absent
                # down-kops-cluster invokes these mocks indirectly.
                # shellcheck disable=SC2317
                sleep() {
                    echo "sleep $*" >> "$FAKE_CALLS_FILE"
                }
                # shellcheck disable=SC2317
                record_kops() {
                    if [[ "$1" == get ]]; then
                        if [[ "$FAKE_CLUSTER_ABSENT" == true ]]; then
                            echo "cluster not found \"$CLUSTER_NAME\"" >&2
                            return 1
                        fi
                        return 0
                    fi
                    echo "kops $*" >> "$FAKE_CALLS_FILE"
                    return "$FAKE_DELETE_STATUS"
                }
                down-kops-cluster
                ;;
            *)
                echo "Unknown cluster type: $cluster_type" >&2
                exit 2
                ;;
        esac
    ) > "${calls_file}.output" 2>&1
    local result=$?
    set -e

    echo "$result"
}

run_kops_cleanup_script_case() {
    local delete_status=$1
    local state_file=$2
    local calls_file=$3
    local fake_kops=$4
    local cluster_absent=${5:-false}

    printf '%s\n%s\n' \
        'kops-cni-test-cluster-123-1.k8s.local' \
        's3://kops-cni-test-eks-123456789012' > "$state_file"
    : > "$calls_file"

    set +e
    FAKE_KOPS_CALLS=$calls_file \
        FAKE_KOPS_STATUS=$delete_status \
        FAKE_KOPS_ABSENT=$cluster_absent \
        KOPS_BIN=$fake_kops \
        KOPS_DELETE_DELAY_SECONDS=0 \
        KOPS_DELETE_ATTEMPTS=2 \
        KOPS_DELETE_RETRY_DELAY_SECONDS=0 \
        "$REPO_ROOT/scripts/cleanup-kops-cluster.sh" "$state_file" \
        > "${calls_file}.output" 2>&1
    local result=$?
    set -e

    echo "$result"
}

calls_file="$TMP_DIR/calls"

result=$(run_cleanup_case performance exit 7 0 1 0 true false "$calls_file")
assert_equal 7 "$result" "performance cleanup must preserve the test failure"
assert_call_count 1 performance-delete "$calls_file" "in-progress performance test must clean up"

result=$(run_cleanup_case performance exit 7 0 1 0 true true "$calls_file")
assert_equal 7 "$result" "repeated signal must not replace the test failure"
assert_call_count 1 performance-delete "$calls_file" "repeated signal must not interrupt cleanup"

result=$(run_cleanup_case performance exit 0 23 1 0 true false "$calls_file")
assert_equal 23 "$result" "cleanup failure must fail an otherwise successful run"

result=$(run_cleanup_case performance exit 7 23 1 0 true false "$calls_file")
assert_equal 7 "$result" "cleanup failure must not hide the test failure"

result=$(run_cleanup_case performance exit 0 0 1 1 true false "$calls_file")
assert_equal 0 "$result" "already-deprovisioned run must retain success"
assert_call_count 0 performance-delete "$calls_file" "already-deprovisioned cluster must not be deleted again"

result=$(run_cleanup_case performance exit 9 0 1 0 false false "$calls_file")
assert_equal 9 "$result" "disabled deprovisioning must preserve the test failure"
assert_call_count 0 performance-delete "$calls_file" "disabled deprovisioning must skip cleanup"

result=$(run_cleanup_case kops exit 7 0 1 0 true false "$calls_file")
assert_equal 7 "$result" "kOps cleanup must preserve the test failure"
assert_call_count 1 kops-delete "$calls_file" "failed kOps run must attempt cleanup"

result=$(run_cleanup_case kops exit 0 23 1 0 true false "$calls_file")
assert_equal 23 "$result" "kOps cleanup failure must fail a successful run"

result=$(run_cleanup_case kops exit 17 0 1 0 true false "$calls_file" 1)
assert_equal 17 "$result" "an earlier cleanup failure must preserve the original status"
assert_call_count 0 kops-delete "$calls_file" "EXIT cleanup must not retry an attempted deletion"

result=$(run_cleanup_case performance error 29 0 1 0 true false "$calls_file")
assert_equal 29 "$result" "ERR trap must preserve the command failure"
assert_call_count 1 performance-delete "$calls_file" "ERR trap must run cleanup"

result=$(run_cleanup_case performance INT 0 0 1 0 true false "$calls_file")
assert_equal 130 "$result" "INT trap must use the conventional status"
assert_call_count 1 performance-delete "$calls_file" "INT trap must run cleanup"

result=$(run_cleanup_case performance TERM 0 0 1 0 true false "$calls_file")
assert_equal 143 "$result" "TERM trap must use the conventional status"
assert_call_count 1 performance-delete "$calls_file" "TERM trap must run cleanup"

result=$(run_cleanup_case performance deprovision 0 0 1 0 true true "$calls_file")
assert_equal 143 "$result" "signal during normal deletion must preserve the signal status"
assert_call_count 2 performance-delete "$calls_file" "signal-interrupted deletion must be retried by EXIT cleanup"

result=$(run_generic_provision_case pre-create "$calls_file")
assert_equal 41 "$result" "pre-create failure must be preserved"
assert_call_count 0 generic-delete "$calls_file" "pre-create failure must not run cluster deletion"

result=$(run_generic_provision_case create "$calls_file")
assert_equal 1 "$result" "eksctl create failure must be preserved"
assert_call_count 1 generic-create-armed "$calls_file" "cleanup must be armed immediately before eksctl create"
assert_call_count 1 generic-delete "$calls_file" "partial eksctl creation must run cleanup"

result=$(run_kops_provision_case "$calls_file")
assert_equal 0 "$result" "kOps provisioning state handoff must succeed"
assert_prefix_count 1 "create cluster " "$calls_file" "kOps state must be written before create"

result=$(run_cluster_delete_case generic 37 "$calls_file")
assert_equal 37 "$result" "generic deletion must preserve the eksctl status"

result=$(run_cluster_delete_case kops 0 "$calls_file")
assert_equal 0 "$result" "kOps deletion must propagate success"
assert_equal "sleep 240" "$(sed -n '1p' "$calls_file")" "kOps deletion must wait before deleting"
assert_equal "kops delete cluster --name kops-cluster --yes" "$(sed -n '2p' "$calls_file")" "kOps deletion must follow the delay"

result=$(run_cluster_delete_case kops 37 "$calls_file")
assert_equal 37 "$result" "kOps deletion must preserve the kOps status"
assert_call_count 2 "kops delete cluster --name kops-cluster --yes" "$calls_file" "kOps deletion must use the bounded retry"
assert_call_count 1 "sleep 10" "$calls_file" "kOps retry must use the configured delay"

result=$(run_cluster_delete_case kops 37 "$calls_file" true)
assert_equal 0 "$result" "already-absent kOps cluster must be treated as cleaned up"
assert_call_count 0 "kops delete cluster --name kops-cluster --yes" "$calls_file" "absent kOps cluster must not be deleted again"

fake_kops="$TMP_DIR/kops"
cat > "$fake_kops" <<'KOPS'
#!/usr/bin/env bash
if [[ "$1" == get ]]; then
    if [[ "$FAKE_KOPS_ABSENT" == true ]]; then
        echo "cluster not found \"$3\"" >&2
        exit 1
    fi
    exit 0
fi
echo "$*" >> "$FAKE_KOPS_CALLS"
exit "$FAKE_KOPS_STATUS"
KOPS
chmod +x "$fake_kops"

state_file="$TMP_DIR/kops-cleanup-state"
result=$(run_kops_cleanup_script_case 0 "$state_file" "$calls_file" "$fake_kops")
assert_equal 0 "$result" "fallback kOps cleanup must propagate success"
assert_equal "delete cluster --name kops-cni-test-cluster-123-1.k8s.local --yes" "$(sed -n '1p' "$calls_file")" "fallback cleanup must delete the recorded cluster"
if [[ -e "$state_file" ]]; then
    echo "FAIL: successful fallback cleanup must remove its state file" >&2
    exit 1
fi

set +e
KOPS_BIN=$fake_kops "$REPO_ROOT/scripts/cleanup-kops-cluster.sh" "$state_file" > "${calls_file}.output" 2>&1
result=$?
set -e
assert_equal 0 "$result" "repeated fallback cleanup must be idempotent"

result=$(run_kops_cleanup_script_case 37 "$state_file" "$calls_file" "$fake_kops" true)
assert_equal 0 "$result" "fallback cleanup must accept an already-absent cluster"
assert_call_count 0 "delete cluster --name kops-cni-test-cluster-123-1.k8s.local --yes" "$calls_file" "already-absent fallback must not delete again"
if [[ -e "$state_file" ]]; then
    echo "FAIL: already-absent fallback cleanup must remove its state file" >&2
    exit 1
fi

result=$(run_kops_cleanup_script_case 37 "$state_file" "$calls_file" "$fake_kops")
assert_equal 37 "$result" "fallback kOps cleanup must preserve deletion failure"
assert_call_count 2 "delete cluster --name kops-cni-test-cluster-123-1.k8s.local --yes" "$calls_file" "fallback kOps cleanup must bound retries"
if [[ ! -e "$state_file" ]]; then
    echo "FAIL: failed fallback cleanup must preserve its state file" >&2
    exit 1
fi

grep -q '^install_cleanup_traps$' "$REPO_ROOT/scripts/run-integration-tests.sh"

echo "PASS: cleanup tests"
