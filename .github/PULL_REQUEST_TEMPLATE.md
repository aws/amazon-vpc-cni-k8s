<!--  Thanks for sending a pull request!  Here are some tips for you:
1. Ensure you have added the unit tests for your changes.
2. Ensure you have included output of manual testing done in the Testing section.
3. Ensure number of lines of code for new or existing methods are within the reasonable limit.
4. Ensure your change works on existing clusters after upgrade.
5. If you are mounting any new file or directory, make sure it is not opening up any security attack vector for aws-vpc-cni-k8s modules.
6. If AWS APIs are invoked, document the call rate in the description section.
7. If EC2 Metadata apis are invoked, ensure to handle stale information returned from metadata.
-->
**What type of PR is this?**
<!--
Add one of the following:
bug
cleanup
dependency update
documentation
feature
improvement
release workflow
testing
-->

**Which issue does this PR fix?**:
<!-- If an issue # is not available please add repro steps and logs from IPAMD/CNI showing the issue -->


**What does this PR do / Why do we need it?**:


**Testing done on this change**:
<!--
Please paste the output from manual and/or integration test results. Please also attach any relevant logs.
-->

<!-- 
If adding a new integration test to any of the CNI release test suites, determine if the test can run against the latest VPC CNI image or if it is dependent on a future version release. If dependent, please call `Skip()` in the test to prevent it from running before the future version is available.
-->

**Will this PR introduce any new dependencies?**:
<!-- 
e.g. new EC2/K8s API, IMDS API, dependency on specific kernel module/version or binary in container OS.
-->

**Will this break upgrades or downgrades? Has updating a running cluster been tested?**:


**Does this change require updates to the CNI daemonset config files to work?**:
<!--
If this change does not work with a "kubectl patch" of the image tag, please explain why.
-->

**Does this PR introduce any user-facing change?**:
<!--
If yes, a release note update is required:
Enter your extended release note in the block below. If the PR requires additional actions
from users switching to the new release, include the string "action required".
-->

```release-note

```

By submitting this pull request, I confirm that my contribution is made under the terms of the Apache 2.0 license.
