<!--  Thanks for sending a pull request!  Here are some tips for you:
1. Ensure you have added the unit tests for your changes.
2. Ensure you have included output of manual testing done in the Testing section.
3. Ensure number of lines of code for new or existing methods are within the reasonable limit.
4. Ensure your change works on existing clusters after upgrade.
5. If your mounting any new file or directory, make sure its not opening up any security attack vector for aws-vpc-cni-k8s modules.
6. If AWS apis are invoked, document the call rate in the description section.
7. If EC2 Metadata apis are invoked, ensure to handle stale information returned from metadata.
-->
**What type of PR is this?**

<!--
Add one of the following:
bug
cleanup
documentation
feature
-->

**Which issue(s) this PR fixes**:


**What this PR does / why we need it**:


**If issue # is not available please add repro steps and logs from IPAMD/CNI showing the issue**:


**Testing**:
<!--
output of manual testing/integration tests results and also attach logs
showing the fix being resolved
-->

**Automation added to e2e**:
<!-- 
Test case added to lib/integration.sh 
If no, create an issue with enhancement/testing label
-->

**Will this break upgrades and downgrades. Has it been tested?**:


**Does this require config only upgrades?**:
<!--
If this change does not support kubectl patch please add the reason.
-->


**Does this PR introduce a user-facing change?**:
<!--
If no, just write "NONE" in the release-note block below.
If yes, a release note is required:
Enter your extended release note in the block below. If the PR requires additional action from users switching to the new release, include the string "action required".

-->


```release-note

```

By submitting this pull request, I confirm that my contribution is made under the terms of the Apache 2.0 license.
