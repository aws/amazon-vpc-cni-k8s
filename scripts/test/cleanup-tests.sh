#!/usr/bin/env bash

# Mocks are invoked indirectly by the sourced cleanup functions.
# shellcheck disable=SC2034,SC2317

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
CALLS_FILE=$(mktemp)
trap 'rm -f "$CALLS_FILE"' EXIT

fail() {
    echo "FAIL: $1" >&2
    exit 1
}

run_case() {
    local cluster_type=$1
    local trigger=$2
    local trigger_status=$3
    local delete_status=$4
    local signal_during_delete=$5
    local expected_status=$6
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

        if [[ "$cluster_type" == kops ]]; then
            RUN_KOPS_TEST=true
        else
            RUN_PERFORMANCE_TESTS=true
            # The former in-progress guard must not suppress EXIT cleanup.
            RUNNING_PERFORMANCE=true
        fi

        record_delete() {
            echo "$cluster_type-delete" >> "$CALLS_FILE"
            if [[ "$signal_during_delete" == true ]]; then
                signal_during_delete=false
                kill -TERM "$BASHPID"
                kill -INT "$BASHPID"
            fi
            return "$delete_status"
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

        case "$trigger" in
            exit)
                exit "$trigger_status"
                ;;
            error)
                bash -c 'exit "$1"' _ "$trigger_status"
                ;;
            INT | TERM)
                kill "-$trigger" "$BASHPID"
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

    call_count=$(grep -Fxc "$cluster_type-delete" "$CALLS_FILE" || true)
    [[ $actual_status -eq $expected_status ]] ||
        fail "$cluster_type $trigger: expected status $expected_status, got $actual_status"
    [[ $call_count -eq 1 ]] ||
        fail "$cluster_type $trigger: expected one delete call, got $call_count"
}

run_case performance exit 7 0 false 7
run_case performance error 29 0 false 29
run_case performance INT 0 0 false 130
run_case performance TERM 0 0 false 143
run_case performance exit 7 0 true 7
run_case performance exit 0 23 false 23
run_case performance exit 7 23 false 7
run_case performance deprovision 0 23 false 23
run_case performance deprovision 0 0 true 0
run_case kops exit 7 0 false 7

echo "PASS: cleanup control flow"
