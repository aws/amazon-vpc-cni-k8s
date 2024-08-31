#!/bin/bash

# This script sets the KUBECONFIG environment variable based on KUBE_CONFIG_PATH if not already set.

if [ -n "$KUBE_CONFIG_PATH" ] && [ -z "$KUBECONFIG" ]; then
  export KUBECONFIG=$KUBE_CONFIG_PATH
fi
