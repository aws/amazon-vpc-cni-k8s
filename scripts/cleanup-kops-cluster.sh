#!/usr/bin/env bash

set -euo pipefail

state_file=${1:-${KOPS_CLEANUP_STATE_FILE:-}}
if [[ -z "$state_file" ]]; then
    echo "Usage: $0 <cleanup-state-file>" >&2
    exit 2
fi

if [[ ! -f "$state_file" ]]; then
    echo "No kOps cleanup state found; cluster is already cleaned up."
    exit 0
fi

cluster_name=''
state_store=''
extra_line=''
{
    IFS= read -r cluster_name || {
        echo "Missing cluster name in $state_file" >&2
        exit 2
    }
    IFS= read -r state_store || {
        echo "Missing state store in $state_file" >&2
        exit 2
    }
    if IFS= read -r extra_line || [[ -n "$extra_line" ]]; then
        echo "Expected exactly two lines in $state_file" >&2
        exit 2
    fi
} < "$state_file"

if [[ ! "$cluster_name" =~ ^kops-cni-test-cluster-[a-zA-Z0-9-]+\.k8s\.local$ ]]; then
    echo "Invalid kOps cluster name in $state_file" >&2
    exit 2
fi
if [[ ! "$state_store" =~ ^s3://kops-cni-test-eks-[0-9]{12}$ ]]; then
    echo "Invalid kOps state store in $state_file" >&2
    exit 2
fi
KOPS_BIN=${KOPS_BIN:-$HOME/kops_bin/kops}
if [[ ! -x "$KOPS_BIN" ]]; then
    echo "kOps binary not found at $KOPS_BIN" >&2
    exit 1
fi

echo "Deleting kOps cluster $cluster_name from $state_store"
if command -v timeout >/dev/null 2>&1; then
    KOPS_STATE_STORE="$state_store" timeout --signal=TERM --kill-after=1m "${KOPS_DELETE_TIMEOUT:-15m}" \
        "$KOPS_BIN" delete cluster --name "$cluster_name" --yes
else
    KOPS_STATE_STORE="$state_store" "$KOPS_BIN" delete cluster --name "$cluster_name" --yes
fi
rm -f "$state_file"
