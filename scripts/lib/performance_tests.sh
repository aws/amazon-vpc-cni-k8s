function check_for_timeout() {
    if [[ $((SECONDS - $1)) -gt 1500 ]]; then
        FAILURE_COUNT=$((FAILURE_COUNT + 1))
        HAS_FAILED=true
        if [[ $FAILURE_COUNT -gt 1 ]]; then
            RUNNING_PERFORMANCE=false
            echo "Failed twice, deprovisioning cluster"
            on_error
        fi
        echo "Failed once, retrying"
    fi
}

function upload_results_to_s3_bucket() {
    echo "$filename"
    echo "Date", "\"slot1\"", "\"slot2\"" >> "$filename"
    echo $(date +"%Y-%m-%d-%T"), $((SCALE_UP_DURATION_ARRAY[0])), $((SCALE_DOWN_DURATION_ARRAY[0])) >> "$filename"
    echo $(date +"%Y-%m-%d-%T"), $((SCALE_UP_DURATION_ARRAY[1])), $((SCALE_DOWN_DURATION_ARRAY[1])) >> "$filename"
    echo $(date +"%Y-%m-%d-%T"), $((SCALE_UP_DURATION_ARRAY[2])), $((SCALE_DOWN_DURATION_ARRAY[2])) >> "$filename"

    cat "$filename"
    if [[ ${#PERFORMANCE_TEST_S3_BUCKET_NAME} -gt 0 ]]; then
        aws s3 cp "$filename" "s3://${PERFORMANCE_TEST_S3_BUCKET_NAME}/${1}/"
    else
        echo "No S3 bucket name given, skipping test result upload."
    fi
}

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
    if [[ $((CURRENT_PERFORMANCE_UP_SUM / 3)) -gt $((PAST_PERFORMANCE_UP_AVERAGE * 5 / 4)) ]]; then
        echo "FAILURE! Performance test pod UPPING took >25% longer than the past three tests"
        echo "This tests time: $((CURRENT_PERFORMANCE_UP_SUM / 3))"
        echo "Previous tests' time: ${PAST_PERFORMANCE_UP_AVERAGE}"
        echo "********************************"
        echo "Look into how current changes could cause cni inefficiency."
        echo "********************************"
        on_error
    fi
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

function run_performance_test_130_pods() {
    echo "Running performance tests against cluster"
    RUNNING_PERFORMANCE=true
    $KUBECTL_PATH apply -f ./testdata/deploy-130-pods.yaml

    DEPLOY_START=$SECONDS
    FAILURE_COUNT=0

    SCALE_UP_DURATION_ARRAY=()
    SCALE_DOWN_DURATION_ARRAY=()
    CURRENT_PERFORMANCE_UP_SUM=0
    CURRENT_PERFORMANCE_DOWN_SUM=0
    while [ ${#SCALE_DOWN_DURATION_ARRAY[@]} -lt 3 ]
    do
        ITERATION_START=$SECONDS
        HAS_FAILED=false
        $KUBECTL_PATH scale -f ./testdata/deploy-130-pods.yaml --replicas=130
        while [[ ! $($KUBECTL_PATH get deploy | grep 130/130) && "$HAS_FAILED" == false ]]
        do
            sleep 1
            echo "Scaling UP"
            echo $($KUBECTL_PATH get deploy)
            check_for_timeout $ITERATION_START
        done

        if [[ "$HAS_FAILED" == false ]]; then
            DURATION=$((SECONDS - ITERATION_START))
            SCALE_UP_DURATION_ARRAY+=( $DURATION )
            CURRENT_PERFORMANCE_UP_SUM=$((CURRENT_PERFORMANCE_UP_SUM + DURATION))
        fi
        $KUBECTL_PATH scale -f ./testdata/deploy-130-pods.yaml --replicas=0
        while [[ $($KUBECTL_PATH get pods) ]]
        do
            sleep 1
            echo "Scaling DOWN"
            echo $($KUBECTL_PATH get deploy)
        done
        if [[ "$HAS_FAILED" == false ]]; then
            DURATION=$((SECONDS - ITERATION_START))
            SCALE_DOWN_DURATION_ARRAY+=( $DURATION )
            CURRENT_PERFORMANCE_DOWN_SUM=$((CURRENT_PERFORMANCE_DOWN_SUM + DURATION))
        fi
    done

    echo "Times to scale up:"
    INDEX=0
    while [ $INDEX -lt ${#SCALE_UP_DURATION_ARRAY[@]} ]
    do
        echo ${SCALE_UP_DURATION_ARRAY[$INDEX]}
        INDEX=$((INDEX + 1))
    done
    echo ""
    echo "Times to scale down:"
    INDEX=0
    while [ $INDEX -lt ${#SCALE_DOWN_DURATION_ARRAY[@]} ]
    do
        echo "${SCALE_DOWN_DURATION_ARRAY[$INDEX]} seconds"
        INDEX=$((INDEX + 1))
    done
    echo ""
    DEPLOY_DURATION=$((SECONDS - DEPLOY_START))

    filename="pod-130-Test-${TEST_ID}-$(date +"%m-%d-%Y-%T")-${TEST_IMAGE_VERSION}.csv"
    upload_results_to_s3_bucket "130-pods"
    
    echo "TIMELINE: 130 Pod performance test took $DEPLOY_DURATION seconds."
    RUNNING_PERFORMANCE=false
    if [[ ${#PERFORMANCE_TEST_S3_BUCKET_NAME} -gt 0 ]]; then
        check_for_slow_performance "130-pods"
    fi
    $KUBECTL_PATH delete -f ./testdata/deploy-130-pods.yaml
}

function run_performance_test_730_pods() {
    echo "Running performance tests against cluster"
    RUNNING_PERFORMANCE=true
    $KUBECTL_PATH apply -f ./testdata/deploy-730-pods.yaml

    DEPLOY_START=$SECONDS
    FAILURE_COUNT=0

    SCALE_UP_DURATION_ARRAY=()
    SCALE_DOWN_DURATION_ARRAY=()
    CURRENT_PERFORMANCE_UP_SUM=0
    CURRENT_PERFORMANCE_DOWN_SUM=0
    while [ ${#SCALE_DOWN_DURATION_ARRAY[@]} -lt 3 ]
    do
        ITERATION_START=$SECONDS
        HAS_FAILED=false
        $KUBECTL_PATH scale -f ./testdata/deploy-730-pods.yaml --replicas=730
        while [[ ! $($KUBECTL_PATH get deploy | grep 730/730) && "$HAS_FAILED" == false ]]
        do
            sleep 2
            echo "Scaling UP"
            echo $($KUBECTL_PATH get deploy)
            check_for_timeout $ITERATION_START
        done

        if [[ "$HAS_FAILED" == false ]]; then
            DURATION=$((SECONDS - ITERATION_START))
            SCALE_UP_DURATION_ARRAY+=( $DURATION )
            CURRENT_PERFORMANCE_UP_SUM=$((CURRENT_PERFORMANCE_UP_SUM + DURATION))
        fi
        $KUBECTL_PATH scale -f ./testdata/deploy-730-pods.yaml --replicas=0
        while [[ $($KUBECTL_PATH get pods) ]]
        do
            sleep 2
            echo "Scaling DOWN"
            echo $($KUBECTL_PATH get deploy)
        done
        if [[ "$HAS_FAILED" == false ]]; then
            DURATION=$((SECONDS - ITERATION_START))
            SCALE_DOWN_DURATION_ARRAY+=( $DURATION )
            CURRENT_PERFORMANCE_DOWN_SUM=$((CURRENT_PERFORMANCE_DOWN_SUM + DURATION))
        fi
    done

    echo "Times to scale up:"
    INDEX=0
    while [ $INDEX -lt ${#SCALE_UP_DURATION_ARRAY[@]} ]
    do
        echo ${SCALE_UP_DURATION_ARRAY[$INDEX]}
        INDEX=$((INDEX + 1))
    done
    echo ""
    echo "Times to scale down:"
    INDEX=0
    while [ $INDEX -lt ${#SCALE_DOWN_DURATION_ARRAY[@]} ]
    do
        echo "${SCALE_DOWN_DURATION_ARRAY[$INDEX]} seconds"
        INDEX=$((INDEX + 1))
    done
    echo ""
    DEPLOY_DURATION=$((SECONDS - DEPLOY_START))

    filename="pod-730-Test-${TEST_ID}-$(date +"%m-%d-%Y-%T")-${TEST_IMAGE_VERSION}.csv"
    upload_results_to_s3_bucket "730-pods"
    
    echo "TIMELINE: 730 Pod performance test took $DEPLOY_DURATION seconds."
    RUNNING_PERFORMANCE=false
    if [[ ${#PERFORMANCE_TEST_S3_BUCKET_NAME} -gt 0 ]]; then
        check_for_slow_performance "730-pods"
    fi
    $KUBECTL_PATH delete -f ./testdata/deploy-730-pods.yaml
}

function scale_nodes_for_5000_pod_test() {
    AUTO_SCALE_GROUP_NAME=$(aws eks describe-nodegroup --cluster-name $CLUSTER_NAME --nodegroup-name cni-test-multi-node-mng | jq -r '.nodegroup.resources.autoScalingGroups[].name') 
    echo "$AUTO_SCALE_GROUP_NAME"
    aws autoscaling update-auto-scaling-group \
        --auto-scaling-group-name "$AUTO_SCALE_GROUP_NAME" \
        --desired-capacity 99
}

function run_performance_test_5000_pods() {
    echo "Running performance tests against cluster"
    RUNNING_PERFORMANCE=true
    $KUBECTL_PATH apply -f ./testdata/deploy-5000-pods.yaml
    
    DEPLOY_START=$SECONDS
    FAILURE_COUNT=0

    SCALE_UP_DURATION_ARRAY=()
    SCALE_DOWN_DURATION_ARRAY=()
    CURRENT_PERFORMANCE_UP_SUM=0
    CURRENT_PERFORMANCE_DOWN_SUM=0
    while [ ${#SCALE_DOWN_DURATION_ARRAY[@]} -lt 3 ]
    do
        ITERATION_START=$SECONDS
        HAS_FAILED=false
        $KUBECTL_PATH scale -f ./testdata/deploy-5000-pods.yaml --replicas=5000
        while [[ ! $($KUBECTL_PATH get deploy | grep 5000/5000) && "$HAS_FAILED" == false ]]
        do
            sleep 2
            echo "Scaling UP"
            echo $($KUBECTL_PATH get deploy)
            check_for_timeout $ITERATION_START
        done

        if [[ "$HAS_FAILED" == false ]]; then
            DURATION=$((SECONDS - ITERATION_START))
            SCALE_UP_DURATION_ARRAY+=( $DURATION )
            CURRENT_PERFORMANCE_UP_SUM=$((CURRENT_PERFORMANCE_UP_SUM + DURATION))
        fi
        $KUBECTL_PATH scale -f ./testdata/deploy-5000-pods.yaml --replicas=0
        while [[ $($KUBECTL_PATH get pods) ]]
        do
            sleep 2
            echo "Scaling DOWN"
            echo $($KUBECTL_PATH get deploy)
        done
        if [[ "$HAS_FAILED" == false ]]; then
            DURATION=$((SECONDS - ITERATION_START))
            SCALE_DOWN_DURATION_ARRAY+=( $DURATION )
            CURRENT_PERFORMANCE_DOWN_SUM=$((CURRENT_PERFORMANCE_DOWN_SUM + DURATION))
        fi
    done

    echo "Times to scale up:"
    INDEX=0
    while [ $INDEX -lt ${#SCALE_UP_DURATION_ARRAY[@]} ]
    do
        echo ${SCALE_UP_DURATION_ARRAY[$INDEX]}
        INDEX=$((INDEX + 1))
    done
    echo ""
    echo "Times to scale down:"
    INDEX=0
    while [ $INDEX -lt ${#SCALE_DOWN_DURATION_ARRAY[@]} ]
    do
        echo "${SCALE_DOWN_DURATION_ARRAY[$INDEX]} seconds"
        INDEX=$((INDEX + 1))
    done
    echo ""
    DEPLOY_DURATION=$((SECONDS - DEPLOY_START))

    filename="pod-5000-Test-${TEST_ID}-$(date +"%m-%d-%Y-%T")-${TEST_IMAGE_VERSION}.csv"
    upload_results_to_s3_bucket "5000-pods"
    
    echo "TIMELINE: 5000 Pod performance test took $DEPLOY_DURATION seconds."
    RUNNING_PERFORMANCE=false
    if [[ ${#PERFORMANCE_TEST_S3_BUCKET_NAME} -gt 0 ]]; then
        check_for_slow_performance "5000-pods"
    fi
    $KUBECTL_PATH delete -f ./testdata/deploy-5000-pods.yaml
}
