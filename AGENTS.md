# AGENTS.md

Guidance for coding agents and human contributors working in this repository.

## What this is

The Amazon VPC CNI assigns VPC IP addresses to Kubernetes pods on EKS nodes and wires up their networking. It runs on each node as the `aws-node` DaemonSet, next to the network policy agent container.

- `cmd/aws-vpc-cni`: the aws-node entrypoint. It copies the plugin binaries to the host, starts ipamd, waits for it to be healthy, then writes `10-aws.conflist` from env.
- `cmd/aws-k8s-agent` (ipamd): manages the node's ENIs, secondary IPs and prefixes, and serves the plugin over gRPC on `/var/run/aws-node/ipamd.sock`. Code in `pkg/ipamd/` (`datastore/` holds pod IP state and the checkpoint file), `pkg/awsutils/` (EC2 and IMDS calls).
- `cmd/routed-eni-cni-plugin` (`aws-cni`) and `cmd/egress-cni-plugin` (`egress-cni`): CNI plugins the container runtime runs on the host from `/opt/cni/bin`. They see only the conflist, not the DaemonSet env.
- `cmd/aws-vpc-cni-init`: the init container. It sets node sysctls and copies the upstream CNI plugins to the host.
- `pkg/networkutils/`: host rules, routes, SNAT and connmark, with iptables and nftables backends. `cmd/cni-metrics-helper`: a separate deployment that publishes ipamd metrics to CloudWatch.
- `rpc/rpc.proto`: the gRPC contract between the plugin, ipamd and the network policy agent. `charts/aws-vpc-cni`: the Helm chart, source of truth for the manifests in `config/`. `test/integration/`: ginkgo suites that run against a live cluster.

## Build and test

- `make build-linux`: builds `aws-k8s-agent`, `aws-cni`, `egress-cni` and `grpc-health-probe` with the host Go toolchain (version from `go.mod`; `.go-version` picks only the Docker builder image). `make build-aws-vpc-cni` and `make build-aws-vpc-cni-init` build the other two. `make docker` and `make docker-init` build the images.
- `make unit-test`: `go test` over `./cmd/...` and `./pkg/...` with coverage. `make unit-test-race` runs `-race` on the main packages and has to run natively. For one package: `go test ./pkg/ipamd/...`.
- `make check-format` (needs `goimports`), `make vet` and `make helm-lint`: the checks PR CI runs. `make check` also runs `lint`, which CI does not.
- `make generate`: runs every `go:generate` (mockgen and `protoc` for `rpc/`). Run it after changing an interface or `rpc/rpc.proto` and commit the result; CI does not check for stale mocks. `pkg/nft/mocks` has no generator.
- `make generate-limits`: regenerates `pkg/vpc/vpc_ip_resource_limit.go` and `misc/eni-max-pods.txt`. Never edit those by hand; overrides go in `scripts/gen_vpc_ip_limits.go`.
- `make build-test-binaries`: builds every `test/integration/<suite>`. Suites run against a live cluster, for example `scripts/run-integration-tests.sh` or `scripts/run-ipv6-integration-tests.sh`.

CI:

- `.github/workflows/pr-automated-tests.yaml` runs on PRs to `master`, `release*` and `sdkv2*`: `check-format`, `vet`, `build-linux`, `build-test-binaries`, `helm-lint`, `unit-test`, the nftables kernel tests in a network namespace, and the image builds on amd64 and arm64. No cluster.
- `integration-tests.yaml` (push to `master` and `release*`) and `nightly-cron-tests.yaml` run the `CANARY` specs of `test/integration/cni` and `ipamd`, plus `custom-networking-sgpp`, on one IPv4 Linux cluster, then conformance. A maintainer can run the same tests against a PR with `pr-manual-tests.yaml`.
- No workflow runs `ipv6`, `pod-eni`, `eni-trunking`, `custom-networking`, `cni-egress`, `cni-upgrade-downgrade`, `snat`, `multus` or the non-`CANARY` specs. For changes in those areas, run the suite and paste the output in the PR.
- Release branches are `release-1.<minor>`. Fixes land on `master` and are backported as `[release-1.x]` PRs; the release branch is merged back with a `Release-1.x to Master sync` PR.

