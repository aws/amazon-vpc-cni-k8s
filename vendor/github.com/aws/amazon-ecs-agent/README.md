# Amazon ECS Container Agent

[![Build Status](https://travis-ci.org/aws/amazon-ecs-agent.svg?branch=master)](https://travis-ci.org/aws/amazon-ecs-agent)
[![Build status](https://ci.appveyor.com/api/projects/status/upkhbwf2oc0srglt?svg=true)](https://ci.appveyor.com/project/AmazonECS/amazon-ecs-agent)


The Amazon ECS Container Agent is software developed for Amazon Elastic Container Service ([Amazon ECS](http://aws.amazon.com/ecs/)).

It runs on container instances and starts containers on behalf of Amazon ECS.

## Usage

The best source of information on running this software is the [Amazon ECS documentation](http://docs.aws.amazon.com/AmazonECS/latest/developerguide/ECS_agent.html).

### On the Amazon Linux AMI

On the [Amazon Linux AMI](https://aws.amazon.com/amazon-linux-ami/), we provide an init package which can be used via `sudo yum install ecs-init && sudo start ecs`. This is the recommended way to run it in this environment.

### On Other Linux AMIs

The Amazon ECS Container Agent may also be run in a Docker container on an EC2 instance with a recent Docker version installed. A Docker image is available in our [Docker Hub Repository](https://registry.hub.docker.com/u/amazon/amazon-ecs-agent/).

```bash
$ # Set up directories the agent uses
$ mkdir -p /var/log/ecs /etc/ecs /var/lib/ecs/data
$ touch /etc/ecs/ecs.config
$ # Set up necessary rules to enable IAM roles for tasks
$ sysctl -w net.ipv4.conf.all.route_localnet=1
$ iptables -t nat -A PREROUTING -p tcp -d 169.254.170.2 --dport 80 -j DNAT --to-destination 127.0.0.1:51679
$ iptables -t nat -A OUTPUT -d 169.254.170.2 -p tcp -m tcp --dport 80 -j REDIRECT --to-ports 51679
$ # Run the agent
$ docker run --name ecs-agent \
    --detach=true \
    --restart=on-failure:10 \
    --volume=/var/run/docker.sock:/var/run/docker.sock \
    --volume=/var/log/ecs:/log \
    --volume=/var/lib/ecs/data:/data \
    --net=host \
    --env-file=/etc/ecs/ecs.config \
    --env=ECS_LOGFILE=/log/ecs-agent.log \
    --env=ECS_DATADIR=/data/ \
    --env=ECS_ENABLE_TASK_IAM_ROLE=true \
    --env=ECS_ENABLE_TASK_IAM_ROLE_NETWORK_HOST=true \
    amazon/amazon-ecs-agent:latest
```

See also the Advanced Usage section below.

### On Windows Server 2016

On Windows Server 2016, the Amazon ECS Container Agent runs as a process or
service on the host. Unlike Linux, the agent may not run inside a container as
it uses the host's registry and the named pipe at `\\.\pipe\docker_engine` to
communicate with the Docker daemon.

#### As a Service
To install the service, you can do the following:

```powershell
PS C:\> # Set up directories the agent uses
PS C:\> New-Item -Type directory -Path ${env:ProgramFiles}\Amazon\ECS -Force
PS C:\> New-Item -Type directory -Path ${env:ProgramData}\Amazon\ECS -Force
PS C:\> New-Item -Type directory -Path ${env:ProgramData}\Amazon\ECS\data -Force
PS C:\> # Set up configuration
PS C:\> $ecsExeDir = "${env:ProgramFiles}\Amazon\ECS"
PS C:\> [Environment]::SetEnvironmentVariable("ECS_CLUSTER", "my-windows-cluster", "Machine")
PS C:\> [Environment]::SetEnvironmentVariable("ECS_LOGFILE", "${env:ProgramData}\Amazon\ECS\log\ecs-agent.log", "Machine")
PS C:\> [Environment]::SetEnvironmentVariable("ECS_DATADIR", "${env:ProgramData}\Amazon\ECS\data", "Machine")
PS C:\> # Download the agent
PS C:\> $agentVersion = "latest"
PS C:\> $agentZipUri = "https://s3.amazonaws.com/amazon-ecs-agent/ecs-agent-windows-$agentVersion.zip"
PS C:\> $zipFile = "${env:TEMP}\ecs-agent.zip"
PS C:\> Invoke-RestMethod -OutFile $zipFile -Uri $agentZipUri
PS C:\> # Put the executables in the executable directory.
PS C:\> Expand-Archive -Path $zipFile -DestinationPath $ecsExeDir -Force
PS C:\> Set-Location ${ecsExeDir}
PS C:\> # Set $EnableTaskIAMRoles to $true to enable task IAM roles
PS C:\> # Note that enabling IAM roles will make port 80 unavailable for tasks.
PS C:\> [bool]$EnableTaskIAMRoles = $false
PS C:\> if (${EnableTaskIAMRoles} {
>> .\hostsetup.ps1
>> }
PS C:\> # Install the agent service
PS C:\> New-Service -Name "AmazonECS" `
        -BinaryPathName "$ecsExeDir\amazon-ecs-agent.exe -windows-service" `
        -DisplayName "Amazon ECS" `
        -Description "Amazon ECS service runs the Amazon ECS agent" `
        -DependsOn Docker `
        -StartupType Manual
PS C:\> sc.exe failure AmazonECS reset=300 actions=restart/5000/restart/30000/restart/60000
PS C:\> sc.exe failureflag AmazonECS 1
```

To run the service, you can do the following:
```powershell
Start-Service AmazonECS
```

#### As a Process

```powershell
PS C:\> # Set up directories the agent uses
PS C:\> New-Item -Type directory -Path ${env:ProgramFiles}\Amazon\ECS -Force
PS C:\> New-Item -Type directory -Path ${env:ProgramData}\Amazon\ECS -Force
PS C:\> New-Item -Type directory -Path ${env:ProgramData}\Amazon\ECS\data -Force
PS C:\> # Set up configuration
PS C:\> $ecsExeDir = "${env:ProgramFiles}\Amazon\ECS"
PS C:\> [Environment]::SetEnvironmentVariable("ECS_CLUSTER", "my-windows-cluster", "Machine")
PS C:\> [Environment]::SetEnvironmentVariable("ECS_LOGFILE", "${env:ProgramData}\Amazon\ECS\log\ecs-agent.log", "Machine")
PS C:\> [Environment]::SetEnvironmentVariable("ECS_DATADIR", "${env:ProgramData}\Amazon\ECS\data", "Machine")
PS C:\> # Set this environment variable to "true" to enable IAM roles.  Note that enabling IAM roles will make port 80 unavailable for tasks.
PS C:\> [Environment]::SetEnvironmentVariable("ECS_ENABLE_TASK_IAM_ROLE", "false", "Machine")
PS C:\> # Download the agent
PS C:\> $agentVersion = "latest"
PS C:\> $agentZipUri = "https://s3.amazonaws.com/amazon-ecs-agent/ecs-agent-windows-$agentVersion.zip"
PS C:\> $zipFile = "${env:TEMP}\ecs-agent.zip"
PS C:\> Invoke-RestMethod -OutFile $zipFile -Uri $agentZipUri
PS C:\> # Put the executables in the executable directory.
PS C:\> Expand-Archive -Path $zipFile -DestinationPath $ecsExeDir -Force
PS C:\> # Run the agent
PS C:\> cd '$ecsExeDir'; .\amazon-ecs-agent.ps1
```

## Building and Running from Source

**Running the Amazon ECS Container Agent outside of Amazon EC2 is not supported.**

### Docker Image (on Linux)

The Amazon ECS Container Agent may be built by typing `make` with the [Docker
daemon](https://docs.docker.com/installation/) (v1.5.0) running.

This produces an image tagged `amazon/ecs-container-agent:make` that
you may run as described above.

### Standalone (on Linux)

The Amazon ECS Container Agent may also be run outside of a Docker container as a
go binary. This is not recommended for production on Linux, but it can be useful for
development or easier integration with your local Go tools.

The following commands run the agent outside of Docker:

```
make gobuild
./out/amazon-ecs-agent
```

### Make Targets (on Linux)

The following targets are available. Each may be run with `make <target>`.

| Make Target            | Description |
|:-----------------------|:------------|
| `release`              | *(Default)* Builds the agent within a Docker container and and packages it into a scratch-based image |
| `gobuild`              | Runs a normal `go build` of the agent and stores the binary in `./out/amazon-ecs-agent` |
| `static`               | Runs `go build` to produce a static binary in `./out/amazon-ecs-agent` |
| `test`                 | Runs all unit tests using `go test` |
| `test-in-docker`       | Runs all tests inside a Docker container |
| `run-integ-tests`      | Runs all integration tests in the `engine` and `stats` packages |
| `run-functional-tests` | Runs all functional tests |
| `clean`                | Removes build artifacts. *Note: this does not remove Docker images* |

### Standalone (on Windows)

The Amazon ECS Container Agent may be built by typing
`go build -o amazon-ecs-agent.exe ./agent`.

### Scripts (on Windows)

The following scripts are available to help develop the Amazon ECS Container
Agent on Windows:

* `scripts\run-integ-tests.ps1` - Runs all integration tests in the `engine` and `stats` packages
* `scripts\run-functional-tests.ps1` - Runs all functional tests
* `misc\windows-deploy\amazon-ecs-agent.ps1` - Helper script to set up the host and run the agent
* `misc\windows-deploy\user-data.ps1` - Sample user-data that can be used with the Windows Server 2016 with Containers AMI


## Advanced Usage

The Amazon ECS Container Agent supports a number of configuration options, most of
which should be set through environment variables.

### Environment Variables

The following environment variables are available. All of them are optional.
They are listed in a general order of likelihood that a user may want to
configure them as something other than the defaults.

| Environment Key | Example Value(s)            | Description | Default value on Linux | Default value on Windows |
|:----------------|:----------------------------|:------------|:-----------------------|:-------------------------|
| `ECS_CLUSTER`       | clusterName             | The cluster this agent should check into. | default | default |
| `ECS_RESERVED_PORTS` | `[22, 80, 5000, 8080]` | An array of ports that should be marked as unavailable for scheduling on this container instance. | `[22, 2375, 2376, 51678, 51679]` | `[53, 135, 139, 445, 2375, 2376, 3389, 5985, 51678, 51679]`
| `ECS_RESERVED_PORTS_UDP` | `[53, 123]` | An array of UDP ports that should be marked as unavailable for scheduling on this container instance. | `[]` | `[]` |
| `ECS_ENGINE_AUTH_TYPE`     |  "docker" &#124; "dockercfg" | The type of auth data that is stored in the `ECS_ENGINE_AUTH_DATA` key. | | |
| `ECS_ENGINE_AUTH_DATA`     | See the [dockerauth documentation](https://godoc.org/github.com/aws/amazon-ecs-agent/agent/engine/dockerauth) | Docker [auth data](https://godoc.org/github.com/aws/amazon-ecs-agent/agent/engine/dockerauth) formatted as defined by `ECS_ENGINE_AUTH_TYPE`. | | |
| `AWS_DEFAULT_REGION` | &lt;us-west-2&gt;&#124;&lt;us-east-1&gt;&#124;&hellip; | The region to be used in API requests as well as to infer the correct backend host. | Taken from Amazon EC2 instance metadata. | Taken from Amazon EC2 instance metadata. |
| `AWS_ACCESS_KEY_ID` | AKIDEXAMPLE             | The [access key](http://docs.aws.amazon.com/general/latest/gr/aws-security-credentials.html) used by the agent for all calls. | Taken from Amazon EC2 instance metadata. | Taken from Amazon EC2 instance metadata. |
| `AWS_SECRET_ACCESS_KEY` | EXAMPLEKEY | The [secret key](http://docs.aws.amazon.com/general/latest/gr/aws-security-credentials.html) used by the agent for all calls. | Taken from Amazon EC2 instance metadata. | Taken from Amazon EC2 instance metadata. |
| `AWS_SESSION_TOKEN` | | The [session token](http://docs.aws.amazon.com/STS/latest/UsingSTS/Welcome.html) used for temporary credentials. | Taken from Amazon EC2 instance metadata. | Taken from Amazon EC2 instance metadata. |
| `DOCKER_HOST`   | `unix:///var/run/docker.sock` | Used to create a connection to the Docker daemon; behaves similarly to this environment variable as used by the Docker client. | `unix:///var/run/docker.sock` | `npipe:////./pipe/docker_engine` |
| `ECS_LOGLEVEL`  | &lt;crit&gt; &#124; &lt;error&gt; &#124; &lt;warn&gt; &#124; &lt;info&gt; &#124; &lt;debug&gt; | The level of detail that should be logged. | info | info |
| `ECS_LOGFILE`   | /ecs-agent.log              | The location where logs should be written. Log level is controlled by `ECS_LOGLEVEL`. | blank | blank |
| `ECS_CHECKPOINT`   | &lt;true &#124; false&gt; | Whether to checkpoint state to the DATADIR specified below. | true if `ECS_DATADIR` is explicitly set to a non-empty value; false otherwise | true if `ECS_DATADIR` is explicitly set to a non-empty value; false otherwise |
| `ECS_DATADIR`      |   /data/                  | The container path where state is checkpointed for use across agent restarts. | /data/ | `C:\ProgramData\Amazon\ECS\data`
| `ECS_UPDATES_ENABLED` | &lt;true &#124; false&gt; | Whether to exit for an updater to apply updates when requested. | false | false |
| `ECS_UPDATE_DOWNLOAD_DIR` | /cache               | Where to place update tarballs within the container. | | |
| `ECS_DISABLE_METRICS`     | &lt;true &#124; false&gt;  | Whether to disable metrics gathering for tasks. | false | true |
| `ECS_RESERVED_MEMORY` | 32 | Memory, in MB, to reserve for use by things other than containers managed by Amazon ECS. | 0 | 0 |
| `ECS_AVAILABLE_LOGGING_DRIVERS` | `["awslogs","fluentd","gelf","json-file","journald","logentries","splunk","syslog"]` | Which logging drivers are available on the container instance. | `["json-file","none"]` | `["json-file","none"]` |
| `ECS_DISABLE_PRIVILEGED` | `true` | Whether launching privileged containers is disabled on the container instance. | `false` | `false` |
| `ECS_SELINUX_CAPABLE` | `true` | Whether SELinux is available on the container instance. | `false` | `false` |
| `ECS_APPARMOR_CAPABLE` | `true` | Whether AppArmor is available on the container instance. | `false` | `false` |
| `ECS_ENGINE_TASK_CLEANUP_WAIT_DURATION` | 10m | Time to wait to delete containers for a stopped task. If set to less than 1 minute, the value is ignored.  | 3h | 3h |
| `ECS_CONTAINER_STOP_TIMEOUT` | 10m | Time to wait for the container to exit normally before being forcibly killed. | 30s | 30s |
| `ECS_ENABLE_TASK_IAM_ROLE` | `true` | Whether to enable IAM Roles for Tasks on the Container Instance | `false` | `false` |
| `ECS_ENABLE_TASK_IAM_ROLE_NETWORK_HOST` | `true` | Whether to enable IAM Roles for Tasks when launched with `host` network mode on the Container Instance | `false` | `false` |
| `ECS_DISABLE_IMAGE_CLEANUP` | `true` | Whether to disable automated image cleanup for the ECS Agent. | `false` | `false` |
| `ECS_IMAGE_CLEANUP_INTERVAL` | 30m | The time interval between automated image cleanup cycles. If set to less than 10 minutes, the value is ignored. | 30m | 30m |
| `ECS_IMAGE_MINIMUM_CLEANUP_AGE` | 30m | The minimum time interval between when an image is pulled and when it can be considered for automated image cleanup. | 1h | 1h |
| `ECS_NUM_IMAGES_DELETE_PER_CYCLE` | 5 | The maximum number of images to delete in a single automated image cleanup cycle. If set to less than 1, the value is ignored. | 5 | 5 |
| `ECS_INSTANCE_ATTRIBUTES` | `{"stack": "prod"}` | These attributes take effect only during initial registration. After the agent has joined an ECS cluster, use the PutAttributes API action to add additional attributes. For more information, see [Amazon ECS Container Agent Configuration](http://docs.aws.amazon.com/AmazonECS/latest/developerguide/ecs-agent-config.html) in the Amazon ECS Developer Guide.| `{}` | `{}` |
| `ECS_ENABLE_TASK_ENI` | `false` | Whether to enable task networking for task to be launched with its own network interface | `false` | Not applicable |
| `ECS_CNI_PLUGINS_PATH` | `/ecs/cni` | The path where the cni binary file is located | `/amazon-ecs-cni-plugins` | Not applicable |
| `ECS_AWSVPC_BLOCK_IMDS` | `true` | Whether to block access to [Instance Metadata](http://docs.aws.amazon.com/AWSEC2/latest/UserGuide/ec2-instance-metadata.html) for Tasks started with `awsvpc` network mode | `false` | Not applicable |
| `ECS_AWSVPC_ADDITIONAL_LOCAL_ROUTES` | `["10.0.15.0/24"]` | In `awsvpc` network mode, traffic to these prefixes will be routed via the host bridge instead of the task ENI | `[]` | Not applicable |
| `ECS_ENABLE_CONTAINER_METADATA` | `true` | When `true`, the agent will create a file describing the container's metadata and the file can be located and consumed by using the container enviornment variable `$ECS_CONTAINER_METADATA_FILE` | `false` | `false` |
| `ECS_HOST_DATA_DIR` | `/var/lib/ecs` | The source directory on the host from which ECS_DATADIR is mounted. We use this to determine the source mount path for container metadata files in the case the ECS Agent is running as a container. We do not use this value in Windows because the ECS Agent is not running as container in Windows. | `/var/lib/ecs` | `Not used` |
| `ECS_ENABLE_TASK_CPU_MEM_LIMIT` | `true` | Whether to enable task-level cpu and memory limits | `true` | `false` |

### Persistence

When you run the Amazon ECS Container Agent in production, its `datadir` should be persisted
between runs of the Docker container. If this data is not persisted, the agent registers
a new container instance ARN on each launch and is not able to update the state of tasks it previously ran.

### Flags

The agent also supports the following flags:

* `-k` &mdash; The agent will not require valid SSL certificates for the services that it communicates with.
* ` -loglevel` &mdash; Options: `[<crit>|<error>|<warn>|<info>|<debug>]`. The
agent will output on stdout at the given level. This is overridden by the
`ECS_LOGLEVEL` environment variable, if present.


## Contributing

Contributions and feedback are welcome! Proposals and pull requests will be
considered and responded to. For more information, see the
[CONTRIBUTING.md](https://github.com/aws/amazon-ecs-agent/blob/master/CONTRIBUTING.md)
file.

Amazon Web Services does not currently provide support for modified copies of
this software.


## License

The Amazon ECS Container Agent is licensed under the Apache 2.0 License.
