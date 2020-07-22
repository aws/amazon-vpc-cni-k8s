function run_kops_conformance() {
    START=$SECONDS

    export KUBECONFIG=~/.kube/config
    kubectl apply -f "$TEST_CONFIG_PATH"
    sleep 5
    while [[ $(kubectl describe ds aws-node -n=kube-system | grep "Available Pods: 0") ]]
    do
        sleep 5
        echo "Waiting for daemonset update"
    done
    echo "Updated!"

    go install github.com/onsi/ginkgo/ginkgo
    wget -qO- https://dl.k8s.io/v$K8S_VERSION/kubernetes-test.tar.gz | tar -zxvf - --strip-components=4 -C /tmp  kubernetes/platforms/linux/amd64/e2e.test

    $GOPATH/bin/ginkgo -p --focus="Conformance"  --failFast --flakeAttempts 2 \
    --skip="(should support remote command execution over websockets)|(should support retrieving logs from the container over websockets)|(Basic StatefulSet functionality [StatefulSetBasic])|\[Slow\]|\[Serial\]" /tmp/e2e.test -- --kubeconfig=$KUBECONFIG

    /tmp/e2e.test --ginkgo.focus="\[Serial\].*Conformance" --kubeconfig=$KUBECONFIG --ginkgo.failFast --ginkgo.flakeAttempts 2 \
    --ginkgo.skip="(should support remote command execution over websockets)|(should support retrieving logs from the container over websockets)|\[Slow\]"
    echo "Kops conformance tests ran successfully!"

    KOPS_TEST_DURATION=$((SECONDS - START))
    echo "TIMELINE: KOPS tests took $KOPS_TEST_DURATION seconds."

    START=$SECONDS
    down-kops-cluster
    DOWN_KOPS_DURATION=$((SECONDS - START))
    echo "TIMELINE: Down KOPS cluster took $DOWN_KOPS_DURATION seconds."
    exit 0
}

function setup_warm_ip_test() {
    echo "Setting WARM_IP_TARGET=2 and MINUMUM_IP_TARGET=10"
    echo "Running warm ip test"
    $KUBECTL_PATH set env ds aws-node -n kube-system WARM_IP_TARGET=2
    $KUBECTL_PATH set env ds aws-node -n kube-system MINIMUM_IP_TARGET=10
    KUBECTL_WARM_IP_TARGET=$($KUBECTL_PATH describe ds -n kube-system | grep WARM_IP_TARGET)
    KUBECTL_MINIMUM_IP_TARGET=$($KUBECTL_PATH describe ds -n kube-system | grep MINIMUM_IP_TARGET)
    if [[ $KUBECTL_WARM_IP_TARGET != *"2"* || $KUBECTL_MINIMUM_IP_TARGET != *"10"* ]]; then
        echo "WARM_IP_TARGET not propogated in daemonset!"
        on_error
    fi

    FIRST_DS_POD_NAME=$($KUBECTL_PATH get pods -n kube-system | grep aws-node | sed -n '1 p' | awk '{print $1}')
    SECOND_DS_POD_NAME=$($KUBECTL_PATH get pods -n kube-system | grep aws-node | sed -n '2 p' | awk '{print $1}')
    START=$SECONDS
    while [[ $($KUBECTL_PATH describe pod $FIRST_DS_POD_NAME -n=kube-system | grep WARM_IP_TARGET) != *"2"* || \
        $($KUBECTL_PATH describe pod $FIRST_DS_POD_NAME -n=kube-system | grep MINIMUM_IP_TARGET) != *"10"* || \
        $($KUBECTL_PATH describe pod $SECOND_DS_POD_NAME -n=kube-system | grep WARM_IP_TARGET) != *"2"* || \
        $($KUBECTL_PATH describe pod $SECOND_DS_POD_NAME -n=kube-system | grep MINIMUM_IP_TARGET)  != *"10"* ]]
    do
        echo "Waiting for daemonset pod propagation..."
        FIRST_DS_POD_NAME=$($KUBECTL_PATH get pods -n kube-system | grep aws-node | sed -n '1 p' | awk '{print $1}')
        SECOND_DS_POD_NAME=$($KUBECTL_PATH get pods -n kube-system | grep aws-node | sed -n '2 p' | awk '{print $1}')
        sleep 5
        if [[ $((SECONDS - START)) -gt 500 ]]; then
            echo "Propogation for WARM_IP_TARGET timed out!"
            on_error
        fi
    done
    echo "WARM_IP_TARGET and MINIMUM_IP_TARGET successfully propagated!"
    echo $WARM_IP_VALUE1
    echo $MINIMUM_IP_VALUE1
}

function setup_warm_eni_test() {
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
    START=$SECONDS
    while [[ $($KUBECTL_PATH describe pod $FIRST_DS_POD_NAME -n=kube-system | grep WARM_ENI_TARGET) != *"0"* || \
        $($KUBECTL_PATH describe pod $SECOND_DS_POD_NAME -n=kube-system | grep WARM_ENI_TARGET) != *"0"* ]]
    do
        echo "Waiting for daemonset pod propagation..."
        FIRST_DS_POD_NAME=$($KUBECTL_PATH get pods -n kube-system | grep aws-node | sed -n '1 p' | awk '{print $1}')
        SECOND_DS_POD_NAME=$($KUBECTL_PATH get pods -n kube-system | grep aws-node | sed -n '2 p' | awk '{print $1}')
        sleep 5
        if [[ $((SECONDS - START)) -gt 500 ]]; then
            echo "Propogation for WARM_IP_TARGET timed out!"
            on_error
        fi
    done
    echo "WARM_ENI_TARGET successfully propagated!"
    echo $WARM_ENI_VALUE1
}
