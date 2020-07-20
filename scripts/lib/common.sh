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
    echo "Setting WARM_IP_TARGET=2 and MINUMUM_IP_TARGET=10"
    echo "Running warm ip test"
    $KUBECTL_PATH set env ds aws-node -n kube-system WARM_IP_TARGET=2
    $KUBECTL_PATH set env ds aws-node -n kube-system MINIMUM_IP_TARGET=10
    #Sleep a couple seconds to ensure propagation
    sleep 2
    KUBECTL_WARM_IP_TARGET=$($KUBECTL_PATH describe ds -n kube-system | grep WARM_IP_TARGET)
    KUBECTL_MINIMUM_IP_TARGET=$($KUBECTL_PATH describe ds -n kube-system | grep MINIMUM_IP_TARGET)
    if [[ $KUBECTL_WARM_IP_TARGET != *"2"* || $KUBECTL_MINIMUM_IP_TARGET != *"10"* ]]; then
        echo "WARM_IP_TARGET not propogated in daemonset!"
        on_error
    fi

    sleep 140
    FIRST_DS_POD_NAME=$($KUBECTL_PATH get pods -n kube-system | grep aws-node | sed -n '1 p' | awk '{print $1}')
    SECOND_DS_POD_NAME=$($KUBECTL_PATH get pods -n kube-system | grep aws-node | sed -n '2 p' | awk '{print $1}')
    WARM_IP_VALUE1=""
    WARM_IP_VALUE2=""
    MINIMUM_IP_VALUE1=""
    MINIMUM_IP_VALUE2=""
    while [[ $($KUBECTL_PATH describe pod $FIRST_DS_POD_NAME -n=kube-system | grep WARM_IP_TARGET) != *"2"* || \
        $($KUBECTL_PATH describe pod $FIRST_DS_POD_NAME -n=kube-system | grep MINIMUM_IP_TARGET) != *"10"* || \
        $($KUBECTL_PATH describe pod $SECOND_DS_POD_NAME -n=kube-system | grep WARM_IP_TARGET) != *"2"* || \
        $($KUBECTL_PATH describe pod $SECOND_DS_POD_NAME -n=kube-system | grep MINIMUM_IP_TARGET)  != *"10"* ]]
    do
        echo "Waiting for daemonset pod propagation..."
        $KUBECTL_PATH get pods -n kube-system
        FIRST_DS_POD_NAME=$($KUBECTL_PATH get pods -n kube-system | grep aws-node | sed -n '1 p' | awk '{print $1}')
        SECOND_DS_POD_NAME=$($KUBECTL_PATH get pods -n kube-system | grep aws-node | sed -n '2 p' | awk '{print $1}')
        sleep 5
    done
    echo "WARM_IP_TARGET and MINIMUM_IP_TARGET successfully propagated!"
    echo $WARM_IP_VALUE1
    echo $MINIMUM_IP_VALUE1
}

function run_warm_eni_test() {
    echo "Setting WARM_ENI_TARGET=0"
    echo "Running warm eni test"
    $KUBECTL_PATH set env ds aws-node -n kube-system WARM_ENI_TARGET=0
    #Sleep a couple seconds to ensure propagation
    sleep 2
    KUBECTL_WARM_ENI_TARGET=$($KUBECTL_PATH describe ds -n kube-system | grep WARM_ENI_TARGET)
    if [[ $KUBECTL_WARM_ENI_TARGET != *"0" ]]; then
        echo "WARM_ENI_TARGET not propogated in daemonset!"
        on_error
    fi
    FIRST_DS_POD_NAME=$($KUBECTL_PATH get pods -n kube-system | grep aws-node | sed -n '1 p' | awk '{print $1}')
    SECOND_DS_POD_NAME=$($KUBECTL_PATH get pods -n kube-system | grep aws-node | sed -n '2 p' | awk '{print $1}')
    WARM_ENI_VALUE1=""
    WARM_ENI_VALUE2=""
    while [[ $WARM_ENI_VALUE1 != *"0"* || $WARM_ENI_VALUE2 != *"0"* ]]
    do
        echo "Waiting for daemonset pod propagation..."
        FIRST_DS_POD_NAME=$($KUBECTL_PATH get pods -n kube-system | grep aws-node | sed -n '1 p' | awk '{print $1}')
        SECOND_DS_POD_NAME=$($KUBECTL_PATH get pods -n kube-system | grep aws-node | sed -n '2 p' | awk '{print $1}')
        WARM_ENI_VALUE1=$($KUBECTL_PATH describe pod $FIRST_DS_POD_NAME -n=kube-system | grep WARM_ENI_TARGET)
        WARM_ENI_VALUE2=$($KUBECTL_PATH describe pod $SECOND_DS_POD_NAME -n=kube-system | grep WARM_ENI_TARGET)
        sleep 5
    done
    echo "WARM_ENI_TARGET successfully propagated!"
    echo $WARM_ENI_VALUE1
}
