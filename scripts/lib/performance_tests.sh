function check_for_timeout() {
    if [[ $((SECONDS - $1)) -gt 10000 ]]; then
        RUNNING_PERFORMANCE=false
        on_error
    fi
}

function run_performance_test_130_pods() {
    echo "Running performance tests against cluster"
    RUNNING_PERFORMANCE=true

    DEPLOY_START=$SECONDS

    SCALE_UP_DURATION_ARRAY=()
    SCALE_DOWN_DURATION_ARRAY=()
    while [ ${#SCALE_UP_DURATION_ARRAY[@]} -lt 3 ]
    do
        ITERATION_START=$SECONDS
        $KUBECTL_PATH scale -f ./testdata/deploy-130-pods.yaml --replicas=130
        sleep 20
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

    now="pod-130-Test#${TEST_ID}-$(date +"%m-%d-%Y-%T").csv"
    echo $now

    echo $(date +"%m-%d-%Y-%T") >> $now
    echo $((SCALE_UP_DURATION_ARRAY[0])), $((SCALE_DOWN_DURATION_ARRAY[0])) >> $now
    echo $((SCALE_UP_DURATION_ARRAY[1])), $((SCALE_DOWN_DURATION_ARRAY[1])) >> $now
    echo $((SCALE_UP_DURATION_ARRAY[2])), $((SCALE_DOWN_DURATION_ARRAY[2])) >> $now

    cat $now
    aws s3 cp $now s3://cni-performance-test-data
    
    echo "TIMELINE: 130 Pod performance test took $DEPLOY_DURATION seconds."
    RUNNING_PERFORMANCE=false
}

function run_performance_test_730_pods() {
    echo "Running performance tests against cluster"
    RUNNING_PERFORMANCE=true

    DEPLOY_START=$SECONDS

    SCALE_UP_DURATION_ARRAY=()
    SCALE_DOWN_DURATION_ARRAY=()
    while [ ${#SCALE_UP_DURATION_ARRAY[@]} -lt 3 ]
    do
        ITERATION_START=$SECONDS
        $KUBECTL_PATH scale -f ./testdata/deploy-730-pods.yaml --replicas=730
        sleep 100
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
        sleep 100
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

    now="pod-730-Test#${TEST_ID}-$(date +"%m-%d-%Y-%T").csv"
    echo $now

    echo $(date +"%m-%d-%Y-%T") >> $now
    echo $((SCALE_UP_DURATION_ARRAY[0])), $((SCALE_DOWN_DURATION_ARRAY[0])) >> $now
    echo $((SCALE_UP_DURATION_ARRAY[1])), $((SCALE_DOWN_DURATION_ARRAY[1])) >> $now
    echo $((SCALE_UP_DURATION_ARRAY[2])), $((SCALE_DOWN_DURATION_ARRAY[2])) >> $now

    cat $now
    aws s3 cp $now s3://cni-performance-test-data
    
    echo "TIMELINE: 730 Pod performance test took $DEPLOY_DURATION seconds."
    RUNNING_PERFORMANCE=false
}

function run_performance_test_5000_pods() {
    echo "Running performance tests against cluster"
    RUNNING_PERFORMANCE=true
    
    DEPLOY_START=$SECONDS

    SCALE_UP_DURATION_ARRAY=()
    SCALE_DOWN_DURATION_ARRAY=()
    while [ ${#SCALE_UP_DURATION_ARRAY[@]} -lt 3 ]
    do
        ITERATION_START=$SECONDS
        $KUBECTL_PATH scale -f ./testdata/deploy-5000-pods.yaml --replicas=5000
        sleep 100
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
        sleep 100
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

    now="pod-5000-Test#${TEST_ID}-$(date +"%m-%d-%Y-%T").csv"
    echo $now

    echo $(date +"%m-%d-%Y-%T") >> $now
    echo $((SCALE_UP_DURATION_ARRAY[0])), $((SCALE_DOWN_DURATION_ARRAY[0])) >> $now
    echo $((SCALE_UP_DURATION_ARRAY[1])), $((SCALE_DOWN_DURATION_ARRAY[1])) >> $now
    echo $((SCALE_UP_DURATION_ARRAY[2])), $((SCALE_DOWN_DURATION_ARRAY[2])) >> $now

    cat $now
    aws s3 cp $now s3://cni-performance-test-data
    
    echo "TIMELINE: 5000 Pod performance test took $DEPLOY_DURATION seconds."
    RUNNING_PERFORMANCE=false
}
