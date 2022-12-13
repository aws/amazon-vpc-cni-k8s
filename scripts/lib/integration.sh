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

    /tmp/e2e.test --ginkgo.focus="Conformance" --kubeconfig=$KUBECONFIG --ginkgo.failFast --ginkgo.flakeAttempts 2 \
	    --ginkgo.skip="(ServiceAccountIssuerDiscovery should support OIDC discovery of service account issuer)|(should support remote command execution over websockets)|(should support retrieving logs from the container over websockets)|(Basic StatefulSet functionality [StatefulSetBasic])|\[Slow\]|\[Serial\]"

    /tmp/e2e.test --ginkgo.focus="\[Serial\].*Conformance" --kubeconfig=$KUBECONFIG --ginkgo.failFast --ginkgo.flakeAttempts 2 \
	    --ginkgo.skip="(ServiceAccountIssuerDiscovery should support OIDC discovery of service account issuer)|(should support remote command execution over websockets)|(should support retrieving logs from the container over websockets)|\[Slow\]"
    echo "Kops conformance tests ran successfully!"

    KOPS_TEST_DURATION=$((SECONDS - START))
    echo "TIMELINE: KOPS tests took $KOPS_TEST_DURATION seconds."

    sleep 240 #Workaround to avoid ENI leakage during cluster deletion: https://github.com/aws/amazon-vpc-cni-k8s/issues/1223
    START=$SECONDS
    down-kops-cluster
    emit_cloudwatch_metric "kops_test_status" "1"
    DOWN_KOPS_DURATION=$((SECONDS - START))
    echo "TIMELINE: Down KOPS cluster took $DOWN_KOPS_DURATION seconds."
    exit 0
}

function run_calico_test() {
  echo "Starting Helm installing Tigera operator and running Calico STAR tests"
  pushd ./test
  VPC_ID=$(eksctl get cluster $CLUSTER_NAME -oyaml | grep vpc | cut -d ":" -f 2 | awk '{$1=$1};1')

  calico_version=$CALICO_VERSION
  if [[ $1 == "true" ]]; then
    # we can automatically use latest version in Calico repo, or use the known highest version (currently v3.23.0)
    if [[ $RUN_LATEST_CALICO_VERSION == true ]]; then
      version_tag=$(curl -i https://api.github.com/repos/projectcalico/calico/releases/latest | grep "tag_name") || true
      if [[ -n $version_tag ]]; then
        calico_version=$(echo $version_tag | cut -d ":" -f 2 | cut -d '"' -f 2 )
      else
        echo "Getting Calico latest version failed, will fall back to default/set version $calico_version instead"
      fi
    else echo "Using default Calico version"
    fi
    echo "Using Calico version $calico_version to test"
  else
    version=$(kubectl describe ds -n calico-system calico-node | grep "calico/node:" | cut -d ':' -f3)
    echo "Calico has been installed in testing cluster, keep using the version $version"
  fi

  echo "Testing amd64"
  instance_type="amd64"
  ginkgo -v e2e/calico -- --cluster-kubeconfig=$KUBECONFIG --cluster-name=$CLUSTER_NAME --aws-region=$AWS_DEFAULT_REGION --aws-vpc-id=$VPC_ID --calico-version=$calico_version --instance-type=$instance_type --install-calico=$1
  echo "Testing arm64"
  instance_type="arm64"
  ginkgo -v e2e/calico -- --cluster-kubeconfig=$KUBECONFIG --cluster-name=$CLUSTER_NAME --aws-region=$AWS_DEFAULT_REGION --aws-vpc-id=$VPC_ID --calico-version=$calico_version --instance-type=$instance_type --install-calico=false
  popd
}

function check_and_build_image(){
  repository_name=$1 
  image_name=$2
  image_tag=$3
  command=$4
  CNI_IMAGES_BUILD=false
  ecr_image_query_result=$(aws ecr batch-get-image --repository-name=$repository_name --image-ids imageTag=$image_tag --query 'images[].imageId.imageTag' --region us-west-2)
  if [[ $ecr_image_query_result != "[]" ]]; then
    echo "CNI image $image_name:$image_tag already exists in repository. Skipping image build..."
  else
    echo "CNI image $image_name:$image_tag does not exist in repository."
    build_and_push_image "$command" "$image_name" "$image_tag"
    CNI_IMAGES_BUILD=true
  fi
  # cleanup if we make docker build and push images
  if [[ "$CNI_IMAGES_BUILD" == true ]]; then
    docker buildx rm "$BUILDX_BUILDER"
    if [[ $TEST_IMAGE_VERSION != "$LOCAL_GIT_VERSION" ]]; then
      popd
    fi
  fi
}

function build_and_push_image(){
  command=$1
  image_name=$2
  image_tag=$3
  START=$SECONDS
  # Refer to https://github.com/docker/buildx#building-multi-platform-images for the multi-arch image build process.
  # create the buildx container only if it doesn't exist already.
  docker buildx inspect "$BUILDX_BUILDER" >/dev/null 2<&1 || docker buildx create --name="$BUILDX_BUILDER" --buildkitd-flags '--allow-insecure-entitlement network.host' --use >/dev/null
  make $command IMAGE="$image_name" VERSION="$image_tag"
  echo "TIMELINE: Docker build took $(($SECONDS - $START)) seconds."
}