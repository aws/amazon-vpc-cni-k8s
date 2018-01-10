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

### Container metadata feature
In order to solve the issues related to container metadata like: [#288](https://github.com/aws/amazon-ecs-agent/issues/288) and [#456](https://github.com/aws/amazon-ecs-agent/issues/456), we are considering adding a new feature "Container Metadata" similar to [EC2 Instance Metadata Service](http://docs.aws.amazon.com/AWSEC2/latest/UserGuide/ec2-instance-metadata.html) in ECS agent to provide the container metadata.

By adding this feature, you can query the information about the task, container and container instance from within the container or from the container instance by reading the metadata file for each container. The following information will be available from this feature:
 * Information from Docker:
  * Container Network Information
  * Container ID from Docker
  * Container Name
  * Image Name
  * Image ID
  * Port Mapping
 * Information from ECS:
  * Cluster Arn
  * Container Instance Arn
  * Task Arn

ECS agent will create a file for each container under the directory `/var/lib/ecs/data/` and then mount the file to the container under a directory eg: `/ecs/metadata` with read only permission when the container is being created. Also an environment variable denoting the file path eg: `ECS_CONTAINER_METADATA_FILE` will be injected into the container. The `ECS_CONTAINER_METADATA_FILE` is consisted of several parts like: "/ecs/metadata/container_name_from_task_definition/metadata.json". After the container is started, ECS agent will update the metadata of container into this file. Then the container can get its metada information by parsing the json file with path `ECS_CONTAINER_METADATA_FILE`. This directory will be removed when task is being clean up. Also this feature will be configurable from the environment variable or in the task definition, eg: adding a field `"metadataEnable": true`. The detailed steps when start a container will be:
* Check whether the feature is enabled, if not then go directly create and start the container.
* Check the task definition make sure the path `/ecs` is not mounted, if it's used in the task definition, then disable the feature.
* Create metadata file for each container under `/var/lib/ecs/data/` for each container.
* Create the container with the two extra steps:
  * Mount the metadata file created above to a specific directory eg: `/ecs/metadata/` with read only permission for each container.
  * Inject the metadata file path as an environment variable to the container eg: `ECS_CONTAINER_METADATA_FILE=/ecs/metadata/container1`.
* After the container is created, write the basic metadata information into the metadata file.
* Start the container and update the networking information into the metadata file.
* When the container is removed during task cleanup, remove the metadata file from disk.

For windows, the mount is a little different like: `%ProgramData%\Amazon\ECS\data` on the host, and `C:\Amazon\ECS\metadata` inside the container.

Information contained in the metadata file will look like:
```json
{
  "Version": "v1",
  "Status": "created",
  "ClusterArn": "arn:aws:ecs:us-west-2:xxxxxxxx:cluster/test",
  "Container_Instance": "arn:aws:ecs:us-west-2:xxxxxxxx:container-instance/732d1a0b-acbe-43e4-8358-f477d83eab03",
  "TaskArn": "",
  "ImageName": "ubuntu",
  "ImageID": "04bbc221d46d3e5eb86c046f22878a8053247ce9b1e6b0a24495b171f9d20025",
  "ContainerID": "ef3dabed9bef087ed147024a221d2d3a822e85d53c0ca4024d3d78e7913eeb64",
  "PortMappings": [{
    "ContainerPort" : 80,
    "HostPort": 80,
    "BindIP": "",
    "Protocol": "tcp"
   }],
  "NetworkMode": "bridge",
  "Gateway": "172.17.0.1",
  "IPAddress": "172.17.0.2",
  "IPv6Gateway": "",
  "LastUpdatedAt": "1487102643"
}
```

General use case:
```
+-----------------------------------------------------------+
|    Containers in task1            Containers in task2     |
|  +----------+  +-----------+        +------------+        |
|  |          |  |           |        |            |        |
|  | /path/c1 |  | /path/c2  |        | /path/c3   |        |
|  +----------+  +-----------+        +------------+        |
|                                                           |
|                     Host                                  |
|             /path/metadata/task1/c1                       |
|             /path/metadata/task1/c2                       |
|             /path/metadata/task2/c3                       |
|                                                           |
+-----------------------------------------------------------+
```

For containers use "--volumes-from", eg: container 2 started with "--volumes-from c1"
```
+-----------------------------------------------------------+
|    Containers in task1            Containers in task2     |
|  +----------+  +-----------+        +------------+        |
|  |  c1      |  |    c2     |        |    c3      |        |
|  | /path/c1 |  | /path/c2  |        | /path/c3   |        |
|  |          |  | /path/c1  |        |            |        |
|  +----------+  +-----------+        +------------+        |
|                                                           |
|                     Host                                  |
|             /path/metadata/task1/c1                       |
|             /path/metadata/task1/c2                       |
|             /path/metadata/task2/c3                       |
|                                                           |
+-----------------------------------------------------------+
```
