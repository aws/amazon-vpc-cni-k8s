## How to run tests
# All tests
    * set AWS_ACCESS_KEY_ID
    * set AWS_SECRET_ACCESS_KEY
    * set AWS_DEFAULT_REGION (optional, defaults to us-west-2 if not set)
    * approve test after build completes
    * Can only run one of the following tests at a time, as most need a unique cluster to work on

# KOPS
    * set RUN_KOPS_TEST=true
    * WARNING: will occassionally fail/flake tests, try re-running test a couple times to ensure there is a 
    
# Bottlerocket
    * set RUN_BOTTLEROCKET_TEST=true

# Calico
    * set RUN_CALICO_TEST=true
    
## How to Manually delete k8s tester Resources (order of deletion)
Cloudformation - (all except cluster, vpc)
EC2 - load balancers, key pair
VPC - Nat gateways, Elastic IPs(after a minute), internet gateway
Cloudformation - cluster
EC2 - network interfaces, security groups
VPC - subnet, route tables
Cloudformation - cluster, vpc(after cluster deletes)
S3 - delete bucket
