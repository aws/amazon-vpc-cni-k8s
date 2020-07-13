## How to run tests
# All tests
    * set AWS_ACCESS_KEY_ID
    * set AWS_SECRET_ACCESS_KEY
    * set AWS_DEFAULT_REGION (optional, defaults to us-west-2 if not set)
    * approve test after build completes

# Performance
    * run from cni test account to upload test results
        * set PERFORMANCE_TEST_S3_BUCKET_NAME to the name of the bucket (likely s3://cni-performance-test-data)
    * set RUN_PERFORMANCE_TESTS=true
    * NOTE: if running on previous versions, change the date inside of the file to the date of release so as to not confuse graphing order

# KOPS
    * set RUN_KOPS_TEST=true
    * will occassionally fail/flake tests, try re-running test a couple times to ensure there is a problem



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