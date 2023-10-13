#!/bin/bash

# After running this script, v1alpha1/zz_generated.deepcopy.go will be modified, please revert the change.
# The next steps would be to copy the new version of the crd from the generated file to customresourcedefinition.yaml

if [[ $(which controller-gen) == "" ]]; then
  echo "Installing controller-gen..."
  go install sigs.k8s.io/controller-tools/cmd/controller-gen@v0.12.1
fi

controller-gen object crd paths="./..." output:crd:artifacts:config=charts/aws-vpc-cni/crds