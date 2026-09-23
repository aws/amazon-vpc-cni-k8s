#!/usr/bin/env bash

# Mocks are invoked indirectly by the sourced cleanup functions.
# shellcheck disable=SC2034,SC2317

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
CALLS_FILE=$(mktemp)
trap 'rm -f "$CALLS_FILE"' EXIT

TRIGGER_STATUS=0
SIGNAL_DURING_DELETE=false

fail() {
    echo "FAIL: $1" >&2
    exit 1
}

verify_cleanup_case() {
    local actual_status
    local call_count

    : > "$CALLS_FILE"
    set +e
    (
        set -Euo pipefail
        source "$REPO_ROOT/scripts/lib/cleanup.sh"

        RUN_KOPS_TEST=false
        RUN_BOTTLEROCKET_TEST=false
        RUN_PERFORMANCE_TESTS=false
        DEPROVISION=true
        CLUSTER_NAME=test-cluster
        __cluster_created=1
        __cluster_deprovisioned=0

        if [[ "$CLUSTER_TYPE" == kops ]]; then
            RUN_KOPS_TEST=true
        else
            RUN_PERFORMANCE_TESTS=true
            # The former in-progress guard must not suppress EXIT cleanup.
            RUNNING_PERFORMANCE=true
        fi

        record_delete() {
            echo "$CLUSTER_TYPE-delete" >> "$CALLS_FILE"
            if [[ "$SIGNAL_DURING_DELETE" == true ]]; then
                SIGNAL_DURING_DELETE=false
                kill -TERM "$BASHPID"
                kill -INT "$BASHPID"
            fi
            return "$DELETE_STATUS"
        }
        eksctl() {
            record_delete
        }
        down-kops-cluster() {
            record_delete
        }
        on_error() {
            exit "$1"
        }

        trap 'on_error $? $LINENO' ERR
        trap cleanup_on_exit EXIT
        trap 'exit 130' INT
        trap 'exit 143' TERM

        case "$TRIGGER" in
            exit)
                exit "$TRIGGER_STATUS"
                ;;
            error)
                bash -c 'exit "$1"' _ "$TRIGGER_STATUS"
                ;;
            INT | TERM)
                kill "-$TRIGGER" "$BASHPID"
                ;;
            deprovision)
                trap - EXIT
                trap '' INT TERM
                deprovision_cluster
                ;;
            *)
                exit 2
                ;;
        esac
    )
    actual_status=$?
    set -e

    call_count=$(grep -Fxc "$CLUSTER_TYPE-delete" "$CALLS_FILE" || true)
    [[ $actual_status -eq $EXPECTED_STATUS ]] ||
        fail "$TEST_NAME: expected status $EXPECTED_STATUS, got $actual_status"
    [[ $call_count -eq 1 ]] ||
        fail "$TEST_NAME: expected one delete call, got $call_count"
}

TEST_NAME="performance failure survives cleanup signals and deletion failure" \
    CLUSTER_TYPE=performance TRIGGER=exit TRIGGER_STATUS=7 \
    DELETE_STATUS=23 SIGNAL_DURING_DELETE=true EXPECTED_STATUS=7 verify_cleanup_case

TEST_NAME="ERR cleanup" \
    CLUSTER_TYPE=performance TRIGGER=error TRIGGER_STATUS=29 \
    DELETE_STATUS=0 EXPECTED_STATUS=29 verify_cleanup_case

TEST_NAME="INT cleanup" \
    CLUSTER_TYPE=performance TRIGGER=INT \
    DELETE_STATUS=0 EXPECTED_STATUS=130 verify_cleanup_case

TEST_NAME="TERM cleanup" \
    CLUSTER_TYPE=performance TRIGGER=TERM \
    DELETE_STATUS=0 EXPECTED_STATUS=143 verify_cleanup_case

TEST_NAME="cleanup failure after success" \
    CLUSTER_TYPE=performance TRIGGER=exit \
    DELETE_STATUS=23 EXPECTED_STATUS=23 verify_cleanup_case

TEST_NAME="normal teardown failure" \
    CLUSTER_TYPE=performance TRIGGER=deprovision \
    DELETE_STATUS=23 EXPECTED_STATUS=23 verify_cleanup_case

TEST_NAME="signal during normal teardown" \
    CLUSTER_TYPE=performance TRIGGER=deprovision \
    DELETE_STATUS=0 SIGNAL_DURING_DELETE=true EXPECTED_STATUS=0 verify_cleanup_case

TEST_NAME="kOps failure cleanup" \
    CLUSTER_TYPE=kops TRIGGER=exit TRIGGER_STATUS=7 \
    DELETE_STATUS=0 EXPECTED_STATUS=7 verify_cleanup_case

echo "PASS: cleanup control flow"