## Hard rules

- **CNI DEL never fails on missing state.** DEL returns nil for an unknown pod, and when ipamd is unreachable or errors it tears down from `prevResult` and still returns nil. Keep the `prevResult` interface layout: the previous release's DEL parses it on rollback.
- **Plugin config comes from the conflist.** `aws-cni` and `egress-cni` never read env. A new plugin setting needs a `NetConf` field, a placeholder in `misc/10-aws.conflist` and a replacement in `cmd/aws-vpc-cni/main.go`.
- **Pod key and checkpoint.** The IPAM key is `(NetworkName, ContainerID, IfName)`; a repeated ADD returns the same IP and the `ErrUnknownPod` text stays the same. Checkpoint changes are additive JSON fields with `omitempty`; never change `CheckpointFormatVersion` or the file path, or the previous release fails to start.
- **Release order.** Remove an IP, prefix or ENI from the datastore first (`force=false`), then release it in EC2, and add it to `reconcileCooldownCache`. Free ENIs only through `RemoveUnusedENIFromStore`.
- **ENI filters.** Every loop over ENIs applies the same filters as its siblings: unmanaged ENIs and network cards, trunk, EFA, excluded subnets, and the primary ENI under custom networking. `GetDataStore(n)` can return nil.
- **ENI tags.** ipamd-created ENIs go through `createENI` and keep the `aws-K8S-<instance-id>` description and the `node.k8s.amazonaws.com/*` and cluster tags; the leaked-ENI sweeper, including the previous release's, matches on them.
- **Modes.** Check every change against IPv6 (prefix delegation only, no custom networking, no warm targets), prefix delegation on and off, custom networking, SGP strict and standard, and multiple network cards. Read the prefix delegation flag only from `c.enablePrefixDelegation`.
- **gRPC.** Edit `rpc/rpc.proto`, add fields only, bump the `next field` comment, and run `make generate`. Never hand-edit `*.pb.go`. Plugin and ipamd must carry the same version; ipamd rejects any other.
- **API cost.** No EC2, IMDS or apiserver call per `AddNetwork`/`DelNetwork` or per pool-manager pass without a cache. A new EC2 action goes into `pkg/ec2wrapper`, emits the EC2 metrics, and is added to `docs/iam-policy.md`.
- **Host datapath.** Host state created during ADD is removed if ADD fails. Delete a host veth only after the alias or veth-peer check proves it is this sandbox's. New rule priorities and route tables fit the ranges in `pkg/networkutils/network.go` and `helper.go`, and every IPv6 rule sets its family. SNAT and connmark rules go through the `shouldExist` desired list, and connmark changes work on both the iptables and nftables backends.
- **Errors.** A failed read of rules, chains, ENIs, subnets or security groups is an error, never "absent" and never a fall back to a broader default.
- **Config surface.** Prefer changing a default to adding an env var. A new env var gets a constant and parser next to its siblings, a README entry, and a chart `values.yaml` entry if users set it. Edit `charts/aws-vpc-cni`, never `config/`; release tooling regenerates it.
- **Exposure.** Keep introspection on loopback by default and read-only. Do not rename `awscni_*` metrics; label values stay bounded.
- **Comments.** Describe current behavior and why, not change history.

## Review checklist

`docs/review-guide.md` has the full checklist: ipamd contracts, plugin and host datapath contracts, config and CI, and recurring review themes, each with code citations and a way to check it. Read the sections that match the files a change touches before you open or review a PR. Each item is tagged `[blocking]` or `[advisory]`. When reviewing, report a finding as blocking only when its own impact meets the blocking bar in the guide's section 1, not because the guide item is tagged blocking; list blocking findings first and keep advisory ones brief.

[`docs/using-the-review-guide.md`](docs/using-the-review-guide.md) explains how to use AGENTS.md and the review guide in your workflow.
