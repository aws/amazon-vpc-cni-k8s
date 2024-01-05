function check_for_timeout() {
    if [[ $((SECONDS - $1)) -gt 1500 ]]; then
        FAILURE_COUNT=$((FAILURE_COUNT + 1))
        HAS_FAILED=true
        if [[ $FAILURE_COUNT -gt 1 ]]; then
            RUNNING_PERFORMANCE=false
            echo "Failed twice, deprovisioning cluster"
            on_error 1 $LINENO
        fi
        echo "Failed once, retrying"
    fi
}

function upload_results_to_s3_bucket() {
    echo "$filename"
    echo "Iteration", "scaleUpTime (s)", "scaleDownTime (s)", "scaleUpMem (Mi)", "scaleDownMem (Mi)" >> "$filename"
    echo 1, $((SCALE_UP_DURATION_ARRAY[0])), $((SCALE_DOWN_DURATION_ARRAY[0])), $((SCALE_UP_MEM_ARRAY[0])), $((SCALE_DOWN_MEM_ARRAY[0])) >> "$filename"
    echo 2, $((SCALE_UP_DURATION_ARRAY[1])), $((SCALE_DOWN_DURATION_ARRAY[1])), $((SCALE_UP_MEM_ARRAY[1])), $((SCALE_DOWN_MEM_ARRAY[1])) >> "$filename"
    echo 3, $((SCALE_UP_DURATION_ARRAY[2])), $((SCALE_DOWN_DURATION_ARRAY[2])), $((SCALE_UP_MEM_ARRAY[2])), $((SCALE_DOWN_MEM_ARRAY[2])) >> "$filename"

    cat "$filename"
    if [[ ${#PERFORMANCE_TEST_S3_BUCKET_NAME} -gt 0 ]]; then
        aws s3 cp "$filename" "s3://${PERFORMANCE_TEST_S3_BUCKET_NAME}/${1}/"
    else
        echo "No S3 bucket name given, skipping test result upload."
    fi
}

# This function loads the last 3 scale-up averages and averages them. If the current
# scale-up time is 25% higher than the calculated value, the test is considered a failure.
# For memory checks, if the memory usage exceeds 250Mi, the test is considered a failure.
function check_for_slow_performance() {
    BUCKET="s3://${PERFORMANCE_TEST_S3_BUCKET_NAME}/${1}/"
    FILE1=$(aws s3 ls "${BUCKET}" | sort | tail -n 2 | sed -n '1 p' | awk '{print $4}')
    FILE2=$(aws s3 ls "${BUCKET}" | sort | tail -n 3 | sed -n '1 p' | awk '{print $4}')
    FILE3=$(aws s3 ls "${BUCKET}" | sort | tail -n 4 | sed -n '1 p' | awk '{print $4}')
    
    PAST_PERFORMANCE_UP_AVERAGE_SUM=0
    find_performance_duration_average $FILE1 1
    find_performance_duration_average $FILE2 2
    find_performance_duration_average $FILE3 3
    PAST_PERFORMANCE_UP_AVERAGE=$((PAST_PERFORMANCE_UP_AVERAGE_SUM / 3))

    # Divided by 3 to get current average, multiply past averages by 5/4 to get 25% window
    # Checks if current average is greater than or equal to 15 seconds to avoid failing due to fast previous runs
    if [[$((CURRENT_DURATION_UP_SUM / 3)) -ge 15] && [ $((CURRENT_DURATION_UP_SUM / 3)) -gt $((PAST_PERFORMANCE_UP_AVERAGE * 5 / 4)) ]]; then
        echo "FAILURE! Performance test pod UPPING took >25% longer than the past three tests"
        echo "This tests time: $((CURRENT_DURATION_UP_SUM / 3))"
        echo "Previous tests' time: ${PAST_PERFORMANCE_UP_AVERAGE}"
        echo "********************************"
        echo "Look into how current changes could cause cni inefficiency."
        echo "********************************"
        on_error 1 $LINENO
    fi
}

function get_aws_node_mem_max() {
    AWS_NODE_MEM_RESULT=$(kubectl top pods -n kube-system -l k8s-app=aws-node | awk '{if (NR!=1) {print $NF}}' | sort -n | tail -n1)
    AWS_NODE_MEM_RESULT=${AWS_NODE_MEM_RESULT:0:-2}
}

function find_performance_duration_average() {
    aws s3 cp ${BUCKET}${1} performance_test${2}.csv
    SCALE_UP_TEMP_DURATION_SUM=0
    for i in {2..4}
    do
        TEMP=$(sed -n "${i} p" performance_test${2}.csv)
        PAIR=${TEMP#*,}
        SCALE_UP_TEMP_DURATION_SUM=$((SCALE_UP_TEMP_DURATION_SUM + ${PAIR%%,*}))
    done
    PAST_PERFORMANCE_UP_AVERAGE_SUM=$(($PAST_PERFORMANCE_UP_AVERAGE_SUM + $((SCALE_UP_TEMP_DURATION_SUM / 3))))
}

# This test case captures the following metrics:
# 1. time (in seconds) to scale from 0 to REPLICAS pods - collected 3 times and set in SCALE_UP_DURATION_DELAY
# 2. time (in seconds) to scale from REPLICAS to 0 pods - collected 3 times and set in SCALE_DOWN_DURATION_DELAY
# 3. average time (in seconds) to scale from 0 to REPLICAS pods - collected in (CURRENT_DURATION_UP_SUM / 3)
# 4. average time (in seconds) to scale from REPLICAS to 0 pods - collected in (CURRENT_DURATION_DOWN_SUM / 3)
# 5. memory (in Mebibytes) after scaling up to REPLICAS pods - collected 3 times and set in SCALE_UP_MEM_ARRAY
# 6. memory (in Mebibytes) after scaling down to 0 pods - collected 3 times and set in SCALE_DOWN_MEM_ARRAY
function run_performance_test() {
    RUNNING_PERFORMANCE=true
    REPLICAS=$1
    echo "Running performance tests against cluster with $REPLICAS replicas"
    $KUBECTL_PATH apply -f ./testdata/deploy-${REPLICAS}-pods.yaml

    DEPLOY_START=$SECONDS
    FAILURE_COUNT=0

    SCALE_UP_DURATION_ARRAY=()
    SCALE_UP_MEM_ARRAY=()
    SCALE_DOWN_DURATION_ARRAY=()
    SCALE_DOWN_MEM_ARRAY=()
    CURRENT_DURATION_UP_SUM=0
    CURRENT_DURATION_DOWN_SUM=0
    AWS_NODE_MEM_RESULT=0
    CURRENT_MEM_MAX=0
    while [ ${#SCALE_DOWN_DURATION_ARRAY[@]} -lt 3 ]
    do
        ITERATION_START=$SECONDS
        HAS_FAILED=false
        $KUBECTL_PATH scale -f ./testdata/deploy-${REPLICAS}-pods.yaml --replicas=${REPLICAS}
        while [[ ! $($KUBECTL_PATH get deploy deploy-${REPLICAS}-pods | grep ${REPLICAS}/${REPLICAS}) && "$HAS_FAILED" == false ]]
        do
            sleep 1
            echo "Scaling UP"
            echo $($KUBECTL_PATH get deploy)
            check_for_timeout $ITERATION_START
        done

        if [[ "$HAS_FAILED" == false ]]; then
            get_aws_node_mem_max
            DURATION=$((SECONDS - ITERATION_START))
            SCALE_UP_DURATION_ARRAY+=( $DURATION )
            SCALE_UP_MEM_ARRAY+=( $AWS_NODE_MEM_RESULT )
            CURRENT_DURATION_UP_SUM=$((CURRENT_DURATION_UP_SUM + DURATION))
            if [[ $AWS_NODE_MEM_RESULT -gt $CURRENT_MEM_MAX ]]; then
                CURRENT_MEM_MAX=$AWS_NODE_MEM_RESULT
            fi
        fi
        $KUBECTL_PATH scale -f ./testdata/deploy-${REPLICAS}-pods.yaml --replicas=0
        while [[ $($KUBECTL_PATH get pods) ]]
        do
            sleep 1
            echo "Scaling DOWN"
            echo $($KUBECTL_PATH get deploy)
        done
        if [[ "$HAS_FAILED" == false ]]; then
            get_aws_node_mem_max
            DURATION=$((SECONDS - ITERATION_START))
            SCALE_DOWN_DURATION_ARRAY+=( $DURATION )
            SCALE_DOWN_MEM_ARRAY+=( $AWS_NODE_MEM_RESULT )
            CURRENT_DURATION_DOWN_SUM=$((CURRENT_DURATION_DOWN_SUM + DURATION))
            if [[ $AWS_NODE_MEM_RESULT -gt $CURRENT_MEM_MAX ]]; then
                CURRENT_MEM_MAX=$AWS_NODE_MEM_RESULT
            fi
        fi
    done

    DEPLOY_DURATION=$((SECONDS - DEPLOY_START))
    filename="pod-${REPLICAS}-Test-${TEST_ID}-$(date +"%m-%d-%Y-%T")-${TEST_IMAGE_VERSION}.csv"
    upload_results_to_s3_bucket "${REPLICAS}-pods"

    echo "TIMELINE: ${REPLICAS} Pod performance test took $DEPLOY_DURATION seconds."
    RUNNING_PERFORMANCE=false
    if [[ ${#PERFORMANCE_TEST_S3_BUCKET_NAME} -gt 0 ]]; then
        check_for_slow_performance "${REPLICAS}-pods"
    fi
    # Check for memory breach
    if [[ $CURRENT_MEM_MAX -gt 200 ]]; then
        echo "Max aws-node pod memory usage exceeded 200Mi. Failing test."
        on_error 1 $LINENO
    fi
    $KUBECTL_PATH delete -f ./testdata/deploy-${REPLICAS}-pods.yaml
}

# This installs the CW agent to push all metrics to Cloudwatch logs. It collects Memory, CPU, Network metrics
# https://docs.aws.amazon.com/AmazonCloudWatch/latest/monitoring/Container-Insights-metrics-EKS.html
function install_cw_agent(){

    CW_NAMESPACE="amazon-cloudwatch"
    CW_POLICY_ARN="arn:aws:iam::aws:policy/CloudWatchAgentServerPolicy"

    echo "Create IAM Service Account for CW agent"
    $KUBECTL_PATH create ns $CW_NAMESPACE
    eksctl create iamserviceaccount \
        --cluster $CLUSTER_NAME \
        --name cloudwatch-agent \
        --namespace $CW_NAMESPACE \
        --attach-policy-arn $CW_POLICY_ARN \
        --approve

    echo "Install Cloudwatch Agent DS"
    $KUBECTL_PATH apply -f https://raw.githubusercontent.com/aws-samples/amazon-cloudwatch-container-insights/latest/k8s-deployment-manifest-templates/deployment-mode/daemonset/container-insights-monitoring/cwagent/cwagent-serviceaccount.yaml

    echo '{ "logs": { "metrics_collected": { "kubernetes": { "metrics_collection_interval": 30, "cluster_name": "eks-net-perf" }},"force_flush_interval": 5 }}' | jq '.' > cwagentconfig.json
    $KUBECTL_PATH create cm -n $CW_NAMESPACE cwagentconfig --from-file cwagentconfig.json
    $KUBECTL_PATH apply -f https://raw.githubusercontent.com/aws-samples/amazon-cloudwatch-container-insights/latest/k8s-deployment-manifest-templates/deployment-mode/daemonset/container-insights-monitoring/cwagent/cwagent-daemonset.yaml
    
    # Allow CW agent to startup and push initial logs
    sleep 60s
}

function uninstall_cw_agent(){

    CW_NAMESPACE="amazon-cloudwatch"

    eksctl delete iamserviceaccount \
        --cluster $CLUSTER_NAME \
        --name cloudwatch-agent \
        --namespace $CW_NAMESPACE

    $KUBECTL_PATH delete namespace $CW_NAMESPACE
}