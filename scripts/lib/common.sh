#!/usr/bin/env bash

check_is_installed() {
    local __name="$1"
    if ! is_installed "$__name"; then
        echo "Please install $__name before running this script."
        exit 1
    fi
}

is_installed() {
    local __name="$1"
    if $(which $__name >/dev/null 2>&1); then
        return 0
    else
        return 1
    fi
}

function display_timelines() {
    echo ""
    echo "Displaying all step durations."
    echo "TIMELINE: Upping test cluster took $UP_CLUSTER_DURATION seconds."
    if [[ $RUN_INTEGRATION_DEFAULT_CNI == true ]]; then
        echo "TIMELINE: Default CNI integration tests took $DEFAULT_INTEGRATION_DURATION seconds."
    fi
    echo "TIMELINE: Current image integration tests took $CURRENT_IMAGE_INTEGRATION_DURATION seconds."
    if [[ "$RUN_CONFORMANCE" == true ]]; then
        echo "TIMELINE: Conformance tests took $CONFORMANCE_DURATION seconds."
    fi
    if [[ "$RUN_PERFORMANCE_TESTS" == true ]]; then
        echo "TIMELINE: Performance tests took $PERFORMANCE_DURATION seconds."
    fi
    echo "TIMELINE: Down processes took $DOWN_DURATION seconds."
}

