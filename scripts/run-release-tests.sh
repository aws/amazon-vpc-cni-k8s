export RUN_BOTTLEROCKET_TEST=true
chmod +x ./scripts/run-integration-tests.sh
echo "Running bottlerocket test"
./scripts/run-integration-tests.sh
unset RUN_BOTTLEROCKET_TEST

export RUN_WARM_IP_TEST=true
echo "Running warm ip test"
./scripts/run-integration-tests.sh
unset RUN_WARM_IP_TEST

export RUN_WARM_ENI_TEST=true
echo "Running warm eni test"
./scripts/run-integration-tests.sh
unset RUN_WARM_ENI_TEST

export RUN_CONFORMANCE=false

export RUN_KOPS_TEST=true
echo "Running KOPS test"
./scripts/run-integration-tests.sh
unset RUN_KOPS_TEST
