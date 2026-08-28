#!/usr/bin/env bash

# Deprovision the active test cluster and mark it complete only after deletion
# succeeds. Keeping this in one place lets the normal and EXIT paths share the
# same behavior.
deprovision_cluster() {
    local deprovision_status=0

    if [[ "$RUN_KOPS_TEST" == true ]]; then
        down-kops-cluster || deprovision_status=$?
    elif [[ "$RUN_BOTTLEROCKET_TEST" == true ]]; then
        eksctl delete cluster "$CLUSTER_NAME" --disable-nodegroup-eviction || deprovision_status=$?
    elif [[ "$RUN_PERFORMANCE_TESTS" == true ]]; then
        eksctl delete cluster "$CLUSTER_NAME" || deprovision_status=$?
    else
        down-test-cluster || deprovision_status=$?
    fi

    if [[ $deprovision_status -eq 0 ]]; then
        __cluster_deprovisioned=1
    fi

    return "$deprovision_status"
}

# Preserve the test result while still making a best-effort attempt to remove
# any cluster created by the run. A cleanup failure only replaces a successful
# test result.
cleanup_on_exit() {
    local original_status=$?
    local cleanup_status=0

    trap - EXIT ERR INT TERM
    set +e

    # These lifecycle flags are initialized by run-integration-tests.sh.
    # shellcheck disable=SC2154
    if [[ $original_status -ne 0 && "$RUN_KOPS_TEST" == true && $__cluster_created -eq 1 && $__cluster_deprovisioned -eq 0 ]]; then
        collect_kops_diagnostics
    fi

    if [[ "$RUNNING_PERFORMANCE" == false && $__cluster_created -eq 1 && $__cluster_deprovisioned -eq 0 && "$DEPROVISION" == true ]]; then
        echo "Cluster was provisioned already. Deprovisioning it..."
        deprovision_cluster
        cleanup_status=$?
    fi

    if [[ $original_status -ne 0 ]]; then
        exit "$original_status"
    fi

    exit "$cleanup_status"
}
