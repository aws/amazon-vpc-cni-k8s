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

    GOPATH=$(go env GOPATH)
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
