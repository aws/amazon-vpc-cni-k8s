# ECS ENI Plugin

## Overview

The ECS ENI plugin configures the network namespace of the container with an
Elastic Network Interface (ENI) device. It also starts the `dhclient` process to
renew leases on the ipv4 and ipv6 addresses (when an ipv6 address is specified).
An example configuration for invoking the plugin is listed next:
```json
{
    "type":"ecs-eni",
    "cniVersion":"0.3.0",
    "eni":"eni-eni01en1",
    "ipv4-address":"172.31.31.65/20",
    "mac":"01:23:45:67:89:ab",
    "block-instance-metadata":true
}
```

## Parameters
* `eni` (string, required): the ENI ID
* `ipv4-address` (string, required): the ipv4 address of the ENI. This is the
Primary private IPV4 address of the interface
* `mac` (string, required): the MAC address of the ENI
* `ipv6-address` (string, optional): the ipv6 address of the ENI
* `block-instance-metadata` (bool, optional): specifies if the route to EC2 
instance metadata should be blocked

## Environment Variables
* `ENI_DHCLIENT_LEASES_PATH` (string, optional): the dhclient leases file path.
Set to `/var/lib/dhclient` by default
* `ENI_DHCLIENT_PID_FILE_PATH` (string, optional): the dhclient pid file path.
Set to `/var/run` by default  

## Example
Please ensure that the environment variables needed for running any CNI plugins
are appropriately configured:
* `CNI_COMMAND`: Command to execute eg: ADD.
* `CNI_PATH`: Plugin binary path eg: `pwd`/bin.
* `CNI_IFNAME`: Interface name inside the container

### Add:
```
export CNI_COMMAND=ADD && cat mynet.conf | ../bin/ecs-eni
```

### Del:
```
export CNI_COMMAND=DEL && cat mynet.conf | ../bin/ecs-eni
```

`mynet.conf` is the configuration file for the plugin, it's the same as described
in the overview above.

## Testing

### End-to-end Tests

The end-to-end test suite for this package makes the following assumptions:
1. The test suite is being executed on an EC2 Instance
2. The EC2 Instance has been launched with an IAM Role that has permissions to
   invoke the following APIs:
   1. ec2:DescribeSecurityGroups
   2. ec2:CreateNetworkInterface
   3. ec2:DescribeNetworkInterfaces
   4. ec2:DeleteNetworkInterface
   5. ec2:AttachNetworkInterface
   6. ec2:DetachNetworkInterface
3. The EC2 Instance has room to attach at least one ENI. Please refer to 
[aws eni documentation](http://docs.aws.amazon.com/AWSEC2/latest/UserGuide/using-eni.html#AvailableIpPerENI) for details on the limits based on instance type
4. The `ecs-eni` plugin executable has been built
5. The `CNI_PATH` environment variable points to the location of these plugins
6. The test is being executed with `root` user privileges

Since these tests invoke the the ECS ENI plugin as if an end user such as 
the ECS Agent is invoking it, additional configuration variables can be set to 
prevent the test runner from cleaning up the artifacts generated during the test 
execution for debugging purposes: 
* `ECS_PRESERVE_E2E_TEST_LOGS`: This is set to `false` by default.
Overriding with `true` preserves log files from the plugins

Please refer the [Makefile](../../Makefile) for an example of the command line required to
run end-to-end tests (under the `e2e-test` target).
