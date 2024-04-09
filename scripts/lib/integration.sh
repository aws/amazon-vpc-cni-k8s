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

    /tmp/e2e.test --ginkgo.focus="Conformance" --ginkgo.timeout 120m --kubeconfig=$KUBECONFIG --ginkgo.v --ginkgo.fail-fast  --ginkgo.flake-attempts 2 \
	    --ginkgo.skip="(works for CRD with validation schema)|(ServiceAccountIssuerDiscovery should support OIDC discovery of service account issuer)|(should support remote command execution over websockets)|(should support retrieving logs from the container over websockets)|(Basic StatefulSet functionality [StatefulSetBasic])|\[Slow\]|\[Serial\]"

    /tmp/e2e.test --ginkgo.focus="\[Serial\].*Conformance" --kubeconfig=$KUBECONFIG --ginkgo.v --ginkgo.fail-fast --ginkgo.flake-attempts 2 \
	    --ginkgo.skip="(ServiceAccountIssuerDiscovery should support OIDC discovery of service account issuer)|(should support remote command execution over websockets)|(should support retrieving logs from the container over websockets)|\[Slow\]"
    echo "Kops conformance tests ran successfully!"

    KOPS_TEST_DURATION=$((SECONDS - START))
    echo "TIMELINE: KOPS tests took $KOPS_TEST_DURATION seconds."

    sleep 240 #Workaround to avoid ENI leakage during cluster deletion: https://github.com/aws/amazon-vpc-cni-k8s/issues/1223
}

function build_and_push_image(){
  command=$1
  args=$2
  START=$SECONDS
  # Refer to https://github.com/docker/buildx#building-multi-platform-images for the multi-arch image build process.
  # create the buildx container only if it doesn't exist already.
  docker buildx inspect "$BUILDX_BUILDER" >/dev/null 2<&1 || docker buildx create --name="$BUILDX_BUILDER" --buildkitd-flags '--allow-insecure-entitlement network.host' --use >/dev/null
  make $command $args
  echo "TIMELINE: Docker build took $(($SECONDS - $START)) seconds."
}
