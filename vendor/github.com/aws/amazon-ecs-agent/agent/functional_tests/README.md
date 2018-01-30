# Amazon ECS Agent - Functional Tests

This directory contains scripts to perform a functional test of a build of the
agent against the production Amazon Elastic Container Service.

These tests are meant to cover scenarios that are difficult to cover with `go test`.
The bulk of the tests in this repository may be found alongside the Go source
code.

These tests are meant to be run on an EC2 instance with a suitably powerful
role to interact with ECS.
You may be charged for the AWS resources utilized while running these tests.
It is not recommended to run these without understanding the implications of
doing so and it is certainly not recommended to run them on an AWS account
handling production work-loads.

## Test setup

### Linux

Before running these tests, you should build the ECS agent and tag its image as
`amazon/amazon-ecs-agent:make`.

You should also run the registry the tests pull from. This can most easily be done via `make test-registry`.

#### Environment variables
In order to run telemetry functional test in non Amazon Linux AMI environment
with older versions of the ECS agent (pre-1.10.0), the following environment
variables should be set:
* `CGROUP_PATH`: cgroup path on the host, default value "/cgroup"
* `EXECDRIVER_PATH`: execdriver path on the host, default value "/var/run/docker/execdriver"

You can configure the following environment variables to change test
execution behavior:
* `AWS_REGION`: Control the region that is used for test execution
* `ECS_CLUSTER`: Control the cluster used for test execution
* `ECS_AGENT_IMAGE`: Override the default image of
  `amazon/amazon-ecs-agent:make`
* `ECS_FTEST_TMP`: Override the default temporary directory used for storing
  test logs and data files
* `ECS_FTEST_AGENT_ARGS`: Pass additional command-line arguments to the agent
* `ECS_FTEST_FORCE_NET_HOST`: Run the agent with `--net=host`

#### Additional setup for IAM roles
In order to run TaskIamRole functional tests, the following steps should b
done first:
* Run command: `sysctl -w net.ipv4.conf.all.route_localnet=1` and
  `iptables -t nat -A PREROUTING -p tcp -d 169.254.170.2 --dport 80 -j DNAT --to-destination 127.0.0.1:51679`.
* Set the environment variable to enable the test under default network mode:
  `export TEST_TASK_IAM_ROLE=true`.
* Set the environment variable of IAM roles the test will use:
  `export TASK_IAM_ROLE_ARN="iam role arn"`. The role should have the
  `ec2:DescribeRegions` permission and have a trust relationship with
  "ecs-tasks.amazonaws.com". If the `TASK_IAM_ROLE_ARN` environment variable
  isn't set, the test will use the IAM role attached to the instance profile.
  In this case, the IAM role should additionally have the
  `iam:GetInstanceProfile` permission.
* Testing under net=host network mode requires additional commands:
  `iptables -t nat -A OUTPUT -d 169.254.170.2 -p tcp -m tcp --dport 80 -j REDIRECT --to-ports 51679`
  and `export TEST_TASK_IAM_ROLE_NET_HOST=true`

### Windows

Before running these tests, you should build the ECS agent (as `agent.exe`) and
record the directory where the binary is present in the `ECS_WINDOWS_TEST_DIR`
environment variable.

#### Environment variables
You can configure the following environment variables to change test
execution behavior:
* `AWS_REGION`: Control the region that is used for test execution
* `ECS_CLUSTER`: Control the cluster used for test execution
* `ECS_WINDOWS_TEST_DIR`: Override the path used to find `agent.exe`
* `ECS_FTEST_TMP`: Override the default temporary directory used for storing
  test logs and data files

#### Additional setup for IAM roles

For performing the IAM roles test, perform the following additional tasks.
* `$env:TEST_TASK_IAM_ROLE="true"`
* devcon.exe should be present in system environment variable `PATH`.


## Running

These tests should be run on an EC2 instance able to correctly run the ECS
agent and access the ECS APIs.

The best way to run them on Linux is via the `make run-functional-tests`
target.

The best way to run them on Windows is via the `run-functional-tests.ps1`
script located in the `scripts` directory.

They may also be manually run with `go test -tags functional -v ./...` and
you can use standard `go test` flags to control which tests execute (for
example, you can use the `-run` flag to provide a regular expression of tests
to include).
