<!--
Copyright 2014-2017 Amazon.com, Inc. or its affiliates. All Rights Reserved.

Licensed under the Apache License, Version 2.0 (the "License"). You may
not use this file except in compliance with the License. A copy of the
License is located at

     http://aws.amazon.com/apache2.0/

or in the "license" file accompanying this file. This file is distributed
on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either
express or implied. See the License for the specific language governing
permissions and limitations under the License.
-->
### Introduction
ECS relies on the networking capability provided by Docker to set up the
network stack for containers. ECS supports the `none` (network interface is not
configured for any container in the task), `bridge` (a primary network interface
is configured for containers using by creating a pair of `veth` devices and
attaching one to the `docker0` bridge and the other to the container) and `host`
(containers just use host's global default namespace) networking modes. As per
comments in [this github issue](https://github.com/aws/amazon-ecs-agent/issues/185),
the `bridge` networking mode results in a number of issues for ECS users:

1. All of the containers launched with this mode share the `docker0` bridge,
  which results in a **performance penalty**
2. Since `docker0` bridge is tied to the primary network interface of the
  instance, it also results in having to configure security groups on
  the primary network interface to satisfy the requirements of both their tasks
  and instances (This is also true for containers launched with `host`
  networking mode), which results in **absence of any network isolation**
3. The docker `bridge` driver allocates IP Addresses from a private address
  space to containers and uses NAT rules on the bridge to route packets to
  containers. Hence, **containers across hosts are not natively addressable**
  using the IP address allocated to them by docker

Providing EC2-esque network capabilities for containers, where they get their
own Network Interface that's routable within the VPC, can alleviate many of
these pain points. Elastic Network Interfaces (ENIs) can be used for this
purpose. This document details the modifications on the container instance to
enable the same.

### Overview of the solution
When an ENI is attached to the container instance for the purpose of being used
by a task, ECS Agent will assign the ENI to a dedicated network namespace and
all the containers in the Task will be able to use this ENI by sharing the
network namespace. ECS Agent will also create routes in this namespace for the
containers to be able to access ECS Agent's credentials endpoint.


                                                +------------------------+
                                            +---|--------------------+   |
                                            |  ENI-1 namespace       |   |
                                            |---+--------------------|   |
                                            |      Routing Table:    |   |
                                            |   +------------------+ |   |
                                          +----+|default via eth1  | +   |
                             172.31.13.14 |eth1||169.254.170.2 via | |   |
                                          +----+|     dev ve_en1   | +   |
                                            |   +------------------+ |---+
                                            |       169.254.x.2      +
                                            +-----------+------------+
                                                        |ve_en1
                                                        |
                                                        |
                                                        |
                                                        |
                          +-------------------------------------------------------------+
                          |default(global) namespace    |                               |
                          |                             |                               |
                          |                     +-------+-------+       +-------------+ |  +------------+
                          |                     |    ve_br_en1  |       |             | |  |net="bridge"|
             172.31.0.226 |   +-----------+     |               |       |             +-|--|container   |
                       +----+ |           |     |ecs-eni bridge |       |             | |  +------------+
                       |eth0| | ECS Agent |     |               |       | docker0     | |
                       +----+ +-----------+     |               |       | bridge      | |
                          |                     |               |       |             | |
                          |                     |  169.254.x.1  |       |             | |
                          |                     +---------------+       +-------------+ |
                          |                                                             |
                          +-------------------------------------------------------------+

In this diagram, the primary network interface is still being used on the instance
by the `docker0` bridge to let containers launched with `net=bridge` to communicate
with entities outside the instance. A secondary network interface has also been
attached for the purpose of being used by a task (`eth1`). This interface has been
moved to the `ENI-1` network namespace. All containers of the task will share this
network namespace.

Tasks (containers in tasks) launched into these namespaces will be addressable
by the primary IP Address of the respective secondary network interfaces. For
example, all containers of the task using ENI `ENI-1` will be addressable within
the VPC using the IP Address `172.31.13.14`.

The `ecs-eni-br` bridge is used by the containers to communicate with the
primary network interface `eth0` for credentials and metadata requests. The
`veth` pairs between the `ENI-1 namespace` and the `ecs-eni-br` bridge are used
for this purpose (`ve-en1` and `ve_br_en1`).

ECS Agent will start a `pause` container for performing this configuration. This
container will only be used to start a process that waits for a signal to
terminate by invoking the `pause()` POSIX API. ECS Agent then moves the ENI to
the network namespace of this container using a CNI plugin. It will then launch
containers of the task with `--net=container:pause-container-id`, thus ensuring
that they use the `pause` container's network namespace.

#### CNI Plugin Sequence
1. Plugin to assign ENI to a network namespace:
	1. Get MAC Address for the ENI from EC2 Instance Metaddata Service
	1. Get ENI device name on default namespace
	1. Get network gateway mask
	1. Get Primary IP Address of the ENI
	1. Move the ENI to container's namespace
	1. Assign the primary IP Address to interface
	1. Setup route to internet via the gateway
	1. Delete entries from the routing table if needed for the ENI device
	1. Start `dhclient` to renew leases

1. Plugin to establish route to ECS Agent for Credentials
	1. ECS Agent determines an available local IP Address from `169.254.0.0/16` (This 
	will be narrowed down to a `/22` or `/23` before implementation)
	1. Create the `ecs-eni` bridge if needed
	1. Create a pair of `veth` devices
	1. Assign one end of `veth` interface to the `ecs-eni` bridge
	1. Assign the local IP address to the other end of the `veth`
	interface and move it to the container's namespace
	1. Create a route to `169.254.170.2` in container's namespace to via the
	`veth` interface

