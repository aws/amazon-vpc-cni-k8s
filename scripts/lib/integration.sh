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

    wget -qO- https://dl.k8s.io/v$K8S_VERSION/kubernetes-test-linux-amd64.tar.gz | tar -zxvf - --strip-components=3 -C /tmp  kubernetes/test/bin/e2e.test

    /tmp/e2e.test --ginkgo.focus="Conformance" --kubeconfig=$KUBECONFIG --ginkgo.failFast --ginkgo.flakeAttempts 2 \
	    --ginkgo.skip="(ServiceAccountIssuerDiscovery should support OIDC discovery of service account issuer)|(should support remote command execution over websockets)|(should support retrieving logs from the container over websockets)|(Basic StatefulSet functionality [StatefulSetBasic])|\[Slow\]|\[Serial\]"

    /tmp/e2e.test --ginkgo.focus="\[Serial\].*Conformance" --kubeconfig=$KUBECONFIG --ginkgo.failFast --ginkgo.flakeAttempts 2 \
	    --ginkgo.skip="(ServiceAccountIssuerDiscovery should support OIDC discovery of service account issuer)|(should support remote command execution over websockets)|(should support retrieving logs from the container over websockets)|\[Slow\]"
    echo "Kops conformance tests ran successfully!"

    KOPS_TEST_DURATION=$((SECONDS - START))
    echo "TIMELINE: KOPS tests took $KOPS_TEST_DURATION seconds."

    sleep 240 #Workaround to avoid ENI leakage during cluster deletion: https://github.com/aws/amazon-vpc-cni-k8s/issues/1223
    START=$SECONDS
    down-kops-cluster
    emit_cloudwatch_metric "kops_test_status" "1"
    DOWN_KOPS_DURATION=$((SECONDS - START))
    echo "TIMELINE: Down KOPS cluster took $DOWN_KOPS_DURATION seconds."
    exit 0
}
