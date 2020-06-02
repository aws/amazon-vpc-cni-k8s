: "${DEPROVISION:=true}"

START=$SECONDS
wget -qO- https://dl.k8s.io/v$K8S_VERSION/kubernetes-test.tar.gz | tar -zxvf - --strip-components=4 -C /tmp  kubernetes/platforms/linux/amd64/e2e.test
  /tmp/e2e.test --ginkgo.focus="\[Serial\].*Conformance" --kubeconfig=$KUBECONFIG --ginkgo.failFast --ginkgo.flakeAttempts 2 \
    --ginkgo.skip="(should support remote command execution over websockets)|(should support retrieving logs from the container over websockets)|\[Slow\]"

SERIAL_CONFORMANCE_DURATION=$((SECONDS - START))
echo "TIMELINE: Conformance tests took $SERIAL_CONFORMANCE_DURATION seconds."

if [[ "$DEPROVISION" == true ]]; then
    START=$SECONDS

    down-test-cluster

    DOWN_DURATION=$((SECONDS - START))
    echo "TIMELINE: Down processes took $DOWN_DURATION seconds."
fi

if [[ $TEST_PASS -ne 0 ]]; then
    exit 1
fi