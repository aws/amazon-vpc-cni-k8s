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
    echo "TIMELINE: Docker build took $DOCKER_BUILD_DURATION seconds."
    echo "TIMELINE: Upping test cluster took $UP_CLUSTER_DURATION seconds."
    echo "TIMELINE: Default CNI integration tests took $DEFAULT_INTEGRATION_DURATION seconds." 
    echo "TIMELINE: Updating CNI image took $CNI_IMAGE_UPDATE_DURATION seconds."
    echo "TIMELINE: Current image integration tests took $CURRENT_IMAGE_INTEGRATION_DURATION seconds."
    echo "TIMELINE: Conformance tests took $CONFORMANCE_DURATION seconds."
    echo "TIMELINE: Down processes took $DOWN_DURATION seconds."
}

function run_warm_ip_test() {
    $KUBECTL_PATH set env ds aws-node -n kube-system WARM_IP_TARGET=2
    $KUBECTL_PATH set env ds aws-node -n kube-system MINIMUM_IP_TARGET=10
    #Sleep a couple seconds to ensure propogation
    sleep 2
    KUBECTL_WARM_IP_TARGET=$(kubectl describe ds -n kube-system | grep WARM_IP_TARGET)
    KUBECTL_MINIMUM_IP_TARGET=$(kubectl describe ds -n kube-system | grep MINIMUM_IP_TARGET)
    if [[ $KUBECTL_WARM_IP_TARGET != *"2"* || $KUBECTL_MINIMUM_IP_TARGET != *"10"* ]]; then
        echo "WARM_IP_TARGET not propogated!"
        on_error
    fi
}
