#!/usr/bin/env bash

# Deletes the workload left running by run-cni-scale-tests.sh so the caller can
# capture full-load metrics before cleanup and recovery metrics afterwards.

set -euo pipefail

SCALE_TEST_NAMESPACE_PREFIX=${SCALE_TEST_NAMESPACE_PREFIX:-cni-scale}
SCALE_TEST_TIMEOUT_SECONDS=${SCALE_TEST_TIMEOUT_SECONDS:-1800}
namespace="${SCALE_TEST_NAMESPACE_PREFIX}-1"

if [[ -n ${KUBE_CONFIG_PATH:-} && -z ${KUBECONFIG:-} ]]; then
  export KUBECONFIG=$KUBE_CONFIG_PATH
fi
KUBECONFIG=${KUBECONFIG:-"${HOME:?HOME must be set}/.kube/config"}
export KUBECONFIG

if [[ ! $SCALE_TEST_TIMEOUT_SECONDS =~ ^[1-9][0-9]*$ ]]; then
  printf 'SCALE_TEST_TIMEOUT_SECONDS must be a positive integer; got %q\n' \
    "$SCALE_TEST_TIMEOUT_SECONDS" >&2
  exit 2
fi
command -v kubectl >/dev/null 2>&1 || {
  printf 'kubectl is required\n' >&2
  exit 127
}

printf 'Deleting CNI scale workload namespace %s\n' "$namespace"
kubectl --kubeconfig "$KUBECONFIG" delete namespace "$namespace" \
  --ignore-not-found=true \
  --wait=true \
  --timeout="${SCALE_TEST_TIMEOUT_SECONDS}s"

if kubectl --kubeconfig "$KUBECONFIG" get namespace "$namespace" >/dev/null 2>&1; then
  printf 'Namespace %s still exists after cleanup\n' "$namespace" >&2
  exit 1
fi

printf 'CNI scale workload cleanup completed\n'
