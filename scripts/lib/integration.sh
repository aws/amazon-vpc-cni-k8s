function run_kops_conformance() {
  START=$SECONDS

  export KUBECONFIG=~/.kube/config

  echo "=== Setting up test environment ==="
  echo "Current directory: $(pwd)"
  echo "KUBECONFIG path: $KUBECONFIG"
  echo "K8S_VERSION: $K8S_VERSION"

  # Download e2e test binary
  echo "=== Downloading e2e test binary ==="
  wget -qO- https://dl.k8s.io/v$K8S_VERSION/kubernetes-test-linux-amd64.tar.gz | tar -zxvf - --strip-components=3 -C /tmp kubernetes/test/bin/e2e.test

  # Apply CNI config and wait for daemonset
  echo "=== Applying CNI configuration ==="
  kubectl apply -f "$TEST_CONFIG_PATH"
  echo "Waiting for aws-node daemonset to be ready..."
  sleep 5
  while [[ $(kubectl describe ds aws-node -n=kube-system | grep "Available Pods: 0") ]]; do
    sleep 5
    echo "Still waiting for daemonset update..."
    kubectl get ds aws-node -n kube-system
  done
  echo "CNI DaemonSet is ready!"

  # Show cluster state before tests
  echo "=== Cluster State Before Tests ==="
  echo "Nodes:"
  kubectl get nodes -o wide
  echo "Pods in kube-system:"
  kubectl get pods -n kube-system
  echo "CNI DaemonSet status:"
  kubectl describe ds aws-node -n=kube-system

  # Run the focused set of tests with detailed logging
  TEST_START=$SECONDS
  TEST_RESULT=success

  /tmp/e2e.test --ginkgo.focus="Conformance" --ginkgo.timeout=120m --kubeconfig=$KUBECONFIG --ginkgo.v --ginkgo.trace --ginkgo.flake-attempts 8 \
    --ginkgo.skip="(works for CRD with validation schema)|(ServiceAccountIssuerDiscovery should support OIDC discovery of service account issuer)|(should support remote command execution over websockets)|(should support retrieving logs from the container over websockets)|(Basic StatefulSet functionality [StatefulSetBasic])|\[Slow\]|\[Serial\]" || TEST_RESULT=fail

  /tmp/e2e.test --ginkgo.focus="\[Serial\].*Conformance" --ginkgo.timeout=120m --kubeconfig=$KUBECONFIG --ginkgo.v --ginkgo.trace --ginkgo.flake-attempts 8 \
    --ginkgo.skip="(ServiceAccountIssuerDiscovery should support OIDC discovery of service account issuer)|(should support remote command execution over websockets)|(should support retrieving logs from the container over websockets)|\[Slow\]" || TEST_RESULT=fail

  TEST_DURATION=$((SECONDS - TEST_START))

  echo "=== Test Results ==="
  echo "Test duration: $TEST_DURATION seconds"
  echo "Test result: $TEST_RESULT"

  # If any test failed, return failure
  if [[ "$TEST_RESULT" == "fail" ]]; then
    echo "One or more test suites failed!"
    exit 1
  fi

  echo "All test suites passed successfully!"

  # Show cluster state after tests
  echo "=== Cluster State After Tests ==="
  echo "Nodes:"
  kubectl get nodes -o wide
  echo "Pods in kube-system:"
  kubectl get pods -n kube-system
  echo "CNI DaemonSet status:"
  kubectl describe ds aws-node -n=kube-system

  KOPS_TEST_DURATION=$((SECONDS - START))
  echo "=== Test Run Complete ==="
  echo "TIMELINE: KOPS tests took $KOPS_TEST_DURATION seconds"

  # Workaround to avoid ENI leakage during cluster deletion
  # See: https://github.com/aws/amazon-vpc-cni-k8s/issues/1223
  echo "Waiting for 240 seconds to avoid ENI leakage..."
  sleep 240

  # Exit with the test exit code
  return 0
}

function build_and_push_image() {
  command=$1
  args=$2
  START=$SECONDS
  # Refer to https://github.com/docker/buildx#building-multi-platform-images for the multi-arch image build process.
  # create the buildx container only if it doesn't exist already.
  docker buildx inspect "$BUILDX_BUILDER" >/dev/null 2<&1 || docker buildx create --name="$BUILDX_BUILDER" --buildkitd-flags '--allow-insecure-entitlement network.host' --use >/dev/null
  make $command $args
  echo "TIMELINE: Docker build took $(($SECONDS - $START)) seconds."
}
