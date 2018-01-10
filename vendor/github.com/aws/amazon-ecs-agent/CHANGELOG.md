# Changelog

## 1.16.1
* Bug - Fixed a bug where the agent could miss sending an ENI attachment to ECS
  because of address propagation delays. [#1148](https://github.com/aws/amazon-ecs-agent/pull/1148)
* Enhancement - Upgrade the `amazon-ecs-cni-plugins` to `2017.10.1`. [#1155](https://github.com/aws/amazon-ecs-agent/pull/1155)

## 1.16.0
* Feature - Support pulling from Amazon ECR with specified IAM role in task definition
* Feature - Enable support for task level CPU and memory constraints.
* Feature - Enable the ECS agent to run as a Windows service. [#1070](https://github.com/aws/amazon-ecs-agent/pull/1070)
* Enhancement - Support CloudWatch metrics for Windows. [#1077](https://github.com/aws/amazon-ecs-agent/pull/1077)
* Enhancement - Enforce memory limits on Windows. [#1069](https://github.com/aws/amazon-ecs-agent/pull/1069)
* Enhancement - Enforce CPU limits on Windows. [#1089](https://github.com/aws/amazon-ecs-agent/pull/1089)
* Enhancement - Simplify task IAM credential host setup. [#1105](https://github.com/aws/amazon-ecs-agent/pull/1105)

## 1.15.2
* Bug - Fixed a bug where container state information wasn't reported. [#1076](https://github.com/aws/amazon-ecs-agent/pull/1076)

## 1.15.1
* Bug - Fixed a bug where container state information wasn't reported. [#1067](https://github.com/aws/amazon-ecs-agent/pull/1067)
* Bug - Fixed a bug where a task can be blocked in creating state. [#1048](https://github.com/aws/amazon-ecs-agent/pull/1048)
* Bug - Fixed dynamic HostPort in container metadata. [#1052](https://github.com/aws/amazon-ecs-agent/pull/1052)
* Bug - Fixed bug on Windows where container memory limits are not enforced. [#1069](https://github.com/aws/amazon-ecs-agent/pull/1069)

## 1.15.0
* Feature - Support for provisioning tasks with ENIs.
* Feature - Support for `--init` Docker run flag. [#996](https://github.com/aws/amazon-ecs-agent/pull/996)
* Feature - Introduces container level metadata. [#981](https://github.com/aws/amazon-ecs-agent/pull/981)
* Enhancement - Enable 'none' logging driver capability by default.
  [#1041](https://github.com/aws/amazon-ecs-agent/pull/1041)
* Bug - Fixed a bug where tasks that fail to pull containers can cause the agent
  to fail to restore properly after a restart. [#1033](https://github.com/aws/amazon-ecs-agent/pull/1033)
* Bug - Fixed default logging level issue. [#1016](https://github.com/aws/amazon-ecs-agent/pull/1016)
* Bug - Fixed a bug where unsupported Docker API client versions could be registered.
  [#1014](https://github.com/aws/amazon-ecs-agent/pull/1014)
* Bug - Fixed a bug where non-essential container state changes were sometimes not submitted.
  [#1026](https://github.com/aws/amazon-ecs-agent/pull/1026)

## 1.14.5
* Enhancement - Retry failed container image pull operations [#975](https://github.com/aws/amazon-ecs-agent/pull/975)
* Enhancement - Set read and write timeouts for websocket connectons [#993](https://github.com/aws/amazon-ecs-agent/pull/993)
* Enhancement - Add support for the SumoLogic Docker log driver plugin
  [#992](https://github.com/aws/amazon-ecs-agent/pull/992)
* Bug - Fixed a memory leak issue when submitting the task state change [#967](https://github.com/aws/amazon-ecs-agent/pull/967)
* Bug - Fixed a race condition where a container can be created twice when agent restarts. [#939](https://github.com/aws/amazon-ecs-agent/pull/939)
* Bug - Fixed an issue where `microsoft/windowsservercore:latest` was not
  pulled on Windows under certain conditions.
  [#990](https://github.com/aws/amazon-ecs-agent/pull/990)
* Bug - Fixed an issue where task IAM role credentials could be logged to disk. [#998](https://github.com/aws/amazon-ecs-agent/pull/998)

## 1.14.4
* Enhancement - Batch container state change events. [#867](https://github.com/aws/amazon-ecs-agent/pull/867)
* Enhancement - Improve the error message when reserved memory is larger than the available memory. [#897](https://github.com/aws/amazon-ecs-agent/pull/897)
* Enhancement - Allow plain HTTP connections through wsclient. [#899](https://github.com/aws/amazon-ecs-agent/pull/899)
* Enhancement - Support Logentries log driver by [@opsline-radek](https://github.com/opsline-radek). [#870](https://github.com/aws/amazon-ecs-agent/pull/870)
* Enhancement - Allow instance attributes to be provided from config file
  by [@ejholmes](https://github.com/ejholmes). [#908](https://github.com/aws/amazon-ecs-agent/pull/908)
* Enhancement - Reduce the disconnection period to the backend for idle connections. [#912](https://github.com/aws/amazon-ecs-agent/pull/912)
* Bug - Fixed data race where a pointer was returned in Getter. [#889](https://github.com/aws/amazon-ecs-agent/pull/899)
* Bug - Reset agent state if the instance id changed on agent restart. [#892](https://github.com/aws/amazon-ecs-agent/pull/892)
* Bug - Fixed a situation in which containers may be falsely reported as STOPPED
  in the case of a Docker "stop" API failure. [#910](https://github.com/aws/amazon-ecs-agent/pull/910)
* Bug - Fixed typo in log string by [@sharuzzaman](https://github.com/sharuzzaman). [#930](https://github.com/aws/amazon-ecs-agent/pull/930)

## 1.14.3
* Bug - Fixed a deadlock that was caused by the ImageCleanup and Image Pull. [#836](https://github.com/aws/amazon-ecs-agent/pull/836)

## 1.14.2
* Enhancement - Added introspection API for querying tasks by short docker ID, by [@aaronwalker](https://github.com/aaronwalker). [#813](https://github.com/aws/amazon-ecs-agent/pull/813)
* Bug - Added checks for circular task dependencies. [#796](https://github.com/aws/amazon-ecs-agent/pull/796)
* Bug - Fixed an issue with Docker auth configuration overrides. [#751](https://github.com/aws/amazon-ecs-agent/pull/751)
* Bug - Fixed a race condition in the task clean up code path. [#737](https://github.com/aws/amazon-ecs-agent/pull/737)
* Bug - Fixed an issue involving concurrent map writes. [#743](https://github.com/aws/amazon-ecs-agent/pull/743)

## 1.14.1
* Enhancement - Log completion of image pulls. [#715](https://github.com/aws/amazon-ecs-agent/pull/715)
* Enhancement - Increase start and create timeouts to improve reliability under
  some workloads. [#696](https://github.com/aws/amazon-ecs-agent/pull/696)
* Bug - Fixed a bug where throttles on state change reporting could lead to
  corrupted state. [#705](https://github.com/aws/amazon-ecs-agent/pull/705)
* Bug - Correct formatting of log messages from tcshandler. [#693](https://github.com/aws/amazon-ecs-agent/pull/693)
* Bug - Fixed an issue where agent could crash. [#692](https://github.com/aws/amazon-ecs-agent/pull/692)

## 1.14.0
* Feature - Support definition of custom attributes on agent registration.
* Feature - Support Docker on Windows Server 2016.
* Enhancement - Enable concurrent docker pull for docker version >= 1.11.1.
* Bug - Fixes a bug where a task could be prematurely marked as stopped.
* Bug - Fixes an issue where ECS Agent would keep reconnecting to ACS without any backoff.
* Bug - Fix memory metric to exclude cache value.

## 1.13.1
* Enhancement - Added cache for DiscoverPollEndPoint API.
* Enhancement - Expose port 51679 so docker tasks can fetch IAM credentials.
* Bug - fixed a bug that could lead to exhausting the open file limit.
* Bug - Fixed a bug where images were not deleted when using image cleanup.
* Bug - Fixed a bug where task status may be reported as pending while task is running.
* Bug - Fixed a bug where task may have a temporary "RUNNING" state when
  task failed to start.
* Bug - Fixed a bug where CPU metrics would be reported incorrectly for kernel >= 4.7.0.
* Bug - Fixed a bug that may cause agent not report metrics.

## 1.13.0
* Feature - Implemented automated image cleanup.
* Enhancement - Add credential caching for ECR.
* Enhancement - Add support for security-opt=no-new-privileges.
* Bug - Fixed a potential deadlock in dockerstate.

## 1.12.2
* Bug - Fixed a bug where agent keeps fetching stats of stopped containers.

## 1.12.1
* Bug - Fixed a bug where agent keeps fetching stats of stopped containers.
* Bug - Fixed a bug that could lead to exhausting the open file limit.
* Bug - Fixed a bug where the introspection API could return the wrong response code.

## 1.12.0
* Enhancement - Support Task IAM Role for containers launched with 'host' network mode.

## 1.11.1
* Bug - Fixed a bug where telemetry data would fail to serialize properly.
* Bug - Addressed an issue where telemetry would be reported after the
  container instance was deregistered.

## 1.11.0
* Feature - Support IAM roles for tasks.
* Feature - Add support for the Splunk logging driver.
* Enhancement - Reduced pull status verbosity in debug mode.
* Enhancement - Add a Docker label for ECS cluster.
* Bug - Fixed a bug that could cause a container to be marked as STOPPED while
  still running on the instance.
* Bug - Fixed a potential race condition in metrics collection.
* Bug - Resolved a bug where some state could be retained across different
  container instances when launching from a snapshotted AMI.

## 1.10.0
* Feature - Make the `docker stop` timeout configurable.
* Enhancement - Use `docker stats` as the data source for CloudWatch metrics.
* Bug - Fixed an issue where update requests would not be properly acknowledged
  when updates were disabled.

## 1.9.0
* Feature - Add Amazon CloudWatch Logs logging driver.
* Bug - Fixed ACS handler when acking blank message ids.
* Bug - Fixed an issue where CPU utilization could be reported incorrectly.
* Bug - Resolved a bug where containers would not get cleaned up in some cases.

## 1.8.2
* Bug - Fixed an issue where `exec_create` and `exec_start` events were not
  correctly ignored with some Docker versions.
* Bug - Fixed memory utilization computation.
* Bug - Resolved a bug where sending a signal to a container caused the
  agent to treat the container as dead.

## 1.8.1
* Bug - Fixed a potential deadlock in docker_task_engine.

## 1.8.0
* Feature - Task cleanup wait time is now configurable.
* Enhancement - Improved testing for HTTP handler tests.
* Enhancement - Updated AWS SDK to v.1.0.11.
* Bug - Fixed a race condition in a docker-task-engine test.
* Bug - Fixed an issue where dockerID was not persisted in the case of an
  error.

## 1.7.1
* Enhancement - Increase `docker inspect` timeout to improve reliability under
  some workloads.
* Enhancement - Increase connect timeout for websockets to improve reliability
  under some workloads.
* Bug - Fixed memory leak in telemetry ticker loop.

## 1.7.0
* Feature - Add support for pulling from Amazon EC2 Container Registry.
* Bug - Resolved an issue where containers could be incorrectly assumed stopped
  when an OOM event was emitted by Docker.
* Bug - Fixed an issue where a crash could cause recently-created containers to
  become untracked.

## 1.6.0

* Feature - Add experimental HTTP proxy support.
* Enhancement - No longer erroneously store an archive of all logs in the
  container, greatly decreasing memory and CPU usage when rotating at the
  hour.
* Enhancement - Increase `docker create` timeout to improve reliability under
  some workloads.
* Bug - Resolved an issue where private repositories required a schema in
  `AuthData` to work.
* Bug - Fixed issue whereby metric submission could fail and never retry.

## 1.5.0
* Feature - Add support for additional Docker features.
* Feature - Detect and register capabilities.
* Feature - Add -license flag and /license handler.
* Enhancement - Properly handle throttling.
* Enhancement - Make it harder to accidentally expose sensitive data.
* Enhancement - Increased reliability in functional tests.
* Bug - Fixed potential divide-by-zero error with metrics.

## 1.4.0
* Feature - Telemetry reporting for Services and Clusters.
* Bug - Fixed an issue where some network errors would cause a panic.

## 1.3.1
* Feature - Add debug handler for SIGUSR1.
* Enhancement - Trim untrusted cert from CA bundle.
* Enhancement - Add retries to EC2 Metadata fetches.
* Enhancement - Logging improvements.
* Bug - Resolved an issue with ACS heartbeats.
* Bug - Fixed memory leak in ACS payload handler.
* Bug - Fixed multiple deadlocks.

## 1.3.0

* Feature - Add support for re-registering a container instance.

## 1.2.1

* Security issue - Avoid logging configured AuthData at the debug level on startup
* Feature - Add configuration option for reserving memory from the ECS Agent

## 1.2.0
* Feature - UDP support for port bindings.
* Feature - Set labels on launched containers with `task-arn`,
  `container-name`, `task-definition-family`, and `task-definition-revision`.
* Enhancement - Logging improvements.
* Bug - Improved the behavior when CPU shares in a `Container Definition` are
  set to 0.
* Bug - Fixed an issue where `BindIP` could be reported incorrectly.
* Bug - Resolved an issue computing API endpoint when region is provided.
* Bug - Fixed an issue where not specifiying a tag would pull all image tags.
* Bug - Resolved an issue where some logs would not flush on exit.
* Bug - Resolved an issue where some instance identity documents would fail to
  parse.


## 1.1.0
* Feature - Logs rotate hourly and log file names are suffixed with timestamp.
* Enhancement - Improve error messages for containers (visible as 'reason' in
  describe calls).
* Enhancement - Be more permissive in configuration regarding whitespace.
* Enhancement - Docker 1.6 support.
* Bug - Resolve an issue where data-volume containers could result in containers
  stuck in PENDING.
* Bug - Fixed an issue where unknown images resulted in containers stuck in
  PENDING.
* Bug - Correctly sequence task changes to avoid resource contention. For
  example, stopping and starting a container using a host port should work
  reliably now.

## 1.0.0

* Feature - Added the ability to update via ACS when running under
  amazon-ecs-init.
* Feature - Added version information (available via the version flag or the
  introspection API).
* Enhancement - Clarified reporting of task state in introspection API.
* Bug - Fix a lock scoping issue that could cause an invalid checkpoint file
  to be written.
* Bug - Correctly recognize various fatal messages from ACS to error out more
  cleanly.

## 0.0.3 (2015-02-19)

* Feature - Volume support for 'host' and 'empty' volumes.
* Feature - Support for specifying 'VolumesFrom' other containers within a task.
* Feature - Checkpoint state, including ContainerInstance and running tasks, to
  disk so that agent restarts do not leave dangling containers.
* Feature - Add a "/tasks" endpoint to the introspection API.
* Feature - Add basic support for DockerAuth.
* Feature - Remove stopped ECS containers after a few hours.
* Feature - Send a "reason" string for some of the errors that might occur while
  running a container.
* Bug - Resolve several issues where a container would remain stuck in PENDING.
* Bug - Correctly set 'EntryPoint' for containers when configured.
* Bug - Fix an issue where exit codes would not be sent properly.
* Bug - Fix an issue where containers with multiple ports EXPOSEd, but not
  all forwarded, would not start.

## 0.0.2 (2014-12-17)

* Bug - Worked around an issue preventing some tasks to start due to devicemapper
  issues.
