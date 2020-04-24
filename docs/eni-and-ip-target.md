## `WARM_ENI_TARGET`, `WARM_IP_TARGET` and `MINIMUM_IP_TARGET` 

The AWS VPC CNI has two components. The CNI binary, `/opt/cni/bin/aws-cni`, and [`ipamd`](https://en.wikipedia.org/wiki/IP_address_management)
running as a Kubernetes daemonset called `aws-node`, adding a pod on every node that keeps track of all ENIs and IPs attached
to the instance. The CNI binary is invoked by the kubelet when a new pod gets added to, or an existing pod removed from the node.

The CNI binary calls `ipamd` using gRPC, asking for an IP for the new pod. If there are no available IPs in the cache, it will
return an error. The number of IPs to keep around is controlled by `WARM_ENI_TARGET`, `WARM_IP_TARGET` and `MINIMUM_IP_TARGET`.
The default setting,`WARM_ENI_TARGET=1` means that `ipamd` should keep "a full ENI" of available IPs around. `ipamd` will
periodically check that it has the right number of available IPs attached, and call EC2 to attach or detach ENIs and IPs when
needed independently of pods being added or deleted on the node.

If the worker nodes are running in small subnets with a limited number of IPs available, using `WARM_IP_TARGET` together with
`MINIMUM_IP_TARGET` is an option. `MINIMUM_IP_TARGET` is the "floor" of how many IPs to keep around on each node. If
you plan to run around 10 pods, the advice is to set `MINIMUM_IP_TARGET` slightly higher than that, like 12, and have
`WARM_IP_TARGET=2`.

Some example cases:

| Instance type | `WARM_ENI_TARGET` | `WARM_IP_TARGET` | `MINIMUM_IP_TARGET` | Pods | Attached  ENIs | Attached  Secondary IPs | Unused IPs | IPs per ENI |
|---------------|:-----------------:|:----------------:|:-------------------:|:----:|:--------------:|:-----------------------:|:----------:|:-----------:|
| t3.small      |         1         |         -        |          -          |   0  |        1       |            3            |      3     |      3      |
| t3.small      |         1         |         -        |          -          |   5  |        3       |            9            |      4     |    3,3,3    |
| t3.small      |         1         |         -        |          -          |   9  |        3       |            9            |      0     |    3,3,3    |
|               |                   |                  |                     |      |                |                         |            |             |
| t3.small      |         -         |         1        |          1          |   0  |        1       |            1            |      1     |      1      |
| t3.small      |         -         |         1        |          1          |   5  |        2       |            6            |      1     |     3,3     |
| t3.small      |         -         |         1        |          1          |   9  |        3       |            9            |      0     |    3,3,3    |
|               |                   |                  |                     |      |                |                         |            |             |
| t3.small      |         -         |         2        |          5          |   0  |        2       |            5            |      5     |     2,3     |
| t3.small      |         -         |         2        |          5          |   5  |        2       |            7            |      2     |    3,3,1    |
| t3.small      |         -         |         2        |          5          |   9  |        3       |            9            |      0     |    3,3,3    |
|               |                   |                  |                     |      |                |                         |            |             |
| p3dn.24xlarge |         1         |         -        |          -          |   0  |        1       |            49           |     49     |      49     |
| p3dn.24xlarge |         1         |         -        |          -          |   3  |        2       |            98           |     95     |    49,49    |
| p3dn.24xlarge |         1         |         -        |          -          |  95  |        3       |           147           |     52     |   49,49,49  |
|               |                   |                  |                     |      |                |                         |            |             |
| p3dn.24xlarge |         -         |         5        |          2          |   0  |        1       |            5            |      5     |      5      |
| p3dn.24xlarge |         -         |         5        |          2          |   3  |        1       |            5            |      2     |      5      |
| p3dn.24xlarge |         -         |         5        |          2          |  10  |        1       |            12           |      2     |      12     |
|               |                   |                  |                     |      |                |                         |            |             |


To be clear, only set `WARM_IP_TARGET` for small clusters, or clusters with very low pod churn. It's also advised to set
`MINIMUM_IP_TARGET` slightly higher than the expected number of pods you plan to run on each node.

The reason to be careful with this setting is that it will increase the number of EC2 API calls that ipamd has to do
to attach and detach IPs to the instance. If the number of calls gets too high, they will get throttled and no new ENIs
or IPs can be attached to any instance in the cluster.

That is the main reason why we have `WARM_ENI_TARGET=1` as the default setting. It's a good balance between having too
many unused IPs attached, and the risk of being throttled by the EC2 API. Creating and attaching a new ENI to a node
can take up to 10 seconds.
