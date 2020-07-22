export RUN_BOTTLEROCKET_TEST=true
bash scripts/run-integration-tests.sh
unset RUN_BOTTLEROCKET_TEST

export RUN_WARM_IP_TEST=true
bash scripts/run-integration-tests.sh
unset RUN_WARM_IP_TEST

export RUN_WARM_ENI_TEST=true
bash scripts/run-integration-tests.sh
unset 

export RUN_CONFORMANCE=false
export RUN_PERFORMANCE_TESTS=true
bash scripts/run-integration-tests.sh
unset RUN_PERFORMANCE_TESTS

export RUN_KOPS_TEST=true
bash scripts/run-integration-tests.sh
unset RUN_KOPS_TEST
