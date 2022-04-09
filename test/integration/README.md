## How to run tests
# All tests
    * set AWS_ACCESS_KEY_ID
    * set AWS_SECRET_ACCESS_KEY
    * set AWS_DEFAULT_REGION (optional, defaults to us-west-2 if not set)
    * approve test after build completes
    * Can only run one of the following tests at a time, as most need a unique cluster to work on

# Performance
    * run from cni test account to upload test results
        * set PERFORMANCE_TEST_S3_BUCKET_NAME to the name of the bucket (likely `cni-performance-tests`)
    * set RUN_PERFORMANCE_TESTS=true
    * to view data graph:
        * Go to Isengard and open aws-wesley+vpc-cni-ci-test@amazon.com as admin
        * Go to QuickSight and signup with your email email (it does not need an additional password)
        * Open dashboards:
            * 130-pods test - https://us-west-2.quicksight.aws.amazon.com/sn/dashboards/af137b24-a4c1-4ecd-addb-2056486e2022/views/4facfa4f-4b29-42d7-bdf5-5335d9114533
	    * 5000-pods test - https://us-west-2.quicksight.aws.amazon.com/sn/dashboards/55b56360-dbc3-4fc4-917a-167249a0eb8c/views/1cb4c112-cea3-4ea2-84f6-32d0436b0711
            * 730-pods test - https://us-west-2.quicksight.aws.amazon.com/sn/dashboards/8e10011a-a29f-4218-a62d-691fd41c71f3/views/f78feb6c-f45b-4788-82c4-0fc348e793d0

    * NOTE: if running on previous versions, change the date inside of the file to the date of release so as to not confuse graphing order

# KOPS
    * set RUN_KOPS_TEST=true
    * WARNING: will occassionally fail/flake tests, try re-running test a couple times to ensure there is a 
    
# Bottlerocket
    * set RUN_BOTTLEROCKET_TEST=true

# Calico
    * set RUN_CALICO_TEST=true

## Conformance test duration log 

* May 20, 2020: Initial integration step took roughly 3h 41min
* May 27: 3h 1min
    * Skip tests labeled as “Slow” for Ginkgo framework
    * Timelines:
        * Default CNI: 73s
        * Updating CNI image: 110s
        * Current image integration: 47s
        * Conformance tests: 119.167 min (2 hrs)
        * Down cluster: 30 min
* May 29: 2h 59min 30s
    * Cache dependencies when testing default CNI
    * Timelines:
        * Docker build: 4 min
        * Up test cluster: 31 min
        * Default CNI: 50s
        * Updating CNI image: 92s
        * Current image integration: 17s
        * Conformance tests: 114 min (1.9 hrs)
        * Down cluster: 30 min
* June 5: 1h 24min 9s
    * Parallel execution of conformance tests
    * Timelines:
        * Docker build: 3 min
        * Up test cluster: 31 min
        * Default CNI: 52s
        * Updating CNI image: 92s
        * Current image integration: 18s
        * Conformance tests: 16 min
        * Down cluster: 30 min



## How to Manually delete k8s tester Resources (order of deletion)

Cloudformation - (all except cluster, vpc)
EC2 - load balancers, key pair
VPC - Nat gateways, Elastic IPs(after a minute), internet gateway
Cloudformation - cluster
EC2 - network interfaces, security groups
VPC - subnet, route tables
Cloudformation - cluster, vpc(after cluster deletes)
S3 - delete bucket
