function check_for_timeout() {
    if [[ $((SECONDS - $1)) -gt 10000 ]]; then
        RUNNING_PERFORMANCE=false
        on_error
    fi
}

function run_performance_test_130_pods() {
    echo "Running performance tests against cluster"
    RUNNING_PERFORMANCE=true
    $KUBECTL_PATH apply -f ./testdata/deploy-130-pods.yaml

    DEPLOY_START=$SECONDS

    SCALE_UP_DURATION_ARRAY=()
    SCALE_DOWN_DURATION_ARRAY=()
    while [ ${#SCALE_UP_DURATION_ARRAY[@]} -lt 3 ]
    do
        ITERATION_START=$SECONDS
        $KUBECTL_PATH scale -f ./testdata/deploy-130-pods.yaml --replicas=130
        while [[ ! $($KUBECTL_PATH get deploy | grep 130/130) ]]
        do
            sleep 1
            echo "Scaling UP"
            echo $($KUBECTL_PATH get deploy)
            check_for_timeout $DEPLOY_START
        done

        SCALE_UP_DURATION_ARRAY+=( $((SECONDS - ITERATION_START)) )
        MIDPOINT_START=$SECONDS
        $KUBECTL_PATH scale -f ./testdata/deploy-130-pods.yaml --replicas=0
        while [[ $($KUBECTL_PATH get pods) ]]
        do
            sleep 1
            echo "Scaling DOWN"
            echo $($KUBECTL_PATH get deploy)
            check_for_timeout $DEPLOY_START
        done
        SCALE_DOWN_DURATION_ARRAY+=($((SECONDS - MIDPOINT_START)))
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

    filename="pod-130-Test#${TEST_ID}-$(date +"%m-%d-%Y-%T").csv"
    echo $filename

    echo $(date +"%m-%d-%Y-%T") >> $filename
    echo $((SCALE_UP_DURATION_ARRAY[0])), $((SCALE_DOWN_DURATION_ARRAY[0])) >> $filename
    echo $((SCALE_UP_DURATION_ARRAY[1])), $((SCALE_DOWN_DURATION_ARRAY[1])) >> $filename
    echo $((SCALE_UP_DURATION_ARRAY[2])), $((SCALE_DOWN_DURATION_ARRAY[2])) >> $filename

    cat $filename
    if [[ ${#PERFORMANCE_TEST_S3_BUCKET_NAME} -gt 0 ]]; then
        aws s3 cp $filename $PERFORMANCE_TEST_S3_BUCKET_NAME
    else
        echo "No S3 bucket name given, skipping test result upload."
    fi
    
    echo "TIMELINE: 130 Pod performance test took $DEPLOY_DURATION seconds."
    RUNNING_PERFORMANCE=false
    $KUBECTL_PATH delete -f ./testdata/deploy-130-pods.yaml
}

function run_performance_test_730_pods() {
    echo "Running performance tests against cluster"
    RUNNING_PERFORMANCE=true
    $KUBECTL_PATH apply -f ./testdata/deploy-730-pods.yaml

    DEPLOY_START=$SECONDS

    SCALE_UP_DURATION_ARRAY=()
    SCALE_DOWN_DURATION_ARRAY=()
    while [ ${#SCALE_UP_DURATION_ARRAY[@]} -lt 3 ]
    do
        ITERATION_START=$SECONDS
        $KUBECTL_PATH scale -f ./testdata/deploy-730-pods.yaml --replicas=730
        while [[ ! $($KUBECTL_PATH get deploy | grep 730/730) ]]
        do
            sleep 2
            echo "Scaling UP"
            echo $($KUBECTL_PATH get deploy)
            check_for_timeout $DEPLOY_START
        done

        SCALE_UP_DURATION_ARRAY+=( $((SECONDS - ITERATION_START)) )
        MIDPOINT_START=$SECONDS
        $KUBECTL_PATH scale -f ./testdata/deploy-730-pods.yaml --replicas=0
        while [[ $($KUBECTL_PATH get pods) ]]
        do
            sleep 2
            echo "Scaling DOWN"
            echo $($KUBECTL_PATH get deploy)
            check_for_timeout $DEPLOY_START
        done
        SCALE_DOWN_DURATION_ARRAY+=($((SECONDS - MIDPOINT_START)))
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

    filename="pod-730-Test#${TEST_ID}-$(date +"%m-%d-%Y-%T").csv"
    echo $filename

    echo $(date +"%m-%d-%Y-%T") >> $filename
    echo $((SCALE_UP_DURATION_ARRAY[0])), $((SCALE_DOWN_DURATION_ARRAY[0])) >> $filename
    echo $((SCALE_UP_DURATION_ARRAY[1])), $((SCALE_DOWN_DURATION_ARRAY[1])) >> $filename
    echo $((SCALE_UP_DURATION_ARRAY[2])), $((SCALE_DOWN_DURATION_ARRAY[2])) >> $filename

    cat $filename
    if [[ ${#PERFORMANCE_TEST_S3_BUCKET_NAME} -gt 0 ]]; then
        aws s3 cp $filename $PERFORMANCE_TEST_S3_BUCKET_NAME
    else
        echo "No S3 bucket name given, skipping test result upload."
    fi
    
    echo "TIMELINE: 730 Pod performance test took $DEPLOY_DURATION seconds."
    RUNNING_PERFORMANCE=false
    $KUBECTL_PATH delete -f ./testdata/deploy-730-pods.yaml
}

function run_performance_test_5000_pods() {
    echo "Running performance tests against cluster"
    RUNNING_PERFORMANCE=true
    $KUBECTL_PATH apply -f ./testdata/deploy-5000-pods.yaml
    
    DEPLOY_START=$SECONDS

    SCALE_UP_DURATION_ARRAY=()
    SCALE_DOWN_DURATION_ARRAY=()
    while [ ${#SCALE_UP_DURATION_ARRAY[@]} -lt 3 ]
    do
        ITERATION_START=$SECONDS
        $KUBECTL_PATH scale -f ./testdata/deploy-5000-pods.yaml --replicas=5000
        while [[ ! $($KUBECTL_PATH get deploy | grep 5000/5000) ]]
        do
            sleep 2
            echo "Scaling UP"
            echo $($KUBECTL_PATH get deploy)
            check_for_timeout $DEPLOY_START
        done

        SCALE_UP_DURATION_ARRAY+=( $((SECONDS - ITERATION_START)) )
        MIDPOINT_START=$SECONDS
        $KUBECTL_PATH scale -f ./testdata/deploy-5000-pods.yaml --replicas=0
        while [[ $($KUBECTL_PATH get pods) ]]
        do
            sleep 2
            echo "Scaling DOWN"
            echo $($KUBECTL_PATH get deploy)
            check_for_timeout $DEPLOY_START
        done
        SCALE_DOWN_DURATION_ARRAY+=($((SECONDS - MIDPOINT_START)))
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

    filename="pod-5000-Test#${TEST_ID}-$(date +"%m-%d-%Y-%T").csv"
    echo $filename

    echo $(date +"%m-%d-%Y-%T") >> $filename
    echo $((SCALE_UP_DURATION_ARRAY[0])), $((SCALE_DOWN_DURATION_ARRAY[0])) >> $filename
    echo $((SCALE_UP_DURATION_ARRAY[1])), $((SCALE_DOWN_DURATION_ARRAY[1])) >> $filename
    echo $((SCALE_UP_DURATION_ARRAY[2])), $((SCALE_DOWN_DURATION_ARRAY[2])) >> $filename

    cat $filename
    if [[ ${#PERFORMANCE_TEST_S3_BUCKET_NAME} -gt 0 ]]; then
        aws s3 cp $filename $PERFORMANCE_TEST_S3_BUCKET_NAME
    else
        echo "No S3 bucket name given, skipping test result upload."
    fi
    
    echo "TIMELINE: 5000 Pod performance test took $DEPLOY_DURATION seconds."
    RUNNING_PERFORMANCE=false
    $KUBECTL_PATH delete -f ./testdata/deploy-5000-pods.yaml
}
