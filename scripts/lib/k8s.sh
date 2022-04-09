#!/usr/bin/env bash

# check_ds_rollout watches the status of the latest rollout until it's done or
# until the timeout. Namespace and timeout are optional parameters
check_ds_rollout() {
    local __ds_name=${1:-}
    local __namespace=${2:-}
    local __timeout=${3:-"2m"}
    local __args=""
    if [ -n "$__namespace" ]; then
        __args="$__args-n $__namespace"
    fi
    kubectl rollout status ds/"$__ds_name" $__args --timeout=$__timeout
}