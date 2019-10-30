#!/usr/bin/env bash

function down-test-cluster() {
    echo "Deleting cluster $CLUSTER_NAME"
    $TESTER_PATH delete cluster --name $CLUSTER_NAME.k8s.local --state $KOPS_STATE_FILE --yes
}