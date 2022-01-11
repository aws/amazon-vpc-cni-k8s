## amazon-vpc-cni-k8s Test Framework
The test framework consists of integration and e2e tests using the Ginkgo framework invoked manually and using collection of bash scripts using GitHub Workflow and Prow(not publicly available).

### Types of Tests

#### Pull Request Tests
Runs on each Pull Request, verifies the new code changes don't introduce any regression. Given the entire test suite may span for long duration, run only the integration tests.

#### Nightly Integration Tests
Runs the entire test suite every night using the current GitHub build to catch regression.

#### Canary Tests
Canary tests run frequently, multiple times in a day on live production environment. Given all integration tests run spans for hours we can only run a limited set of tests of most important features along with tests that have dependencies like Load Balancer Service Creation. These test runs are not publicly accessible at this moment.

Ginkgo Focus: [CANARY]

### Smoke Tests
Smoke test provide fail early mechanism by failing the test if basic functionality doesn't work. This can be used as a pre-requisite for the running the much longer Integration tests.

Ginkgo Focus: [SMOKE]


#### Work In Progress
- Run Upstream Conformance tests as part of Nightly Integration tests.
- Run All Integration/e2e tests as part of Nightly Integration tests.
- Run all Integration tests on each Pull Request.
