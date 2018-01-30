<!--
Please make sure you've read and understood our contributing guidelines;
https://github.com/aws/amazon-ecs-agent/blob/master/CONTRIBUTING.md

Please provide the following information:
-->

### Summary
<!-- What does this pull request do? -->

### Implementation details
<!-- How are the changes implemented? -->

### Testing
<!-- How was this tested? -->
<!--
Note for external contributors:
`make test` and `make run-integ-tests` can run in a Linux development
environment like your laptop.  `go test -timeout=25s ./agent/...` and
`.\scripts\run-integ.tests.ps1` can run in a Windows development environment
like your laptop.  Please ensure unit and integration tests pass (on at least
one platform) before opening the pull request.  `make run-functional-tests` and
`.\scripts\run-functional-tests.ps1` must be run on an EC2 instance with an
instance profile allowing it access to AWS resources.  Running
`make run-functional-tests` and `.\scripts\run-functional-tests.ps1` may incur
charges to your AWS account; if you're unable or unwilling to run these tests
in your own account, we can run the tests and provide test results.
-->
- [ ] Builds on Linux (`make release`)
- [ ] Builds on Windows (`go build -out amazon-ecs-agent.exe ./agent`)
- [ ] Unit tests on Linux (`make test`) pass
- [ ] Unit tests on Windows (`go test -timeout=25s ./agent/...`) pass
- [ ] Integration tests on Linux (`make run-integ-tests`) pass
- [ ] Integration tests on Windows (`.\scripts\run-integ-tests.ps1`) pass
- [ ] Functional tests on Linux (`make run-functional-tests`) pass
- [ ] Functional tests on Windows (`.\scripts\run-functional-tests.ps1`) pass

New tests cover the changes: <!-- yes|no -->

### Description for the changelog
<!--
Write a short (one line) summary that describes the changes in this
pull request for inclusion in the changelog.
You can see our changelog entry style here:
https://github.com/aws/amazon-ecs-agent/commit/c9aefebc2b3007f09468f651f6308136bd7b384f
-->

### Licensing
<!--
Please confirm that this contribution is under the terms of the Apache 2.0
License.
-->
This contribution is under the terms of the Apache 2.0 License: <!-- yes -->
