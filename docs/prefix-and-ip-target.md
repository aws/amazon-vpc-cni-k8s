## `WARM_PREFIX_TARGET`, `WARM_IP_TARGET` and `MINIMUM_IP_TARGET`

IPAMD will start allocating (/28) prefixes to the ENIs with `ENABLE_PREFIX_DELEGATION` set to `true`. Setting either `WARM_PREFIX_TARGET` or both `WARM_IP_TARGET` and `MINIMUM_IP_TARGET` environment variables to zero is not supported with prefix delegation enabled. If we set either of these to zero, it will significantly slow IP assignment to the pods as IPAMD will not maintain any additional prefixes in its warm pool. The pod startup latency becomes even more pronounced if IPAMD needs to allocate and attach a new ENI (and wait for IMDS sync) before assigning IPs to new pods. So, we decided against supporting this combination.

`WARM_IP_TARGET` and `MINIMUM_IP_TARGET` if set will override `WARM_PREFIX_TARGET`. `WARM_PREFIX_TARGET` will allocate one full (/28) prefix even if a single IP is consumed with the existing prefix. If the ENI has no space to allocate a prefix, then a new ENI will be created. So make sure to use this only if needed, i.e., if pod density is high since this will be carved out of the ENIs subnet. `WARM_IP_TARGET` and `MINIMUM_IP_TARGET` give more fine-grained control on the number of IPs, but if existing prefixes are not sufficient to maintain the warm pool, then IPAMD will allocate more prefixes to the existing ENI or create a new ENI if the existing ENIs are running out of prefixes.

When a new ENI is allocated, IPAMD will determine the number of prefixes needed to maintain the `WARM_PREFIX_TARGET` or `WARM_IP_TARGET` and `MINIMUM_IP_TARGET` setting, and then those many prefixes will be allocated. This is done to avoid extra EC2 calls to allocate more prefixes, and later free extra prefixes on ENI bring up. New ENI will only be allocated after we exhaust all the prefixes allocated to existing ENIs.

### Examples

This table demonstrates how prefixes and ENIs will be allocated and use as pods will be created and scheduled to an instance. When reading this table, please keep in mind the following:

* Every instance type has different limits of ENI pre instance type, and secondary IPv4 addresses per ENI. This information is available on our [EC2 Docs](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/using-eni.html#AvailableIpPerENI).  
In the example below, `t3.small` was chosen to demonstrate how additional ENIs will be created, since `t3.small` have a maximum of 3 ENIs and 4 IPv4 addresses per ENI. This result in a maximum of 11 available prefixes (3 * 4 minus the primary IPv4 address used by the instance)

* When using prefixes or IPv4 assignment, if the value of `MINIMUM_IP_TARGET` is bigger than N*16, it's equivalent to setting it to (N+1)*16.  
The reason for this is because prefixes are allocated as /28 CIDR block or 16 consecutive IP addresses, so asking for minimum of 20 IPv4 addresses is equally as asking 32 IPv4 addresses

| Instance type | `WARM_PREFIX_TARGET` | `WARM_IP_TARGET` | `MINIMUM_IP_TARGET` | Pods | ENIs | Pod per ENIs | Attached Prefixes | Pod per Prefixes | Unused Prefixes | Prefixes per ENI | Unused IPs |
|---------------|:--------------------:|:----------------:|:-------------------:|:----:|:----:|:------------:|:-----------------:|------------------|:---------------:|:----------------:|:----------:|
| t3.small      | 1                    | -                | -                   | 0    | 1    | 0            | 1                 | 0                | 1               | 1                | 16         |
| t3.small      | 1                    | -                | -                   | 5    | 1    | 5            | 2                 | 5                | 1               | 2                | 27         |
| t3.small      | 1                    | -                | -                   | 17   | 1    | 17           | 3                 | 16,1             | 1               | 3                | 31         |
| t3.small      | 1                    | -                | -                   | 58   | 2    | 48,10         | 5                 | 16,16,16,10,0    | 1               | 3,2              | 22         |
|               |                      |                  |                     |      |      |              |                   |                  |                 |                  |            |
| t3.small      | -                    | 1                | 1                   | 0    | 1    | 0            | 1                 | 0                | 1               | 1                | 16         |
| t3.small      | -                    | 1                | 1                   | 5    | 1    | 5            | 1                 | 5                | 0               | 1                | 11         |
| t3.small      | -                    | 1                | 1                   | 17   | 1    | 17           | 2                 | 16,1             | 0               | 2                | 15         |
| t3.small      | -                    | 1                | 1                   | 58   | 2    | 48,10        | 4                 | 16,16,16,10      | 0               | 3,1              | 6          |
|               |                      |                  |                     |      |      |              |                   |                  |                 |                  |            |
| t3.small      | -                    | 7                | 20                  | 0    | 1    | 0            | 1                 | 0,0              | 1               | 2                | 16         |
| t3.small      | -                    | 7                | 20                  | 5    | 1    | 5            | 2                 | 5,0              | 1               | 2                | 27         |
| t3.small      | -                    | 7                | 20                  | 17   | 1    | 17           | 2                 | 16,1             | 0               | 2                | 15         |
| t3.small      | -                    | 7                | 20                  | 58   | 2    | 48,10        | 5                 | 16,16,16,10,0    | 1               | 3,2              | 22         |
|               |                      |                  |                     |      |      |              |                   |                  |                 |                  |            |
| p3dn.24xlarge | 1                    | -                | -                   | 0    | 1    | 0            | 1                 |                  | 1               | 1                | 16         |
| p3dn.24xlarge | 1                    | -                | -                   | 3    | 2    | 3,0          | 2                 |                  | 1               | 2,0              | 29         |
| p3dn.24xlarge | 1                    | -                | -                   | 95   | 3    | 95,0,0       | 7                 |                  | 1               | 7,0,0            | 17         |
|               |                      |                  |                     |      |      |              |                   |                  |                 |                  |            |
| p3dn.24xlarge | -                    | 5                | 10                  | 0    | 1    | 0            | 1                 |                  | 1               | 1                | 16         |
| p3dn.24xlarge | -                    | 5                | 10                  | 7    | 1    | 7            | 1                 |                  | 0               | 1                | 9          |
| p3dn.24xlarge | -                    | 5                | 10                  | 15   | 1    | 15           | 2                 |                  | 1               | 2                | 17         |
| p3dn.24xlarge | -                    | 5                | 10                  | 45   | 2    | 45,0         | 4                 |                  | 1               | 4,0              | 19         |
|               |                      |                  |                     |      |      |              |                   |                  |                 |                  |            |
