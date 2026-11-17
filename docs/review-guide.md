# Review guide for amazon-vpc-cni-k8s

## 1. Purpose and how to use it

This guide lists the problems maintainers keep finding in review, so authors and reviewers can find them first. It covers the contracts inside ipamd (pod keys, the checkpoint, the warm pool, ENI tagging and selection), the contracts between the CNI plugin, ipamd and the network policy agent, the host datapath, and what CI does and does not test.

An item is tagged `[blocking]` when, whenever its condition holds, the change gives pods wrong or duplicate IPs or wrong connectivity, leaks IPs, ENIs or host network state without bound, leaves a node unable to assign IPs or route pod traffic until someone intervenes, or leaves nodes broken after an upgrade or rollback completes. It is `[advisory]` when the harm is transient (it self-corrects on the next reconcile, retry or restart), needs an unusual precondition, or is about quality, noise, tests or docs. Each blocking item has a "Blocking when:" clause, and some advisory items a "Blocking only if:" clause, naming the condition that makes an instance blocking. Report blocking findings first and keep advisory ones brief. The tag is the item's worst case, so judge each finding by its own reach and whether it self-corrects, and don't copy the tag.

Run it against a diff. For each changed area, look up the matching checks:

| If the diff touches | Read |
|---|---|
| `pkg/ipamd/datastore/` | [I1](#i1), [I2](#i2), [I3](#i3), [I4](#i4), [I7](#i7), [I8](#i8), [I18](#i18), [I21](#i21) |
| `pkg/ipamd/rpc_handler.go`, `rpc/` | [I1](#i1), [I8](#i8), [I10](#i10), [I11](#i11), [I13](#i13), [I18](#i18), [R1](#r1), [D1](#d1) |
| `pkg/ipamd/ipamd.go`: `New`, `nodeInit`, `handlePreScaling` | [I2](#i2), [I9](#i9), [I12](#i12), [I15](#i15), [I18](#i18), [I19](#i19) |
| `pkg/ipamd/ipamd.go`: pool manager, reconcile, `tryFreeENI`, `tryAssign*` | [I3](#i3), [I4](#i4), [I5](#i5), [I7](#i7), [I8](#i8), [I13](#i13), [I18](#i18) |
| `pkg/ipamd/ipamd.go`: `setupENI`, `filterUnmanagedENIs`, `hasRoomForEni`, exclusion helpers | [I7](#i7), [I8](#i8), [I9](#i9) |
| `pkg/ipamd/ipamd.go`: env constants and parsers | [I5](#i5), [I17](#i17), [I18](#i18) |
| `pkg/ipamd/ipamd.go`: SGP, CNINode, API server access | [I10](#i10), [I19](#i19) |
| `pkg/ipamd/ipamd.go`: host network init, VPC CIDR loop | [N1](#n1) |
| `pkg/awsutils/`, `pkg/ec2wrapper/` | [I6](#i6), [I7](#i7), [I14](#i14), [I15](#i15), [I22](#i22) |
| `pkg/eniconfig/`, `pkg/k8sapi/` | [I15](#i15), [I19](#i19) |
| `pkg/vpc/`, `scripts/gen_vpc_ip_limits.go`, `misc/eni-max-pods.txt` | [I16](#i16) |
| `cmd/routed-eni-cni-plugin/cni.go` | [D1](#d1), [D2](#d2), [D3](#d3), [D4](#d4), [D5](#d5), [S1](#s1), [R1](#r1), [I1](#i1), [I11](#i11) |
| `cmd/routed-eni-cni-plugin/driver/` | [D5](#d5), [D6](#d6), [D7](#d7), [D8](#d8), [S1](#s1) |
| `cmd/egress-cni-plugin/` | [D3](#d3), [D4](#d4), [E1](#e1) |
| `cmd/aws-vpc-cni/`, `misc/10-aws.conflist` | [D3](#d3), [D10](#d10), [E1](#e1), [S1](#s1), [I17](#i17) |
| `cmd/aws-vpc-cni-init/`, `scripts/dockerfiles/` | [D9](#d9) |
| `cmd/aws-k8s-agent/` | [I12](#i12), [I19](#i19), [I20](#i20), [R1](#r1) |
| `pkg/networkutils/`, `pkg/iptableswrapper/`, `pkg/nft/` | [D6](#d6), [D7](#d7), [D8](#d8), [N1](#n1), [N2](#n2), [S1](#s1) |
| `pkg/sgpp/` | [S1](#s1), [I10](#i10) |
| `pkg/ipamd/introspect.go`, `utils/prometheusmetrics/`, `cmd/cni-metrics-helper/` | [I20](#i20), [I21](#i21), [R1](#r1) |
| `pkg/utils/logger/`, new log lines | [I22](#i22) |
| `charts/aws-vpc-cni/`, `config/`, `README.md` env sections | [C1](#c1), [I17](#i17), [I19](#i19), [D3](#d3) |
| `docs/iam-policy.md`, a new EC2 call | [I14](#i14) |
| `test/`, `scripts/`, `.github/workflows/` | [C2](#c2), [CI coverage](#ci) |
| Any `generate_mocks.go` or `mocks/` | [C3](#c3) |
| Any change | [General review hygiene](#ck-general) |

**Citations.** Every citation is `path:line` at `aws/amazon-vpc-cni-k8s` master `9e78e390` (2026-11-16) unless it names another commit. A citation that starts with `:` is in the last file named before it. If a line has moved, find it by the quoted symbol.

Items marked "(inferred)" rest on reasoning, or on kernel, Kubernetes or EC2 behavior that was not traced in source. Treat them as questions, not facts.

## 2. Quick checklist

<a id="ck-ipamd"></a>
**ipamd: IP and ENI state**

- [blocking] The pod key is `(NetworkName, ContainerID, IfName)`, and ADD/DEL are idempotent on it. [I1](#i1) Blocking when: a change keys an allocation by pod name/namespace/UID, adds a field to `IPAMKey`, or makes a repeated ADD allocate a second IP or a DEL for an unknown key return a non-`ErrUnknownPod` error.
- [blocking] Checkpoint file: additive JSON only, version string never bumped casually. [I2](#i2) Blocking when: a change renames or removes a `CheckpointEntry`/`IPAMKey`/`IPAMMetadata` JSON field, changes `CheckpointFormatVersion`, changes the default path or the `-nic-N` naming, or returns success from an assign/unassign without writing the checkpoint.
- [blocking] Release order: datastore first, EC2 second, cooldown cache in between. [I3](#i3) Blocking when: a change calls `DeallocIPAddresses`/`DeallocPrefixAddresses`/`FreeENI` before removing the CIDR or ENI from the datastore, removes with `force=true` outside the reconcile sweep, or frees CIDRs without adding them to `reconcileCooldownCache`.
- [blocking] ENI deletability and pool-shrink guards. [I4](#i4) Blocking when: a change frees an ENI that is primary, trunk, EFA, younger than `minENILifeTime`, holding a cooling IP, or holding pods; or frees ENIs while the node is terminating or unschedulable without `AWS_MANAGE_ENIS_NON_SCHEDULABLE`.
- [blocking] Warm-pool env vars: precedence and per-card defaults. [I5](#i5) Blocking when: a change alters which of `WARM_IP_TARGET`/`MINIMUM_IP_TARGET`/`WARM_ENI_TARGET`/`WARM_PREFIX_TARGET` wins, so an existing node either never grows its pool or allocates and frees in a loop.
- [blocking] ENI tags, description and the leaked-ENI sweeper are one contract. [I6](#i6) Blocking when: a change alters `eniDescriptionPrefix`, the `node.k8s.amazonaws.com/instance_id`, `node.k8s.amazonaws.com/createdAt`, `cluster.k8s.amazonaws.com/name` or `eks:eni:owner` keys or values, creates an ENI without `createENITags()`, or adds a new ipamd-created ENI type that the sweeper cannot see.
- [blocking] ENI selection filters every loop must apply. [I7](#i7) Blocking when: a loop that allocates to, frees from, re-tags, re-SGs or counts ENIs omits a filter its sibling loops apply ([T3](#themes)).
- [blocking] Multi-NIC: one datastore and one checkpoint per managed card. [I8](#i8) Blocking when: new code assumes one datastore, calls `GetDataStore(n)` without handling a nil return for a skipped card, or applies card-0 env targets to cards > 0.
- [blocking] IPv6 mode is a different pool, not a flag on the IPv4 pool. [I9](#i9) Blocking when: a change alters a path IPv6 shares (setupENI, reconcile, RPC handler, datastore assign) for an IPv4 reason without an IPv6 test, or removes an `enableIPv6` guard ([T13](#themes)).
- [blocking] Security groups for pods: ipamd reads, the VPC Resource Controller allocates. [I10](#i10) Blocking when: ipamd starts creating, attaching or freeing branch ENIs, stores branch IPs in the datastore, drops the pod-UID check on DEL, or reports SGP enabled in CNINode without a free ENI slot for the trunk.
- [blocking] Plugin-ipamd gRPC: strict version match, regenerated proto, append-only fields. [I11](#i11) Blocking when: a PR hand-edits `rpc/rpc.pb.go`/`rpc_grpc.pb.go`, renumbers or reuses a proto field, relaxes `validateVersion` without a plan, or adds a plugin-side setting that only ipamd's environment has.
- [blocking] nodeInit: what may fail startup and what must only log. [I12](#i12) Blocking when: a new step in `New`/`nodeInit` returns an error for a condition that a restart will not fix (aws-node crash-loops, node stays NotReady), or a step that leaves the node unable to route pod IPs only logs.
- [advisory] Pool manager and RPC handlers run concurrently; only the datastore is locked. [I13](#i13) Blocking only if: new shared state on `IPAMContext` is written from both the pool-manager goroutine and an RPC handler without synchronization, and a race can hand out or free the wrong address.
- [advisory] EC2 calls: one session, nested retries, metrics and IAM docs per call. [I14](#i14) Blocking only if: a new EC2 call runs per pod or per reconcile pass with no cache, or retries without backoff ([T6](#themes)).
- [blocking] Custom networking: ENIConfig selects subnet and SGs for every new ENI. [I15](#i15) Blocking when: a change creates an ENI under `AWS_VPC_K8S_CNI_CUSTOM_NETWORK_CFG=true` without the ENIConfig subnet, assigns pod IPs from the primary ENI, or rewrites ENIConfig-created ENIs' SGs with the node's primary SGs.
- [advisory] `pkg/vpc` limits and `misc/eni-max-pods.txt` are generated. [I16](#i16) Blocking only if: the PR hand-edits `pkg/vpc/vpc_ip_resource_limit.go` or `misc/eni-max-pods.txt` with values that overstate ENI or IP limits, since `hasRoomForEni` and `maxPods` then attempt attachments EC2 rejects on every pool tick.
- [advisory] A new ipamd env var touches five places, and the chart default may differ from the code default. [I17](#i17) Blocking only if: a renamed or removed env var silently changes behavior on upgrade for users who set the old name, or a new default changes pool size on existing nodes.
- [blocking] Prefix delegation: one effective flag, /28 only, toggle cleanup at startup. [I18](#i18) Blocking when: code re-reads `ENABLE_PREFIX_DELEGATION` after `New`, assigns from a CIDR whose kind (prefix vs /32) does not match the effective mode, releases a /28 with any assigned address, or drops the startup cleanup that releases the other kind's unused CIDRs.
- [blocking] Kubernetes access: optional at startup, cached per node, RBAC in the chart. [I19](#i19) Blocking when: ipamd `Get`s a new resource type (it starts an unfiltered, cluster-wide informer and needs list/watch RBAC), adds a new CRD read without RBAC in `charts/aws-vpc-cni/templates/clusterrole.yaml`, or uses `k8sClient` on a path that runs when `withApiServer` is false.
- [advisory] Metrics: names are an API, labels stay bounded. [I20](#i20) Blocking only if: a change renames or removes an `awscni_*` metric or label that `cni-metrics-helper` reads, since CloudWatch dashboards and alarms then go silent.
- [advisory] Introspection endpoint: loopback, read-only, JSON shape used by support tooling. [I21](#i21) Blocking only if: the default bind leaves loopback, or a handler mutates state.
- [advisory] Logging: package logger, lazy in code the plugin imports, levels by frequency. [I22](#i22) Blocking only if: a package imported by the `aws-cni` plugin binary builds the logger at package init (`var log = logger.Get()`), which creates the ipamd log file from the plugin and fails on read-only roots (#3893).

<a id="ck-datapath"></a>
**CNI plugin and host datapath**

- [blocking] CNI DEL must succeed without ipamd and must not fail on missing state. [D1](#d1) Blocking when: a DEL path can return an error for state that is already gone, or stops tearing down host rules when ipamd is unreachable or rejects the call.
- [blocking] `prevResult` interface encoding is a cross-version contract. [D2](#d2) Blocking when: a change alters what ADD writes into `Interfaces` (names, order, count, `Mac`/`Sandbox` meaning) or what DEL parses, without keeping the older encodings readable.
- [blocking] The plugin reads its settings only from the conflist. [D3](#d3) Blocking when: code under `cmd/routed-eni-cni-plugin` or `cmd/egress-cni-plugin` reads an env var, or a new plugin setting is added to `NetConf` without a placeholder and a renderer.
- [advisory] CHECK is a no-op. [D4](#d4)
- [blocking] A failed ADD must undo the host state it created. [D5](#d5) Blocking when: a new host-side object (rule, route, link, iptables/nft entry) is created in ADD before a step that can fail, with no removal on that failure path.
- [blocking] Never delete a host veth you cannot prove belongs to this sandbox. [D6](#d6) Blocking when: a change deletes or renames an existing host link by name without the alias stamp or the veth-peer check, or changes the alias format or veth naming hash.
- [blocking] Rule priorities and route-table ID spaces are fixed. [D7](#d7) Blocking when: a change adds or moves a rule priority, or computes a route table ID, without checking it against every range below and against what n-1 deletes.
- [blocking] IPv6 pod setup: sysctls before link up, family on every rule. [D8](#d8) Blocking when: a v4 change touches `createVethPairContext.run`, `setupVlan` or a rule builder that v6 shares, or a v6 rule is created without `Family = AF_INET6`.
- [blocking] SGP strict and standard modes set up different host state. [S1](#s1) Blocking when: a change to the branch-ENI path handles one mode only, adds a third mode, or changes teardown so it no longer removes both modes' rules.
- [blocking] SNAT and connmark rules converge from a desired list, and only at start or on VPC CIDR change. [N1](#n1) Blocking when: a change adds a rule outside the desired-list machinery, makes an "absent" rule depend on a failed read, or assumes the rules are reconciled periodically.
- [blocking] Connmark backend: iptables or nftables, chosen once, each cleans the other. [N2](#n2) Blocking when: a change to connmark rules works on one backend only, changes how existing nft rules are classified, or changes the nft table/chain/register layout without checking what v1.23.0/1.23.1 do on rollback.
- [blocking] egress-cni: chained, gated by `enabled`, and DEL depends on the pod's link. [E1](#e1) Blocking when: egress ADD can leave a host-local IP or SNAT chain without the matching DEL removing it, or the plugin starts running when `enabled` is not `"true"`.
- [blocking] aws-vpc-cni-init sets node-wide sysctls and installs host binaries; nothing reverts them. [D9](#d9) Blocking when: a change adds a sysctl or a host file in the init container without saying what happens on rollback or when the flag is turned off.
- [blocking] aws-vpc-cni startup order: binaries, ipamd healthy, then conflist. [D10](#d10) Blocking when: a change writes `10-aws.conflist` before ipamd is serving, removes the health wait, or lets the plugin and ipamd come from different builds.
- [blocking] ipamd gRPC lives on a unix socket; the proto is shared with the plugin and the network policy agent. [R1](#r1) Blocking when: a change edits `rpc.pb.go` by hand, renumbers or removes a proto field, moves the socket or changes its mode, or makes ADD/DEL depend on the NP agent when `NETWORK_POLICY_ENFORCING_MODE` is unset.

<a id="ck-config"></a>
**Config, tests and CI**

- [advisory] Change the chart, not `config/master`. [C1](#c1)
- [advisory] Say which cluster suite you ran; CI runs only the IPv4 Linux canary set. [C2](#c2)
- [advisory] Regenerate mocks and generated code; CI does not check. [C3](#c3)

<a id="ck-general"></a>
**General review hygiene**

These apply to any change. Where a line links an entry or theme, that makes it specific to this repository.

- [advisory] If a call fails, does the error reach the caller, or does the code continue as if the state were absent or fall back to a broader default (primary-ENI SGs, an unexcluded subnet, a duplicate chain)? [T1](#themes) Blocking only if: a failed read is treated as "absent" and the next pass deletes or duplicates live state, or widens access.
- [advisory] What happens on upgrade from the last release, and on rollback to it, to the checkpoint, ENIs and their tags, host rules, nft/iptables state and the conflist? [I2](#i2), [I6](#i6), [D2](#d2), [N2](#n2), [T11](#themes)
- [advisory] Is the change correct for IPv6 clusters, for prefix delegation on and off, with custom networking, with SGP in both modes, and with more than one network card? [I5](#i5), [I8](#i8), [I9](#i9), [I15](#i15), [S1](#s1), [T3](#themes), [T13](#themes)
- [advisory] How many EC2, IMDS and apiserver calls does the change add per pod and per pool-manager pass? Attach before and after numbers when the count rises. [I13](#i13), [I14](#i14), [T6](#themes)
- [advisory] Does the code run in the right process? The host plugin sees no DaemonSet env; ipamd sees no conflist. [D3](#d3), [T7](#themes)
- [advisory] Each new test fails on master, passes with the change and calls production code. Mocks use real values and exact call counts (`.Times(1)`, not `gomock.Any()` / `.AnyTimes()`). [T8](#themes)
- [advisory] A datapath, IPAM or kernel-race fix is backed by a cluster suite run, not only by mocked unit tests, and the PR names the suite, IP family, OS and iptables backend. [C2](#c2), [T2](#themes), [T12](#themes)
- [advisory] Does an existing helper, constant or data source already do this? [I7](#i7), [I14](#i14), [T9](#themes)
- [advisory] ARNs, endpoints and service names come from the resolved partition, never a hard-coded `aws`. [T17](#themes) Blocking only if: the hard-coded value is on a path every node takes in CN or GovCloud.
- [advisory] Log lines are at the right level for how often they fire, not inside the datastore lock, and contain nothing sensitive. [I22](#i22), [T10](#themes)
- [advisory] A new env var is really needed (vs changing a default), is documented in the README and wired through the chart. [I17](#i17), [C1](#c1), [T4](#themes)
- [advisory] Comments describe current behavior and why, without change history.
- [advisory] The PR description matches the final code and fills in the template's testing, upgrade/downgrade and new-dependency sections (`.github/PULL_REQUEST_TEMPLATE.md`).

<a id="ipamd"></a>
## 3. ipamd contracts

These cover `cmd/aws-k8s-agent`, `pkg/ipamd`, `pkg/ipamd/datastore`, `pkg/awsutils`, `pkg/vpc`, `rpc/` and the config they read. Blocking bar: pods get wrong or duplicate IPs, IPs or ENIs leak and grow without bound, a node cannot assign IPs and does not self-correct, or an upgrade or rollback leaves nodes broken.

<a id="i1"></a>
### [blocking] I1. The pod key is `(NetworkName, ContainerID, IfName)`, and ADD/DEL are idempotent on it

Blocking when: a change keys an allocation by pod name/namespace/UID, adds a field to `IPAMKey`, or makes a repeated ADD allocate a second IP or a DEL for an unknown key return a non-`ErrUnknownPod` error.

**Rule.** `IPAMKey` is the CNI-spec primary key and nothing else. A repeated `AddNetwork` with the same key returns the existing address. `DelNetwork` for an unknown key returns `datastore.ErrUnknownPod`, which the plugin matches by string and turns into success. Pod name/namespace live in `IPAMMetadata` only.

**Why.** kubelet retries ADD and always calls DEL after a failed ADD; a second IP per retry leaks pool addresses, and a DEL error that is not `ErrUnknownPod` makes the sandbox teardown loop. Two sandboxes of one pod (restart) share name/namespace but not ContainerID, so a name key would hand one IP to two sandboxes. Pre-CRI-migration entries are found under the backfill key, so a new key shape strands them.

**Cites.** `pkg/ipamd/datastore/data_store.go:85-93` (key), `:106-110` (metadata), `:788-791` (duplicate assign returns existing), `:1246-1257` (`_migrated-from-cri` fallback, `ErrUnknownPod`); `pkg/ipamd/rpc_handler.go:207-222`, `:335-339`; `cmd/routed-eni-cni-plugin/cni.go:451-456` (string match on `ErrUnknownPod.Error()`).

**Check.** Does a test call ADD twice with one key and get one IP, and DEL twice and get success both times? Is the `ErrUnknownPod` text unchanged?

<a id="i2"></a>
### [blocking] I2. Checkpoint file: additive JSON only, version string never bumped casually

Blocking when: a change renames or removes a `CheckpointEntry`/`IPAMKey`/`IPAMMetadata` JSON field, changes `CheckpointFormatVersion`, changes the default path or the `-nic-N` naming, or returns success from an assign/unassign without writing the checkpoint.

**Rule.** `/var/run/aws-node/ipam.json` (card 0) and `ipam-nic-<N>.json` (card N) hold `{"version":"vpc-cni-ipam/1","allocations":[...]}`. Add fields with `omitempty`; never change the version string unless n-1 can read the new one. Every state change writes the file under the datastore lock and unwinds on write failure.

**Why.** `ReadBackingStore` returns an error on any other version string, and `nodeInit` returns that error, so a rolled-back n-1 ipamd crash-loops and the node assigns no IPs. `encoding/json` ignores unknown fields, which is why additive changes are rollback-safe (inferred from `json.NewDecoder(f).Decode` with no `DisallowUnknownFields`). Restore also drops entries whose host veth is gone and re-writes the file, so a field that changes the veth-name inputs drops live pods on upgrade.

**Cites.** `pkg/ipamd/datastore/data_store.go:325-344` (format), `:348-367` (version check), `:416-419` (rewrite after restore), `:425-465` (writer), `:1627-1665` (veth normalization; empty name/namespace skips it for old entries), `:1732-1745` (per-card path); `pkg/ipamd/datastore/checkpoint.go:76-112` (temp file + rename, plain decoder); `pkg/ipamd/ipamd.go:144-146` (default path, `AWS_VPC_K8S_CNI_BACKING_STORE`), `:613-616` (`ReadAllDataStores` error fails init); unwind precedents `data_store.go:842-851`, `:1260-1265`. [T11](#themes).

**Check.** Does the PR add a field with `omitempty` and a test that restores a checkpoint written without it? Did the PR author run `test/integration/cni-upgrade-downgrade`?

<a id="i3"></a>
### [blocking] I3. Release order: datastore first, EC2 second, cooldown cache in between

Blocking when: a change calls `DeallocIPAddresses`/`DeallocPrefixAddresses`/`FreeENI` before removing the CIDR or ENI from the datastore, removes with `force=true` outside the reconcile sweep, or frees CIDRs without adding them to `reconcileCooldownCache`.

**Rule.** Shrink paths remove from the datastore with `force=false` (fails if a pod holds an address), then call EC2, and record each freed IP/prefix in `reconcileCooldownCache` (60 s) so the next reconcile does not re-add it from stale IMDS. `force=true` is only for state that EC2/IMDS says is already gone.

**Why.** EC2-first lets a concurrent `AddNetwork` hand out an address that EC2 is unassigning: the pod gets an IP the VPC no longer routes to it. Without the cooldown entry, IMDS still lists the IP, reconcile adds it back, and the pool flaps or a pod gets a released IP. If EC2 release fails after datastore removal, reconcile re-adds the ENI or IP as "new", so the node self-corrects.

**Cites.** `pkg/ipamd/ipamd.go:944-962` (delete `force=false`, then `DeallocCidrs`), `:2753-2781` (`DeallocCidrs` adds to cooldown), `:345-377` (cache), `:68-70` (`ipReconcileCooldown`), `:1866-1916` (reconcile honours cooldown, re-verifies with EC2), `:875-916` (`tryFreeENI`: `RemoveUnusedENIFromStore` then `FreeENI`); `pkg/ipamd/datastore/data_store.go:568-603` (`force` semantics).

**Check.** In the diff, is every new EC2 release call preceded by a datastore delete that can fail on in-use, and followed or preceded by `reconcileCooldownCache.Add`? Existing gaps not to copy: `tryUnassignIPFromENI`/`tryUnassignPrefixFromENI` skip the cooldown cache (`ipamd.go:2469-2552`; a datastore/IMDS mismatch forces an EC2 read at `:1772-1783`, which limits the damage); and the forced branch of `RemoveENIFromDataStore` subtracts each CIDR from `ds.total` twice (`data_store.go:1200`, `:1212`), so pool stats undercount after a forced ENI removal (observed on master; effect on pool decisions inferred).

<a id="i4"></a>
### [blocking] I4. ENI deletability and pool-shrink guards

Blocking when: a change frees an ENI that is primary, trunk, EFA, younger than `minENILifeTime`, holding a cooling IP, or holding pods; or frees ENIs while the node is terminating or unschedulable without `AWS_MANAGE_ENIS_NON_SCHEDULABLE`.

**Rule.** All ENI-free decisions go through `getDeletableENI` (via `RemoveUnusedENIFromStore`), and `tryFreeENI`/`increaseDatastorePool` keep the `isTerminating` / non-schedulable gates. Scale-down runs at most every `decreaseIPPoolInterval` (30 s).

**Why.** Freeing a trunk or EFA ENI breaks SGP or EFA pods the CNI did not create; freeing during IP cooldown lets the IP land on another node while conntrack/NP state for the old pod still exists (inferred); freeing a just-attached ENI causes allocate/free churn and EC2 throttling.

**Cites.** `pkg/ipamd/datastore/data_store.go:38` (`minENILifeTime` 1 min), `:41`, `:243-255` (`IP_COOLDOWN_PERIOD`, default 30 s), `:1042-1094` (`getDeletableENI`), `:1144-1173`; `pkg/ipamd/ipamd.go:875-886`, `:981-992`, `:851-861`, `:64`.

**Check.** Does a new free path call `RemoveUnusedENIFromStore` rather than picking an ENI itself?

<a id="i5"></a>
### [blocking] I5. Warm-pool env vars: precedence and per-card defaults

Blocking when: a change alters which of `WARM_IP_TARGET`/`MINIMUM_IP_TARGET`/`WARM_ENI_TARGET`/`WARM_PREFIX_TARGET` wins, so an existing node either never grows its pool or allocates and frees in a loop.

**Rule.** Precedence: if `WARM_IP_TARGET` or `MINIMUM_IP_TARGET` is non-zero, they drive short/over and `WARM_ENI_TARGET`/`WARM_PREFIX_TARGET` are ignored; otherwise PD uses `WARM_PREFIX_TARGET` (prefix count) and secondary-IP mode uses `WARM_ENI_TARGET` (whole ENIs, default 1). With PD, short/over are converted to /28 counts. Network cards > 0 always use warm IP 1 / minimum IP 1. All targets are ignored in IPv6. Invalid or negative values fall back to defaults silently. Over-allocation is capped by `maxPods`.

**Why.** These rules are spread over five functions; changing one (for example the PD `over` math) without the others yields a pool that oscillates between `increaseDatastorePool` and `decreaseDatastorePool` every 5 s and drives EC2 calls ([T6](#themes)).

**Cites.** `pkg/ipamd/ipamd.go:74-114` (doc comments), `:1481-1514`, `:2086-2114` (parsers), `:1998-2000` (`warmIPTargetsDefined`), `:2234-2289` (`datastoreTargetState`, card > 0 defaults at `:2238-2241`, PD conversion `:2263-2286`), `:2692-2726` (`isDatastorePoolTooLow`, `maxPods` cap `:2705-2707`), `:2728-2746`, `:2748-2751` (`warmPrefixTargetDefined` is true whenever PD is on, since default is 0), `:2783-2800` (WARM_IP takes precedence over WARM_PREFIX), `:440-444` (ignored in IPv6), `:794-806` (IPv6 pool manager only self-heals).

**Check.** Do table tests cover each combination the change touches, with PD on and off and card 0 and card 1? Does the README warm-pool section still match?

<a id="i6"></a>
### [blocking] I6. ENI tags, description and the leaked-ENI sweeper are one contract

Blocking when: a change alters `eniDescriptionPrefix`, the `node.k8s.amazonaws.com/instance_id`, `node.k8s.amazonaws.com/createdAt`, `cluster.k8s.amazonaws.com/name` or `eks:eni:owner` keys or values, creates an ENI without `createENITags()`, or adds a new ipamd-created ENI type that the sweeper cannot see.

**Rule.** Every ENI ipamd creates carries description `aws-K8S-<instance-id>` and tags from `buildENITags()` plus `createdAt`, set at create time. The hourly sweeper deletes only `available` ENIs in this VPC that have the `instance_id` tag key, the description prefix, the cluster tag when `CLUSTER_NAME` is set, and a `createdAt` older than 5 minutes; an ENI without `createdAt` is tagged now and spared once. `ADDITIONAL_ENI_TAGS` cannot use keys containing `k8s.amazonaws.com`.

**Why.** Create-then-attach is not atomic: a crash between `CreateNetworkInterface` and attach (or a failed `DeleteOnTermination` modify) leaves an `available` ENI holding subnet IPs. The sweeper is the only thing that reclaims it, so any change to the matching keys leaks ENIs per node restart until the subnet is exhausted. Changing the keys also breaks n-1, whose sweeper still filters on the old ones. The 5-minute `createdAt` guard protects ENIs another ipamd is attaching right now.

**Cites.** `pkg/awsutils/awsutils.go:64`, `:78-85`, `:94-95`, `:1164-1177`, `:1236`, `:1474-1489`, `:1056-1098` (delete on attach failure, `FreeENI` on modify failure), `:2503-2529`, `:2568-2645` (filters), `:486` (hourly), `:1963-1988` (reserved prefix); `pkg/ipamd/ipamd.go:2120-2127` (`disableLeakedENICleanup`: also off in IPv6, IMDS-only and provisioning-disabled modes).

**Check.** If the PR adds an ENI-creating path, does it go through `createENI` and is it covered by the sweeper? IPv6 mode now creates ENIs on network cards > 0 (`ipamd.go:1037-1068`) while the sweeper is off in IPv6, so a new IPv6 ENI path should state who reclaims a leaked ENI (inferred gap on master).

<a id="i7"></a>
### [blocking] I7. ENI selection filters every loop must apply

Blocking when: a loop that allocates to, frees from, re-tags, re-SGs or counts ENIs omits a filter its sibling loops apply ([T3](#themes)).

**Rule.** The filters are: unmanaged (`no_manage=true` tag; untagged ENIs when `MANAGE_UNTAGGED_ENI=false`; primary is always managed), unmanaged network card (HyperPod, EFA-only cards), EFA-only ENI, trunk (`IsTrunk`, never gets IPs, never freed), EFA (never freed), excluded-for-pod-IPs (subnet tag `kubernetes.io/role/cni=0`), and the primary ENI under custom networking (no pod IPs). Use the existing helpers (`filterUnmanagedENIs`, `GetAllocatableENIs`, `getDeletableENI`, `getFilteredENIs`) rather than a new loop. `hasRoomForEni` subtracts unmanaged, reserved, trunk-slot and excluded ENIs from `maxENI`.

**Why.** A missing trunk filter assigns secondary IPs to the trunk, which branch-ENI pods then cannot use; a missing unmanaged filter makes ipamd re-tag or free an ENI another system owns; a missing excluded filter hands out IPs from a subnet the user excluded; a wrong `hasRoomForEni` count either blocks growth forever or calls `AttachNetworkInterface` past the instance limit in a loop.

**Cites.** `pkg/ipamd/ipamd.go:266-297` (`setUnmanagedENIs`), `:2173-2231` (`filterUnmanagedENIs`), `:2656-2690` (`hasRoomForEni`), `:1244-1265` (self-heal applies trunk/excluded/custom-primary filters), `:2942-2965`; `pkg/ipamd/datastore/data_store.go:794-798` (assign skips excluded), `:1118-1142` (`GetAllocatableENIs`), `:1042-1094`; `pkg/awsutils/awsutils.go:1885-1891` (trunk/EFA by `InterfaceType`), `:652-680`.

**Check.** For each new ENI loop, list which of the filters above it applies, and why it skips any. Note `cleanupExcludedENI` passes `DefaultNetworkCardIndex` for every ENI (`ipamd.go:2554-2578`) although its caller iterates all cards (`:2580-2605`); copy the caller's `networkCard`, not that call.

<a id="i8"></a>
### [blocking] I8. Multi-NIC: one datastore and one checkpoint per managed card

Blocking when: new code assumes one datastore, calls `GetDataStore(n)` without handling a nil return for a skipped card, or applies card-0 env targets to cards > 0.

**Rule.** `DataStoreAccess.DataStores` holds one datastore per managed network card (skipped cards have none). Card 0 uses the env warm targets; cards > 0 use warm IP 1 / minimum IP 1 and only take multi-NIC pods. `AddNetwork` tries each card for single-IP pods and requires one IP per card for `RequiresMultiNICAttachment`; `DelNetwork` stops after `InterfacesCount` matches. Pool decisions are per card (`isDatastorePoolTooLow` returns a map).

**Why.** `GetDataStore` returns nil for an unmanaged card, so a direct method call panics ipamd and the node stops assigning IPs. A partial multi-NIC ADD leaves IPs on earlier cards assigned; it relies on kubelet's DEL after a failed ADD and on `InterfacesCount` in the checkpoint to free them all (inferred from `rpc_handler.go:230-252` and `:350-360`).

**Cites.** `pkg/ipamd/datastore/data_store.go:1728-1773`; `pkg/ipamd/ipamd.go:207-213` (card > 0 defaults), `:315-342` (`markUnmanagedNetworkCards`), `:2234-2241`, `:2692`, `:875-889`; `pkg/ipamd/rpc_handler.go:194-205`, `:213-252`, `:341-376`, `:469` (`MultiNICEnabled` sent to NP agent). [T3](#themes) (#3895).

**Check.** Is the change tested with `ENABLE_MULTI_NIC=true` on a multi-card instance type, or does the PR say why cards > 0 are unaffected?

<a id="i9"></a>
### [blocking] I9. IPv6 mode is a different pool, not a flag on the IPv4 pool

Blocking when: a change alters a path IPv6 shares (setupENI, reconcile, RPC handler, datastore assign) for an IPv4 reason without an IPv6 test, or removes an `enableIPv6` guard ([T13](#themes)).

**Rule.** IPv6 requires `ENABLE_PREFIX_DELEGATION=true`, forbids custom networking and dual stack, and is rejected at startup otherwise. Each managed ENI gets one /80 (extra prefixes are left attached and ignored). The IPv6 pool manager never grows or shrinks; it waits for the trunk when SGP is on and then only self-heals ENIs with no /80. Warm targets are ignored. Subnet exclusion does not mark ENIs excluded in IPv6; instead nodeInit drops excluded ENIs from the list.

**Why.** A v4 change that runs on v6 can try to allocate secondary IPv4 addresses to a v6-only node, or a removed guard can make the v6 pool shrink to zero with no growth path, so the node cannot assign IPs and does not self-correct.

**Cites.** `pkg/ipamd/ipamd.go:2824-2851` (`isConfigValid`), `:794-806` (v6 pool manager), `:1203-1242` (`assignIPv6Prefix`), `:1244-1265`, `:1359-1367` (setupENI v6), `:554-575` (v6 excluded primary), `:2956-2961` (no exclusion mark in v6), `:684-692` (v6 secondary ENIs on cards > 0); `pkg/ipamd/rpc_handler.go:139-143`, `:277-285`. Suite: `test/integration/ipv6`.

**Check.** Did the change run `test/integration/ipv6` or explain why no shared path changed?

<a id="i10"></a>
### [blocking] I10. Security groups for pods: ipamd reads, the VPC Resource Controller allocates

Blocking when: ipamd starts creating, attaching or freeing branch ENIs, stores branch IPs in the datastore, drops the pod-UID check on DEL, or reports SGP enabled in CNINode without a free ENI slot for the trunk.

**Rule.** With `ENABLE_POD_ENI=true`, ipamd only (a) adds the `SecurityGroupsForPods` feature to this node's CNINode when a trunk slot is free, (b) reserves that slot in `hasRoomForEni`, (c) on ADD for a pod with a `vpc.amazonaws.com/pod-eni*` limit, returns the branch IP/VLAN/MAC from the `vpc.amazonaws.com/pod-eni` annotation and fails if the trunk or annotation is missing, and (d) on DEL for an unknown key, re-reads the pod, rejects a UID mismatch, and returns the VLAN from the annotation. Branch pods never touch the datastore; the trunk ENI is in the datastore only as `IsTrunk`.

**Why.** The controller owns branch ENI lifecycle; a second owner leaks or double-frees branch ENIs. The UID check stops a DEL for an old sandbox from tearing down the VLAN of a newer pod with the same name. Patching CNINode with no trunk slot makes the controller try to attach a trunk that cannot fit (inferred).

**Cites.** `pkg/ipamd/rpc_handler.go:101-187`, `:384-421`; `pkg/ipamd/ipamd.go:1524-1539`, `:2661-2664`, `:822-826`, `:2853-2891` (CNINode patch with optimistic lock); `pkg/ipamd/datastore/data_store.go:949-959`. [T14](#themes). Suites: `test/integration/pod-eni`, `custom-networking-sgpp`.

**Check.** If the change touches the SGP branch, did it run `pod-eni` on both trunk-compatible and incompatible node groups (#3250, [T2](#themes))?

<a id="i11"></a>
### [blocking] I11. Plugin-ipamd gRPC: strict version match, regenerated proto, append-only fields

Blocking when: a PR hand-edits `rpc/rpc.pb.go`/`rpc_grpc.pb.go`, renumbers or reuses a proto field, relaxes `validateVersion` without a plan, or adds a plugin-side setting that only ipamd's environment has.

**Rule.** Both binaries get the same version via `-X` ldflags; ipamd rejects ADD/DEL from any other `ClientVersion` with `FailedPrecondition`. Change `rpc/rpc.proto`, use the field number in its `// next field: N` comment and bump the comment, then run `make generate` (protoc + mockgen via `go:generate`). New request fields must have a safe zero value, because during a rolling upgrade the version check, not the proto, is what blocks mixed pairs (inferred).

**Why.** The host `aws-cni` binary is copied by the init container and can briefly differ from the running ipamd; the version check turns that into a retried CNI error instead of a silently misread field. A hand-edited `.pb.go` drifts from the `.proto` and the next `make generate` reverts it.

**Cites.** `pkg/ipamd/rpc_handler.go:82-87`, `:316-321`, `:329-333`; `cmd/routed-eni-cni-plugin/cni.go:58`, `:202`, `:313`, `:439`; `Makefile:92`, `:378-380`; `rpc/generate_mocks.go` (`go:generate protoc ...`); `rpc/rpc.proto:24`, `:49`, `:62`, `:74`. [T7](#themes) (#3879, #3304).

**Check.** Does `git diff rpc/` show `.proto` and generated files changing together, with the `next field` comment bumped?

<a id="i12"></a>
### [blocking] I12. nodeInit: what may fail startup and what must only log

Blocking when: a new step in `New`/`nodeInit` returns an error for a condition that a restart will not fix (aws-node crash-loops, node stays NotReady), or a step that leaves the node unable to route pod IPs only logs.

**Rule.** Order is fixed: limits, host network, (API server) maxPods and HyperPod, `DescribeAllENIs`, unmanaged ENIs and cards, datastores, tag + `setupENI` each ENI (5 retries, then continue), `ReadAllDataStores`, exclusion cleanup, PD-toggle cleanup, IP rules, SG refresh, CNINode features, SGP, then pre-scale. Precedents: ENI allocation failure never fails init ("should not cause node to be NotReady"); custom networking with an empty pool does; `ReadBackingStore` errors do. Checkpoint restore must run after every ENI is in the datastore, since restore drops allocations whose IP is in no known CIDR.

**Why.** Restoring before an ENI is set up silently frees its pods' IPs, and they are then handed to new pods (duplicate IPs). Failing init on a transient EC2 error is acceptable (the pod restarts); failing on a persistent config condition makes the node unusable until someone edits the DaemonSet.

**Cites.** `pkg/ipamd/ipamd.go:416-475` (`New`), `:477-696` (`nodeInit`; ENI loop `:577-611`, restore `:613-616`, PD toggle `:621-631`), `:698-717` (`handlePreScaling`, custom-networking fatal at `:709-713`), `:1025-1029`, `:2824-2851`; `pkg/ipamd/datastore/data_store.go:377-410` (restore matches IPs to known CIDRs); `cmd/aws-k8s-agent/main.go:148-158`.

**Check.** For each new `return err` in init, name the condition and whether a restart clears it.

<a id="i13"></a>
### [advisory] I13. Pool manager and RPC handlers run concurrently; only the datastore is locked

Blocking only if: new shared state on `IPAMContext` is written from both the pool-manager goroutine and an RPC handler without synchronization, and a race can hand out or free the wrong address.

**Rule.** One goroutine runs `StartNodeIPPoolManager` (5 s loop: grow/shrink, then `nodeIPPoolReconcile` at most every 60 s after the last pool action). gRPC runs each ADD/DEL in its own goroutine. `DataStore` methods take `ds.lock`; `IPAMContext` fields (`primaryIP`, `lastNodeIPPoolAction`, `lastInsufficientCidrError`) are unguarded today, and `reconcileCooldownCache` has its own mutex. Keep EC2 calls in the pool manager; the only RPC-path EC2 call is the PD-mode mismatch release in `DelNetwork`. `AddNetwork` already reads VPC CIDRs from IMDS on every ADD (`rpc_handler.go:263`, `:278`), and IMDS is throttled per instance (inferred), so do not add more per-ADD IMDS reads.

**Why.** An unlocked map written from both sides panics with "concurrent map writes" and restarts aws-node (inferred). EC2 calls in ADD multiply by pod churn and node count ([T6](#themes), #3873).

**Cites.** `pkg/ipamd/ipamd.go:794-820`, `:822-849`, `:1627-1634`, `:221-259` (fields; `primaryIP` at `:241`), `:345-377`; `pkg/ipamd/rpc_handler.go:369-382` (DEL-path `tryUnassignIPFromENI`/`tryUnassignPrefixFromENI`); `cmd/aws-k8s-agent/main.go:168`, `:184`.

**Check.** Does a new field have one writer goroutine, or a lock? Does the PR add an EC2 or apiserver call to `AddNetwork`/`DelNetwork`?

<a id="i14"></a>
### [advisory] I14. EC2 calls: one session, nested retries, metrics and IAM docs per call

Blocking only if: a new EC2 call runs per pod or per reconcile pass with no cache, or retries without backoff ([T6](#themes)).

**Rule.** Build EC2 clients from `awssession.New` (HTTP timeout from `HTTP_TIMEOUT`, minimum 10 s; SDK standard retryer with 10 attempts; `AWS_EC2_ENDPOINT` override; user agent with `VPC_CNI_VERSION`). ENI-level operations wrap that in `retry.NWithBackoff` (up to 12 tries, max 1 min backoff), so a call can take minutes; do not add a third retry layer. Add the method to `pkg/ec2wrapper` `EC2`, regenerate mocks, and record `Ec2ApiReq`, `AwsAPILatency`, `awsAPIErrInc`/`Ec2ApiErr` and `checkAPIErrorAndBroadcastEvent` like the siblings. A new EC2 action also goes into `docs/iam-policy.md` for each IP family that reaches it; the managed `AmazonEKS_CNI_Policy` is outside this repo, so a new action is a breaking change for users of that policy (inferred).

**Why.** Every node calls EC2 with the account's shared throttle; `InsufficientCidrBlocks`/`InsufficientFreeAddressesInSubnet` already back off 120 s so a full subnet does not burn the quota.

**Cites.** `pkg/awsutils/awssession/session.go:32-37`, `:52-63`, `:66-98`; `pkg/awsutils/awsutils.go:62-63`, `:443-470`, `:1082-1094` (metrics pattern), `:2774-2784`, `:1508`, `:1563`, `:1638`; `pkg/ec2wrapper/client.go:24-41`; `pkg/ipamd/ipamd.go:173`, `:386-397`, `:1003-1015`; `docs/iam-policy.md:10-30`, `:52-75` (IPv6 policy lists no `CreateNetworkInterface`/`AttachNetworkInterface`, though IPv6 now creates ENIs on cards > 0, `ipamd.go:1037-1068`; inferred doc gap).

**Check.** Count calls per pod and per 5 s loop before and after; ask for scale-test numbers when the count rises.

<a id="i15"></a>
### [blocking] I15. Custom networking: ENIConfig selects subnet and SGs for every new ENI

Blocking when: a change creates an ENI under `AWS_VPC_K8S_CNI_CUSTOM_NETWORK_CFG=true` without the ENIConfig subnet, assigns pod IPs from the primary ENI, or rewrites ENIConfig-created ENIs' SGs with the node's primary SGs.

**Rule.** The ENIConfig name comes from node label `vpc.amazonaws.com/externalEniConfig`, then the `ENI_CONFIG_ANNOTATION_DEF` annotation, then the `ENI_CONFIG_LABEL_DEF` label (both default `k8s.amazonaws.com/eniConfig`), then `default`. `tryAllocateENI` reads it on each allocation; an ENIConfig with no SGs falls back to primary SGs (warned). The primary ENI never gets pod IPs (`skipPrimary`), `RefreshSGIDs` does not propagate primary SG changes, and with SGP the CNINode gets the `CustomNetworking` feature.

**Why.** A missed subnet puts pods in the node subnet, which is often the exhausted one that custom networking was meant to avoid; overwriting SGs widens pod access ([T14](#themes)). Enabling or disabling custom networking requires a new node, so state written for one mode is never read by the other (code comment at `ipamd.go:656-658`).

**Cites.** `pkg/eniconfig/eniconfig.go:33-49`, `:72-95`, `:125-142`; `pkg/ipamd/ipamd.go:1076-1091`, `:1155`, `:1271`, `:653-675`, `:709-713`; `pkg/awsutils/awsutils.go:1256-1262`, `:1444-1455`, `:750-778` (`!cache.useCustomNetworking` guard at `:760`). Suites: `test/integration/custom-networking`, `custom-networking-sgpp`.

**Check.** Does the ENI loop in the diff apply `c.useCustomNetworking && eni.IsPrimary` the way `GetAllocatableENIs` does?

<a id="i16"></a>
### [advisory] I16. `pkg/vpc` limits and `misc/eni-max-pods.txt` are generated

Blocking only if: the PR hand-edits `pkg/vpc/vpc_ip_resource_limit.go` or `misc/eni-max-pods.txt` with values that overstate ENI or IP limits, since `hasRoomForEni` and `maxPods` then attempt attachments EC2 rejects on every pool tick.

**Rule.** Run `make generate-limits` (`scripts/gen_vpc_ip_limits.go`, then `gofmt`). Missing or wrong API values go in `addManualLimits` in the generator. A weekly workflow opens the regeneration PR. Unknown instance types fall back to `DescribeInstanceTypes` at startup, so a missing type is not an outage.

**Why.** The file header says `DO NOT EDIT`; the next generator run reverts hand edits silently.

**Cites.** `pkg/vpc/vpc_ip_resource_limit.go:14`; `scripts/gen_vpc_ip_limits.go:37-38`, `:65-66`, `:198-202`, `:566-574`; `Makefile:382-387`; `.github/workflows/generate-limits.yaml:35-41`; `pkg/awsutils/awsutils.go:2107-2148`; precedent commit `dd477e86` ("add trn3.48xlarge to manual VPC IP limits").

**Check.** Does `git diff` touch the generated file without touching the generator?

<a id="i17"></a>
### [advisory] I17. A new ipamd env var touches five places, and the chart default may differ from the code default

Blocking only if: a renamed or removed env var silently changes behavior on upgrade for users who set the old name, or a new default changes pool size on existing nodes.

**Rule.** Define the name as an `env...` constant next to its siblings with a doc comment, parse it once in `New` (or a helper next to `getWarmIPTarget`) with `utils.GetBoolAsStringEnvVar`/`GetIntFromStringEnvVar`, and treat invalid input as the default with a log line. Combinations that must stop the node belong in the init container's `validateEnvVars` (`cmd/aws-vpc-cni/main.go:337-389`; precedent: PD with every warm/minimum target at 0 is rejected at `:378-387`), which fails before ipamd starts. Then: README `#### \`NAME\` (vX.Y.Z+)` with Type and Default; `charts/aws-vpc-cni/values.yaml` `env:` only if the chart should set it (every key there is rendered into aws-node); never edit `config/master/*.yaml` (generated by `make generate-cni-yaml` / `config-folder-sync`). If ipamd reads it but the plugin also needs it, it must travel through the conflist ([T7](#themes)). Prefer changing a default over adding a variable ([T4](#themes), #3873).

**Why.** The chart pins values that differ from the binary: chart `WARM_PREFIX_TARGET: "1"` vs code default 0, and the chart always sets `WARM_ENI_TARGET: "1"`. A code-default change does not reach chart installs, and a chart change reaches every node on upgrade (inferred).

**Cites.** `pkg/ipamd/ipamd.go:60-213` (constants), `:416-461`, `:2068-2077`, `:2341-2349` (`GetConfigForDebug` exposes only five); `charts/aws-vpc-cni/values.yaml:74-99`; `charts/aws-vpc-cni/templates/daemonset.yaml:89-100`; `README.md:715-724` (format example); `Makefile:399-403`. [T4](#themes) (#3428, #3254).

**Check.** Is the variable in README and, if user-facing, in `values.yaml`? Did `config/` change only through the generator?

<a id="i18"></a>
### [blocking] I18. Prefix delegation: one effective flag, /28 only, toggle cleanup at startup

Blocking when: code re-reads `ENABLE_PREFIX_DELEGATION` after `New`, assigns from a CIDR whose kind (prefix vs /32) does not match the effective mode, releases a /28 with any assigned address, or drops the startup cleanup that releases the other kind's unused CIDRs.

**Rule.** The env is read once into `c.enablePrefixDelegation`; `isConfigValid` turns it off on non-Nitro instances; only then is it copied to awsutils (`InitCachedPrefixDelegation`) and the datastores (`InitializeDataStores`). Use those copies. Prefixes are /28 (16 IPs, `GetPrefixDelegationDefaults`). The datastore assigns only from CIDRs matching the mode and leaves the others for cleanup: at init, PD on releases free secondary IPs, PD off releases free prefixes; DEL releases a mismatched CIDR as soon as it empties. A prefix is freeable only when no address in it is assigned.

**Why.** Reading the env again on a non-Nitro node disagrees with the datastore and allocates prefixes EC2 rejects on every tick. Assigning from a mismatched CIDR after a toggle keeps the old CIDRs alive forever, so the node holds both kinds and exhausts the subnet (inferred).

**Cites.** `pkg/ipamd/ipamd.go:428`, `:2152-2154`, `:2840-2848`, `:467` (`InitCachedPrefixDelegation`), `:550-552` (`InitializeDataStores` with PD), `:621-631`, `:2626-2640` (`GetIPv4Limit`), `:1267-1315`; `pkg/ipamd/datastore/data_store.go:805-823`, `:1345-1372`, `:1552-1558`; `pkg/ipamd/rpc_handler.go:369-382`. Docs: `docs/prefix-and-ip-target.md`.

**Check.** Is the change tested with PD toggled on an existing node (secondary IPs present, then PD on; and the reverse)?

<a id="i19"></a>
### [blocking] I19. Kubernetes access: optional at startup, cached per node, RBAC in the chart

Blocking when: ipamd `Get`s a new resource type (it starts an unfiltered, cluster-wide informer and needs list/watch RBAC), adds a new CRD read without RBAC in `charts/aws-vpc-cni/templates/clusterrole.yaml`, or uses `k8sClient` on a path that runs when `withApiServer` is false.

**Rule.** ipamd waits for the API server only when SGP, custom networking or `ANNOTATE_POD_IP` is on; otherwise it starts without it (maxPods from `eni-max-pods.txt`) and switches over in `SetAPIServerConnectivity`. The controller-runtime client reads through a cache filtered to this node for Pods, Node and (only with SGP) CNINode; any other type `Get` through that client is cached cluster-wide (inferred from controller-runtime behavior). Add a `ByObject` field selector for any per-node type. RBAC is conditional where the need is: pod `patch` only when `env.ANNOTATE_POD_IP` is set. The ENIConfig CRD is shipped from `charts/aws-vpc-cni/crds/`; CNINode is owned by the VPC Resource Controller.

**Why.** A cluster-wide pod informer per node multiplies apiserver load by node count. Missing RBAC makes `Get` fail forever and, in nodeInit, keeps the node NotReady.

**Cites.** `cmd/aws-k8s-agent/main.go:108-133`, `:162-165`; `pkg/ipamd/ipamd.go:507-527`, `:2893-2939`, `:2386-2399`; `pkg/k8sapi/k8sutils.go:39-61`, `:126-160`; `charts/aws-vpc-cni/templates/clusterrole.yaml:8-52` (conditional `patch` at `:17-27`); `charts/aws-vpc-cni/crds/customresourcedefinition.yaml:4`.

**Check.** For each new `k8sClient` call: which type, is it in the cache filter, which RBAC rule allows it, and what happens before the API server is reachable?

<a id="i20"></a>
### [advisory] I20. Metrics: names are an API, labels stay bounded

Blocking only if: a change renames or removes an `awscni_*` metric or label that `cni-metrics-helper` reads, since CloudWatch dashboards and alarms then go silent.

**Rule.** Metrics live in `utils/prometheusmetrics` and are registered once in `PrometheusRegister`. Reuse `IpamdErr` (`awscni_ipamd_error_count{fn}`) via `ipamdErrInc("<fn>")`, `PodENIErr` via `podENIErrInc`, `ReconcileCnt{fn}`, and the EC2 trio (I14) before adding a metric. A metric meant for CloudWatch also needs an entry in `InterestingCNIMetrics`. Labels are function names, API names, ENI IDs or CIDRs; never pod names or IPs. Per-ENI labels are deleted when the ENI leaves (`EniIPsInUse.DeleteLabelValues`). The endpoint listens on `:61678` on the host network.

**Why.** Pod-level labels grow without bound on a churning node (inferred). Missing `DeleteLabelValues` leaves a series per freed ENI forever.

**Cites.** `utils/prometheusmetrics/prometheusmetrics.go:30-218`, `:221-246`, `:248-279`; `cmd/cni-metrics-helper/metrics/cni_metrics.go:31-164`; `pkg/ipamd/ipamd.go:1587-1594`; `pkg/ipamd/datastore/data_store.go:1166-1171`; `cmd/aws-k8s-agent/main.go:38`, `:170-173`.

**Check.** Is the new label set bounded by ENIs or functions on the node? Is a renamed metric also renamed in `cni_metrics.go`?

<a id="i21"></a>
### [advisory] I21. Introspection endpoint: loopback, read-only, JSON shape used by support tooling

Blocking only if: the default bind leaves loopback, or a handler mutates state.

**Rule.** `INTROSPECTION_BIND_ADDRESS` defaults to `127.0.0.1:61679` (a `unix:` path is allowed); `DISABLE_INTROSPECTION` turns it off. Handlers under `/v1/` only marshal state (`/v1/enis` returns per-card `ENIInfos`, `/v1/ipamd-env-settings` returns `GetConfigForDebug`). Exported fields of `datastore.ENI`/`CidrInfo` are the response schema, so renaming them changes the output that `docs/troubleshooting.md` tells users to read.

**Why.** aws-node runs with `hostNetwork: true`; a non-loopback default exposes pod-to-IP mapping to the node's network.

**Cites.** `pkg/ipamd/introspect.go:32-36`, `:54-75`, `:77-123`, `:125-206`; `pkg/ipamd/datastore/data_store.go:112-113` ("Exported fields will be marshaled for introspection"); `charts/aws-vpc-cni/templates/daemonset.yaml:43`; `README.md:408-425` (`aws-cni-support.sh` reads it).

**Check.** Does a new handler only read? Is a new exported datastore field safe to show?

<a id="i22"></a>
### [advisory] I22. Logging: package logger, lazy in code the plugin imports, levels by frequency

Blocking only if: a package imported by the `aws-cni` plugin binary builds the logger at package init (`var log = logger.Get()`), which creates the ipamd log file from the plugin and fails on read-only roots (#3893).

**Rule.** ipamd packages use the shared `logger.Get()`; packages the plugin also links (`pkg/awsutils/awssession`, `pkg/ipamd/datastore` via `datastore.ErrUnknownPod`, utilities) use `logger.Lazy()` or a logger passed in, as the datastore does. Default level is Debug and the chart sets `DEBUG`, so Debug lines in the 5 s loop or per pod are written by default. Use Info for per-pod assignments (existing precedent), Warn/Error for actionable conditions. The logger is a zap sugared logger: `Errorf("msg", err)` without a verb prints `%!(EXTRA ...)`, and `%w` is not supported. Prefer a node or pod event (`eventrecorder`) for user-actionable errors, as `MissingIAMPermissions` and `SubnetExclusionCheckFailed` do ([T10](#themes)).

**Why.** ipamd logs to a 100 MB × 5 rotated file on the host; per-pod Debug lines at scale rotate away the lines needed to debug an incident (inferred), and a user rarely reads ipamd.log, but does see events.

**Cites.** `pkg/utils/logger/logger.go:48-77`, `pkg/utils/logger/config.go:20-58`, `pkg/utils/logger/zaplogger.go:52-62`, `:154-163` (lumberjack 100 MB × 5); `pkg/awsutils/awssession/session.go:46-50`; `pkg/ipamd/datastore/data_store.go:313` (logger injected); `pkg/ipamd/ipamd.go:216`; `cmd/routed-eni-cni-plugin/cni.go:451`; `charts/aws-vpc-cni/values.yaml:80-81`; `pkg/awsutils/awsutils.go:2774-2784`; `pkg/ipamd/ipamd.go:1345-1351`. Existing misuse to not copy: `ipamd.go:673`, `:1536`, `:666`.

**Check.** Does the new log line run per pod or per loop at a level that ships by default?

<a id="datapath"></a>
## 4. CNI plugin and host datapath

These cover the `aws-cni` and `egress-cni` plugins, the aws-node entrypoint and init container, and host rules in `pkg/networkutils`. Blocking bar: a pod gets no or wrong connectivity, host routes, rules or iptables/nft state leak or do not self-correct, or an upgrade or rollback leaves nodes broken.

<a id="d1"></a>
### [blocking] D1. CNI DEL must succeed without ipamd and must not fail on missing state

Blocking when: a DEL path can return an error for state that is already gone, or stops tearing down host rules when ipamd is unreachable or rejects the call.

**Rule.** DEL returns nil when the pod is unknown, and when ipamd cannot be reached or returns an error it tears down from `prevResult` and still returns nil. Every new teardown step tolerates "not found".

**Why.** Kubelet does not guarantee a DEL retry, and a DEL error blocks the runtime from cleaning the sandbox (`cni.go:422-424`, `:460-461`). Without the `prevResult` fallback, host rules at priorities 512/1536 stay for an IP that ipamd later hands to another pod. The unix-socket `Dial` is lazy (no `WithBlock`, `cni.go:139-146`), so an ipamd outage usually surfaces at `DelNetwork`, i.e. the `:458-470` path, not the dial path (`:416-433`) *(inferred from grpc-go Dial semantics)*. A version mismatch is also an error here (`rpc_handler.go:316-321`, `:330-333`), so it too falls back to `prevResult`.

**Cites.** `cmd/routed-eni-cni-plugin/cni.go:403-412` (branch-ENI DEL from prevResult first), `:416-433`, `:451-457` (`ErrUnknownPod` → nil), `:458-470`, `:611-683` (`teardownPodNetworkWithPrevResult`); `driver/driver.go:706-743` (`ContainsNoSuchRule`, `IsNotExistsError` tolerated); `pkg/networkutils/network.go:735-745` (`NetLinkRuleDelAll` loops until "no such rule").

**Check.** For each new error return in `del()`, ask whether a second DEL of the same container would also fail. Unit-test DEL with `DelNetwork` returning a transport error and assert `TeardownPodNetwork` is called and `del` returns nil (`cni_test.go`). Non-`CANARY` `cni/host_networking_test.go:57` checks rule cleanup on a cluster but no workflow runs it.

<a id="d2"></a>
### [blocking] D2. `prevResult` interface encoding is a cross-version contract

Blocking when: a change alters what ADD writes into `Interfaces` (names, order, count, `Mac`/`Sandbox` meaning) or what DEL parses, without keeping the older encodings readable.

**Rule.** Keep the layout: per IP a host veth and a container veth (container `Mac` = route table ID), then one dummy interface `dummy<hash>` whose `Mac` is the VLAN ID (0 for non-branch) and whose `Sandbox` is the NC-0 device number. DEL must still handle pods created by older versions (no container `Mac`, single interface).

**Why.** DEL without ipamd reconstructs everything from this: interface count `(len-1)/2` (`cni.go:647`), route table from container `Mac` or from the dummy's device number (`:645`, `:660-666`), SGP detection from dummy `Mac != 0` (`:577-588`). The dummy entry is kept "for supporting downgrade to v1.19 and below" (`:299-302`). A rollback runs the old DEL against results the new ADD wrote, so a changed shape leaks rules on rollback.

**Cites.** `cni.go:266-272`, `:286-303`, `:337-340`, `:564-608`, `:611-683`; route table math `pkg/networkutils/helper.go:18-26`.

**Check.** `git diff` on `cni.go` around `podInterfaces`/`dummyInterface`. A unit test should feed DEL a `prevResult` in the old shape (no container `Mac`) and the new shape. State n-1 behavior in the PR (PR template `:44`).

<a id="d3"></a>
### [blocking] D3. The plugin reads its settings only from the conflist

Blocking when: code under `cmd/routed-eni-cni-plugin` or `cmd/egress-cni-plugin` reads an env var, or a new plugin setting is added to `NetConf` without a placeholder and a renderer.

**Rule.** `aws-cni` and `egress-cni` are execed by the container runtime on the host and see none of the DaemonSet env. A new plugin knob needs all three: a field on the plugin `NetConf` (`cni.go:60-81`), a `__PLACEHOLDER__` in `misc/10-aws.conflist`, and a `strings.Replace` in `generateJSON` (`cmd/aws-vpc-cni/main.go:262-284`) or a chained plugin there (`:288-326`).

**Why.** An env read in the plugin returns the default on every node, silently. `LoadNetConf` also applies defaults (`MTU 9001`, `eni`, `strict`, `cni.go:109-116`), so a missing key does not error. The rendered file is replaced only when aws-node restarts, so env changes need a rollout.

**Cites.** `misc/10-aws.conflist:1-38`; `cmd/aws-vpc-cni/main.go:122-156` (`NetConf` the renderer writes), `:454-465`; `cni.go:109-137`; chart override `charts/aws-vpc-cni/templates/daemonset.yaml:123-129` (`cniConfig.enabled` mounts a custom conflist over `/app/10-aws.conflist`, which bypasses placeholders the user's file lacks *(inferred)*).

**Check.** `grep -n 'Getenv\|GetEnv\|LookupEnv' cmd/routed-eni-cni-plugin cmd/egress-cni-plugin -r` must stay empty outside tests. A new key needs a `cmd/aws-vpc-cni/main_test.go` case.

<a id="d4"></a>
### [advisory] D4. CHECK is a no-op

**Rule.** Do not rely on CHECK to repair or verify anything. Both plugins pass `nil` for `cmdCheck` (`cni.go:693`, `egress-cni-plugin/main.go:38`), the conflist sets `"disableCheck": true` (`misc/10-aws.conflist:4`), and skel returns success for a nil handler (containernetworking/cni v1.3.1 `pkg/skel/skel.go:200-201`).

**Why.** A fix that assumes "the runtime will CHECK and re-ADD" never runs. Implementing CHECK would also need `disableCheck` flipped in the rendered config, which changes behavior on every node at once.

**Check.** A PR adding CHECK must change `misc/10-aws.conflist`, explain kubelet/containerd behavior on CHECK failure, and run the cni suite.

<a id="d5"></a>
### [blocking] D5. A failed ADD must undo the host state it created

Blocking when: a new host-side object (rule, route, link, iptables/nft entry) is created in ADD before a step that can fail, with no removal on that failure path.

**Rule.** On `SetupPodNetwork`/`SetupBranchENIPodNetwork` failure, ADD releases the IP (`DelNetwork` with `Reason: SetupNSFailed`, `cni.go:306-328`) and returns. It does not tear down host rules. Undo new host state inside the driver on error, or make it reclaimable by the next ADD the way `claimHostVethName` reclaims the veth name.

**Why.** After the release, the runtime's follow-up DEL finds no `prevResult` and ipamd answers `ErrUnknownPod`, so `del` returns at `cni.go:451-457` before any teardown. A to-container rule (`driver.go:682-690`) or from-pod rule (`:692-701`) added before a later failure stays, and the IP can be given to the next pod *(inferred: traced through both paths; not seen in a test)*. Reviewers also asked for validation before veth creation ([T16](#themes)).

**Cites.** `driver/driver.go:297-321`, `:360-411`; `cni.go:304-329`.

**Check.** For each new `RuleAdd`/`RouteAdd`/`LinkAdd` in the driver, find the error returns after it and the matching delete. A driver unit test that fails the step after the add and asserts the delete was called.

<a id="d6"></a>
### [blocking] D6. Never delete a host veth you cannot prove belongs to this sandbox

Blocking when: a change deletes or renames an existing host link by name without the alias stamp or the veth-peer check, or changes the alias format or veth naming hash.

**Rule.** Host veth names are `prefix + sha1(ns.name)[:11]` (`pkg/networkutils/names.go:12-25`). `claimHostVethName` deletes an existing link only when its alias is `aws-vpc-cni:` + full SHA-256 of `namespace/name`, or, unstamped, when its veth peer is in this netns (`driver.go:520-555`, `:567-595`). ADD also claims the same name under the other mode's prefix (`vlan` for regular pods, the normal prefix for branch pods), so a pod moved between modes does not keep a stale link (`cni.go:247-255`, `driver.go:557-562`).

**Why.** Names collide across pods because the hash is truncated; deleting another pod's veth cuts its connectivity. The stamp is full SHA-256 because the 11-hex name hash can be ground (`driver.go:508-516`). Changing the alias format makes every existing link "unstamped" and pushes all reclaims to the peer check (`#3924`, `CHANGELOG.md` v1.23.3).

**Cites.** `driver.go:471-504` (stamp after create), `:506-516`, `:520-595`.

**Check.** `grep -n LinkDel cmd/routed-eni-cni-plugin/driver/driver.go`: every hit must be the claim path or a vlan link keyed by VLAN ID. Unit tests exist for stamped-other-pod refusal in `driver_test.go`.

<a id="d7"></a>
### [blocking] D7. Rule priorities and route-table ID spaces are fixed

Blocking when: a change adds or moves a rule priority, or computes a route table ID, without checking it against every range below and against what n-1 deletes.

**Rule.** Priorities (`pkg/networkutils/network.go:53-80`): 0 local (moved to 20 in SGP strict), 1 from-interface (multi-IP, inside the pod netns, `driver.go:244-254`), 10 VLAN IIF rules, 512 to-pod → main, 1024 connmark → main, 1535 external service CIDRs, 1536 from-pod → ENI table, 32765 from primary IP of secondary ENI. Tables (`helper.go:18-26`): NC0 dev0 = main (254), NC0 devN = N+1, NC>0 = 10000 + dev + 100·NC; branch VLANs = VLAN ID + 100, 101..4194, rejecting 253-255 (`driver.go:345-356`).

**Why.** Rules are matched by priority + table + selector on teardown (`driver.go:706-743`, `:791-803`, `NetLinkRuleDelAll` deletes every match). A priority shared with another feature, or a table that overlaps another range, lets one teardown delete another feature's rule. Teardown treats table 1 as main because older versions computed primary as 1 (`driver.go:332-337`).

**Cites.** above, plus `network.go:336-355` (1024 rule deleted and re-added at start), `:1257`, `:1295`.

**Check.** A new constant needs a comment in `network.go:53-80` naming its range, and a unit test in `driver_test.go`/`network_test.go` asserting the exact `netlink.Rule` (priority, table, family, selector) passed to `RuleAdd` and `RuleDel`.

<a id="d8"></a>
### [blocking] D8. IPv6 pod setup: sysctls before link up, family on every rule

Blocking when: a v4 change touches `createVethPairContext.run`, `setupVlan` or a rule builder that v6 shares, or a v6 rule is created without `Family = AF_INET6`.

**Rule.** The host veth gets `accept_ra=0`, `accept_redirects=1`, `forwarding=0` before it is brought up (`driver.go:153-173`, `:445-469`); the VLAN link the same before `LinkSetUp` (`:614-642`). `ENOENT` from procsys is tolerated (kernel without IPv6). v6 enables `disable_ipv6=0` on the container veth and `lo` (`:187-201`), uses gateway `fe80::<index+1>` (`helper.go:32-34`), and waits for DAD (`driver.go:286-290`). IIF rules set `Family` explicitly (`:764-786`, `:791-797`).

**Why.** A host veth that is up while accepting RAs can take a pod-advertised route; that was the reason for the ordering. A rule without family defaults to v4 and the v6 rule never matches, so the pod has no route back *(inferred)*. IPv6 has no SNAT or connmark rules at all (`network.go:444-447`) and no automated cluster coverage ([section 5](#ci)).

**Cites.** above; host side `network.go:318-329`, `:378-383` (v6 gateway rule in strict mode).

**Check.** Run `test/integration/ipv6` (`scripts/run-ipv6-integration-tests.sh`) and paste output. Unit tests in `driver_test.go` should assert sysctl `Set` happens before `LinkSetUp` (gomock `InOrder`).

<a id="s1"></a>
### [blocking] S1. SGP strict and standard modes set up different host state

Blocking when: a change to the branch-ENI path handles one mode only, adds a third mode, or changes teardown so it no longer removes both modes' rules.

**Rule.** Strict: host veth prefix `vlan` (`pkg/sgpp/utils.go:12-21`), route to pod in the VLAN table, IIF rules at priority 10 for both the VLAN link and the host veth (`driver.go:749-789`), and the local-table rule moved from 0 to 20 at node setup (`network.go:357-384`). Standard: normal veth prefix and IP rules as for regular pods (`driver.go:406-410`). Teardown deletes both modes' rules, since the pod may have been set up under the other mode (`driver.go:432-439`).

**Why.** Strict keeps pod traffic on the branch ENI so the pod's SGs apply; the `vlan` prefix also takes these pods out of SNAT (`! -o vlan+`, `network.go:548`) and the `eni+` connmark rules (`:600`). The mode is read twice: ipamd from env (`network.go:218`, `sgpp/utils.go:24-34`, unknown → strict) and the plugin from the conflist. `aws-vpc-cni` validates it only when `ENABLE_POD_ENI` is true (`cmd/aws-vpc-cni/main.go:356-364`). The driver `switch` has no default (`driver.go:401-410`), so an unrecognized mode string sets no pod route or rules: no connectivity *(inferred)*. Moving from strict to standard does not move the local rule back to 0 (`network.go:357`, only in strict) *(inferred; impact not traced)*.

**Cites.** above; DEL with empty netns skips SGP teardown (`cni.go:499-502`, `:589-592`).

**Check.** Run `test/integration/pod-eni` in both modes (not in any workflow) and `custom-networking-sgpp` (runs on push). Any new mode-dependent code needs a `driver_test.go` case per mode.

<a id="n1"></a>
### [blocking] N1. SNAT and connmark rules converge from a desired list, and only at start or on VPC CIDR change

Blocking when: a change adds a rule outside the desired-list machinery, makes an "absent" rule depend on a failed read, or assumes the rules are reconciled periodically.

**Rule.** Build every rule as an `iptablesRule{shouldExist}` in `buildIptablesSNATRules` (`network.go:489-617`) so both enable and disable paths converge (`updateIptablesRules`, `:619-653`). Stale rules are found by listing every chain with the `AWS-SNAT-CHAIN` prefix (`:655-717`); chains numbered >0 are deleted once (`:387-430`). Disabling a feature (e.g. `AWS_VPC_K8S_CNI_EXTERNALSNAT=true`) must delete what the enabled state wrote: `shouldExist: !n.useExternalSNAT` (`:520`, `:539`) and `connmark.Cleanup()` (`:454-458`).

**Why.** `UpdateHostSNATRules` runs from `SetupHostNetwork` at init (`ipamd.go:494`) and from a 30 s loop that calls it only when the VPC CIDR set changed (`ipamd.go:637-640`, `:775-783`). A rule someone flushes is not restored until aws-node restarts. A list error inside `computeStaleIptablesRules` aborts the whole update (`:565-568`), which is the right direction; treating it as "no rules" would duplicate or delete live rules ([T1, T5](#themes)).

**Cites.** above; SNAT target `--random-fully` default with fallback to `--random` (`network.go:546-563`, `:836-858`).

**Check.** Unit test in `network_test.go` with a fake iptables holding stale rules and the flag flipped both ways. Cluster: `test/integration/snat` (no workflow) and the weekly mixed-OS SNAT run.

<a id="n2"></a>
### [blocking] N2. Connmark backend: iptables or nftables, chosen once, each cleans the other

Blocking when: a change to connmark rules works on one backend only, changes how existing nft rules are classified, or changes the nft table/chain/register layout without checking what v1.23.0/1.23.1 do on rollback.

**Rule.** `NewConnmark` picks the backend once from `iptables --version` inside aws-node (`nftables_connmark.go:82-103`, `pkg/iptableswrapper/iptables.go:122-146`; the image sets `iptables-wrapper`, `Dockerfile.release:28`). The nft backend owns `table ip aws-cni` with base chain `nat-prerouting` at priority -90 and chain `snat-mark` (`nftables_connmark.go:34-50`, layout `:117-139`). On first success each backend removes the other's leftovers: iptables removes the nft table (`iptables_connmark.go:82-110`), nft removes `AWS-CONNMARK-CHAIN-0` and the PREROUTING rules (`nftables_connmark.go:222`, `:477-492`).

**Why.** Restore rules use register 2 so that v1.23.0/1.23.1 reject them on rollback and rebuild the chain (`nftables_connmark.go:42-45`). Reconcile classifies existing rules by shape (`classifyRestoreRule`, `:626`); #3875 was a downgrade where the old classifier matched a new rule and deleted it ([T11](#themes)). The nft table is IPv4 only (`:478-480`). Mixed nodes (AL2023 nft, AL2023 legacy, Ubuntu) are only tested weekly (`weekly-cron-tests.yaml:16`).

**Cites.** above; kernel tests `pkg/networkutils/nftables_connmark_kernel_test.go`, run in PR CI under `unshare -n` (`pr-automated-tests.yaml:40-44`).

**Check.** For nft changes add a `TestNftKernel*` case and run it locally (`RUN_NFT_KERNEL_TESTS=1`, needs root + netns). State what the previous release does with the new rules on rollback.

<a id="e1"></a>
### [blocking] E1. egress-cni: chained, gated by `enabled`, and DEL depends on the pod's link

Blocking when: egress ADD can leave a host-local IP or SNAT chain without the matching DEL removing it, or the plugin starts running when `enabled` is not `"true"`.

**Rule.** The plugin must be chained (`PrevResult` required, `egress-cni-plugin/main.go:59-62`) and passes through the result when `enabled != "true"` (`:79-81`, `:124-127`). ADD releases the host-local IP on any later error (`:83-88`). Per-pod SNAT chains are named from network name + container ID with `E4-`/`E6-` (`:100`, `:104`, `:138`, `:143`); rules use `AppendUnique` (`snat/snat.go:50-90`).

**Why.** DEL finds the IP whose SNAT rule to delete by listing addresses on `v4if0`/`v6if0` inside the netns (`egressContext.go:329-367`). With an empty netns path or a link already gone, it deletes nothing and returns nil, so the `E4-`/`E6-` chain and POSTROUTING jump stay on the host *(inferred: no other DEL path removes them)*. The rules are written with the host's `iptables`/`ip6tables` binary, not aws-node's `iptables-wrapper` choice *(inferred from `iptableswrapper.NewIPTables` running in the host process)*. `enabled` comes from `ENABLE_V6_EGRESS` (v4 cluster) or `ENABLE_V4_EGRESS` (v6 cluster), and v6 egress is turned off with a warning when the node has no IPv6 (`cmd/aws-vpc-cni/main.go:231-261`).

**Cites.** above; subnets and data dirs `cmd/aws-vpc-cni/main.go:56-61`.

**Check.** `test/integration/cni-egress` (no workflow). A unit test for DEL with an empty `NsPath` should show what is left behind.

<a id="d9"></a>
### [blocking] D9. aws-vpc-cni-init sets node-wide sysctls and installs host binaries; nothing reverts them

Blocking when: a change adds a sysctl or a host file in the init container without saying what happens on rollback or when the flag is turned off.

**Rule.** The init container copies every file in its workdir except itself to `/host/opt/cni/bin` (`cmd/aws-vpc-cni-init/main.go:152-161`) and sets `rp_filter=2` on the primary ENI, `tcp_early_demux` (`:70-101`), and for IPv6/v6-egress `all/disable_ipv6=0`, `all/forwarding=1`, primary `accept_ra=2` (`:103-142`). Any failure exits 1 and blocks aws-node.

**Why.** IPv6 is never disabled again when the env is unset "to preserve default host semantics" (`:106`); the same holds for every sysctl here, so turning a flag off or rolling back leaves the node changed *(inferred)*. `rp_filter=2` is required for return traffic on secondary ENIs; strict-mode TCP liveness probes depend on `tcp_early_demux` (`test/integration/pod-eni/security_group_per_pod_test.go:217-307`). Copied files land on every node's CNI bin dir, so a new binary in `/init` is installed fleet-wide.

**Cites.** above; `scripts/dockerfiles/Dockerfile.init:12-25`.

**Check.** The PR states the sysctl's value after rollback. For a new file under `/init`, confirm it should be on the host.

<a id="d10"></a>
### [blocking] D10. aws-vpc-cni startup order: binaries, ipamd healthy, then conflist

Blocking when: a change writes `10-aws.conflist` before ipamd is serving, removes the health wait, or lets the plugin and ipamd come from different builds.

**Rule.** Order in `_main` (`cmd/aws-vpc-cni/main.go:418-475`): validate env, copy `aws-cni` and `egress-cni`, start ipamd, wait for gRPC health on the unix socket, render and copy the conflist, wait on ipamd. The conflist is kubelet's "network ready" signal (`:14-33`). Both binaries get `$(VERSION)` through `LDFLAGS` (`Makefile:92`) and ipamd rejects any other plugin version (`rpc_handler.go:316-321`).

**Why.** Writing the conflist early makes kubelet send ADDs that fail until ipamd is up. The binary is copied before ipamd starts, so during an upgrade the new plugin talks to whatever ipamd is up; a mismatch fails ADD (`FailedPrecondition`) and pushes DEL to the `prevResult` path (D1). `waitForIPAM` has no sleep and no bound (`:182-189`); only the liveness probe (`values.yaml:168-177`, 60 s initial delay) ends a hung start.

**Cites.** above; chart probes `charts/aws-vpc-cni/values.yaml:168-188`.

**Check.** `cmd/aws-vpc-cni/main_test.go` covers `generateJSON`; run `test/integration/cni-upgrade-downgrade` (no workflow) for anything that changes startup or version handling.

<a id="r1"></a>
### [blocking] R1. ipamd gRPC lives on a unix socket; the proto is shared with the plugin and the network policy agent

Blocking when: a change edits `rpc.pb.go` by hand, renumbers or removes a proto field, moves the socket or changes its mode, or makes ADD/DEL depend on the NP agent when `NETWORK_POLICY_ENFORCING_MODE` is unset.

**Rule.** ipamd serves `CNIBackend`, `ConfigServerBackend` and health on `/var/run/aws-node/ipamd.sock` mode 0660 (`rpc_handler.go:47-48`, `:495-538`). The NP agent serves `NPBackend` on `/var/run/aws-node/npa.sock` and reads node config through `GetNetworkPolicyConfigs` (`rpc/rpc.proto:78-81`, `:104-115`; `rpc_handler.go:464-488`). Change `rpc/rpc.proto` and run `go generate ./rpc` (`rpc/generate_mocks.go`). TCP `127.0.0.1:50051` is gone since `a4cfda20`.

**Why.** The plugin calls the NP agent only when ipamd returns a non-empty `NetworkPolicyMode` (`cni.go:344-347`, `:517-520`). ADD then fails if the agent does not answer within 2 s (`:348-353`, `:366-372`); DEL returns an error after the network is already torn down (`:524-528`), and the retried DEL ends at `ErrUnknownPod` before reaching the agent (`:451-457`) *(inferred)*. The socket path is a hostPath shared by both containers (`daemonset.yaml:132`, `:178`, `:203-204`). Metrics `:61678` listen on all interfaces of the host network (`cmd/aws-k8s-agent/main.go:38`, `daemonset.yaml:43`); introspection defaults to `127.0.0.1:61679` (`introspect.go:33`, `:109-111`). Do not widen either default.

**Cites.** above.

**Check.** `git diff --stat rpc/`: `rpc.proto` changes must come with regenerated `rpc.pb.go`, `rpc_grpc.pb.go` and `mocks/rpc_mocks.go`. Proto field numbers only added. Network-policy changes need a matching NP agent release named in the PR.

<a id="config"></a>
## 5. Config, tests and CI

<a id="ci"></a>
### What CI runs

| Workflow | Trigger | Runs |
|---|---|---|
| `pr-automated-tests.yaml` | PR to `master`, `release*`, `sdkv2*` (`:2-7`) | `check-format`, `vet`, `build-linux`, `build-test-binaries`, `helm-lint`, `unit-test` (`:28-39`); the nftables kernel tests (`TestNftKernel*`, under `sudo unshare -n` with `RUN_NFT_KERNEL_TESTS=1`, `:40-44`); `make docker` and `make docker-init` on amd64 and arm64 (`:49-94`). No cluster. |
| `deps.yml` | PR to `master`, `release*`, `sdkv2` | dependency review and `govulncheck` on `go.mod` and `test/agent/go.mod` (`:28-40`) |
| `integration-tests.yaml` | push to `master`, `release*`, `sdk*` (`:3-8`) | `scripts/run-integration-tests.sh` with `RUN_CONFORMANCE=true` (`:40-47`) |
| `pr-manual-tests.yaml` | `workflow_dispatch` with a PR number, run by a maintainer; checks out `refs/pull/<n>/merge` (`:4-10`, `:24`) | the same script against the PR |
| `nightly-cron-tests.yaml` | daily (`:3-6`) | the same script |
| `weekly-cron-tests.yaml` | Wednesday (performance, Bottlerocket) and Sunday (mixed OS) (`:14-16`) | performance and Bottlerocket runs (`:56-72`); `scripts/run-mixed-os-snat-kube-proxy-test.sh` (`:114-115`), which builds AL2023, AL2023 iptables-legacy and Ubuntu node groups and runs the `test SNAT with kube-proxy modes` specs of `test/integration/cni` |
| `kops-test.yaml` | daily | a kops cluster, no CNI suite (`:44-49`) |
| `generate-limits.yaml` | weekdays | `make generate-limits` and opens a PR (`:36`, `:55`) |
| `release.yaml` | release published | `make generate-cni-yaml`, the eks-charts sync and `config-folder-sync` (`:20-31`) |

`scripts/run-integration-tests.sh` runs the `CANARY` specs of `test/integration/cni` (skipping `STATIC_CANARY`) and `test/integration/ipamd`, then the whole `test/integration/custom-networking-sgpp` suite (`:297-302`), on an IPv4 Linux eksctl cluster from `scripts/test/config/test-cluster.yaml`, then conformance (`:312-324`). The `CANARY` set is small (`test/integration/cni/service_connectivity_test.go:38`, `test/integration/cni/pod_traffic_test.go:172`, `:210`, `test/integration/ipamd/eni_ip_leak_test.go:20`).

No workflow runs `ipv6`, `pod-eni`, `eni-trunking`, `custom-networking`, `eni-subnet-discovery`, `cni-egress`, `cni-upgrade-downgrade`, `snat`, `multus`, `calico`, `az-traffic`, `metrics-helper` or `core-plugins`, or the non-`CANARY` specs of `cni` and `ipamd`, such as the host networking cleanup spec (`test/integration/cni/host_networking_test.go:57`). `metrics-helper` and `core-plugins` run from `scripts/run-cni-release-tests.sh` (`:50`, `:55`), `ipv6` from `scripts/run-ipv6-integration-tests.sh` and `multus` from `scripts/run-multus-tests.sh`. There is no Windows suite. A change to IPv6, SGP, egress, upgrade and downgrade, SNAT or host-rule cleanup has no automated cluster coverage, so the author runs the suite and pastes the output.

`lint`, `unit-test-race` and `make generate` are not run in PR CI.

### Entries

<a id="c1"></a>
### [advisory] C1. Change the chart, not `config/master`

**Rule.** Edit `charts/aws-vpc-cni` (env in `values.yaml`, rendered by `templates/daemonset.yaml:89-97`). Leave `config/master/*.yaml` alone; the release PR and `release.yaml` regenerate it (`scripts/generate-cni-yaml.sh:11-12`, `:67-80`; `release.yaml:26-31`). Regenerate in the PR only when the new image cannot run with the old manifests, as `a4cfda20` did for the probe address.

**Why.** Recent non-release chart changes (`678c61e1`, `cbf41380`, `4915d8ff`, `58f46523`) did not touch `config/master`; release `4696ca43` did. Hand edits to `config/master` are overwritten. Maintainers ask for the chart path ([T4](#themes), #3428).

**Check.** `make helm-lint`. `git diff --stat` touching `config/master` without `charts/` or a release is suspect. New env vars need a README entry.

<a id="c2"></a>
### [advisory] C2. Say which cluster suite you ran; CI runs only the IPv4 Linux canary set

**Rule.** For datapath, IPAM or host-rule changes, run the matching `test/integration/<suite>` on a real cluster and paste the output (PR template `:30-33`). Name the IP family, OS/AMI and iptables backend.

**Why.** PR CI has no cluster (`pr-automated-tests.yaml`). After merge, `integration-tests.yaml` runs only `CANARY` tests of `cni` and `ipamd` plus `custom-networking-sgpp` on an IPv4 Linux eksctl cluster (`scripts/run-integration-tests.sh:297-302`). IPv6, pod-eni, egress, upgrade/downgrade, snat and the host-networking cleanup test run in no workflow ([section 5](#ci)). `soak-tests.yml` sets variables the script does not read, so it runs no suite *(inferred)*. Maintainers reject mocked unit tests as proof for kernel races ([T2](#themes)).

**Check.** A maintainer can run `pr-manual-tests.yaml` against the PR merge ref. Suite → scripts: `scripts/run-ipv6-integration-tests.sh`, `scripts/run-cni-release-tests.sh`, `scripts/run-multus-tests.sh`.

<a id="c3"></a>
### [advisory] C3. Regenerate mocks and generated code; CI does not check

**Rule.** After changing an interface with a `generate_mocks.go`, run `make generate` (`Makefile:378-380`) and commit the `mocks/` diff. `pkg/nft/mocks` has no generator; update it by hand with the same mockgen invocation. After changing `scripts/gen_vpc_ip_limits.go` overrides, run `make generate-limits` (`Makefile:384-387`).

**Why.** No workflow runs `make generate` or diffs the tree; `lint` and `unit-test-race` are not run in PR CI either (`pr-automated-tests.yaml:28-39`). A stale mock compiles until a test calls the new method. Exact mock expectations (`.Times(1)`, real values) are a recurring ask ([T8](#themes)).

**Check.** `make generate && git status --porcelain` is clean. Run `make unit-test-race` for changes in `pkg/ipamd`, `pkg/networkutils`, `pkg/awsutils` (`Makefile:227-237`).

<a id="themes"></a>
## 6. Recurring review themes

Roughly ordered by how often maintainers raised them on PRs closed between 2024-06-21 and 2026-11-16 (413 maintainer comments on 110 PRs; hand-tagged, approximate). The IDs are referenced from the entries above. General review hygiene is the last row; its questions are in [General review hygiene](#ck-general).

| ID | Severity | Theme | Reviewer's question | Examples |
|---|---|---|---|---|
| T1 | blocking | Error swallowed or fail-open | If this call fails, does the error reach the caller or the user, or do we silently continue with a wrong or less secure state? Blocking when: a failed read of rules, chains, subnets or SGs is treated as "absent" or falls back to a broader default (e.g. primary SGs, duplicate chain, unexcluded subnet). | [#3588](https://github.com/aws/amazon-vpc-cni-k8s/pull/3588#discussion_r2950298924), [#3463](https://github.com/aws/amazon-vpc-cni-k8s/pull/3463#discussion_r2723224597), [#3339](https://github.com/aws/amazon-vpc-cni-k8s/pull/3339#discussion_r2201336186) |
| T2 | blocking | Integration test or repro on a real cluster | Is there an integration test under `test/integration/` (or a repro that fails on master and passes here), since a mocked unit test does not trigger the real race or datapath? Blocking when: a datapath, IPAM or kernel-race fix is backed only by unit tests with mocked netlink/EC2/iptables. | [#3858](https://github.com/aws/amazon-vpc-cni-k8s/pull/3858#issuecomment-5734383021), [#3782](https://github.com/aws/amazon-vpc-cni-k8s/pull/3782#issuecomment-5110149195), [#3661](https://github.com/aws/amazon-vpc-cni-k8s/pull/3661#issuecomment-4298571042) |
| T3 | blocking | ENI selection: custom networking, trunk, multi-NIC, excluded ENIs | Which ENIs does this loop touch, and is it still right when eth0 is skipped (custom networking), a trunk ENI exists (SGP), the ENI is excluded/unmanaged, or there are network cards > 0? Blocking when: a change iterates or allocates over ENIs without the trunk / `IsExcludedForPodIPs` / custom-networking-primary / network-card filters the sibling paths apply. | [#3873](https://github.com/aws/amazon-vpc-cni-k8s/pull/3873#discussion_r4049043379), [#3895](https://github.com/aws/amazon-vpc-cni-k8s/pull/3895#discussion_r4362153696), [#3913](https://github.com/aws/amazon-vpc-cni-k8s/pull/3913#issuecomment-6807969664) |
| T4 | advisory | Config surface: env var, Helm chart, manifests, docs | Do we need a new env var at all (vs. changing the default), is it named and defaulted consistently, and is it wired through `charts/aws-vpc-cni` (not the generated YAML) and documented in README? | [#3873](https://github.com/aws/amazon-vpc-cni-k8s/pull/3873#discussion_r3982709188), [#3645](https://github.com/aws/amazon-vpc-cni-k8s/pull/3645#discussion_r3046693932), [#3428](https://github.com/aws/amazon-vpc-cni-k8s/pull/3428#issuecomment-3251014303), [#3287](https://github.com/aws/amazon-vpc-cni-k8s/pull/3287#discussion_r2100780495) |
| T5 | blocking | iptables vs nftables backend and rule identity | Is this correct on iptables-legacy, iptables-nft and nftables, with kube-proxy in iptables or IPVS mode, and does rule matching check every field, ordering and the OR-vs-XOR mark semantics? Blocking when: rule matching or cleanup can misidentify one of our rules (wrong field set, ordering, mark op) so reconcile deletes or duplicates live rules. | [#3588](https://github.com/aws/amazon-vpc-cni-k8s/pull/3588#discussion_r2955114844), [#3588](https://github.com/aws/amazon-vpc-cni-k8s/pull/3588#discussion_r3189189451), [#3588](https://github.com/aws/amazon-vpc-cni-k8s/pull/3588#discussion_r2944430442) |
| T6 | blocking | EC2 / API-server call cost and throttling | Does this add an EC2 (or kube-apiserver) call per pod or per reconcile, and can it be cached, paginated or moved off the hot path so a fleet does not throttle the account? Blocking when: a new EC2 call runs per CNI ADD or per reconcile loop with no cache, or an unbounded retry can cause a thundering herd. | [#3463](https://github.com/aws/amazon-vpc-cni-k8s/pull/3463#discussion_r2726273752), [#3354](https://github.com/aws/amazon-vpc-cni-k8s/pull/3354#discussion_r2237928795), [#3523](https://github.com/aws/amazon-vpc-cni-k8s/pull/3523#issuecomment-3577304066), [#3873](https://github.com/aws/amazon-vpc-cni-k8s/pull/3873#discussion_r4049937550) |
| T7 | blocking | Plugin / ipamd / NP-agent boundary | Does this run in the right process: the host-side `aws-cni` binary (no daemonset env, config via conflist), ipamd (gRPC), or the periodic reconciler, and do both sides of the gRPC contract (version, proto, cniVersion) still agree? Blocking when: plugin code reads an env var only ipamd has, the proto is hand-edited instead of regenerated, or plugin and ipamd versions can mismatch. | [#3440](https://github.com/aws/amazon-vpc-cni-k8s/pull/3440#discussion_r2392457252), [#3879](https://github.com/aws/amazon-vpc-cni-k8s/pull/3879#discussion_r4141006970), [#3304](https://github.com/aws/amazon-vpc-cni-k8s/pull/3304#discussion_r2127523975), [#3873](https://github.com/aws/amazon-vpc-cni-k8s/pull/3873#discussion_r3984838600) |
| T8 | advisory | Unit-test assertions are exact | Do mocks use real values and exact call counts (`.Times(1)`, not `gomock.Any()` / `.AnyTimes()`), and does the test actually enforce what its comment claims? | [#3782](https://github.com/aws/amazon-vpc-cni-k8s/pull/3782#discussion_r3669538967), [#3874](https://github.com/aws/amazon-vpc-cni-k8s/pull/3874#discussion_r4009614582), [#3588](https://github.com/aws/amazon-vpc-cni-k8s/pull/3588#discussion_r2955772470) |
| T9 | advisory | Reuse existing helpers and data | Is there already a helper, constant or source of truth (nil-safe accessor, `awssession.New`, VPC RC `limits.go` `IsTrunkingCompatible`, IMDS instance ID) we should call instead? | [#3250](https://github.com/aws/amazon-vpc-cni-k8s/pull/3250#issuecomment-3255089878), [#3647](https://github.com/aws/amazon-vpc-cni-k8s/pull/3647#discussion_r3060896420), [#3588](https://github.com/aws/amazon-vpc-cni-k8s/pull/3588#discussion_r2955462994) |
| T10 | advisory | Log level, volume and content | Is this at the right level, not inside the datastore lock, not leaking sensitive data, and is a pod/node event better than another ipamd log line? | [#3380](https://github.com/aws/amazon-vpc-cni-k8s/pull/3380#discussion_r2265413711), [#3429](https://github.com/aws/amazon-vpc-cni-k8s/pull/3429#discussion_r2319825343), [#3843](https://github.com/aws/amazon-vpc-cni-k8s/pull/3843#discussion_r3867357561) |
| T11 | blocking | Upgrade, downgrade and rollback | After rolling back to the previous release (or upgrading with a flag already set), are stale nft/iptables rules, ENIs, pod IPs or CRDs left behind or misread? Blocking when: n-1 misreads or deletes state this version writes, or a rollback leaks ENIs/IPs or leaves rules in the other backend. | [#3875](https://github.com/aws/amazon-vpc-cni-k8s/pull/3875#discussion_r4161007230), [#3463](https://github.com/aws/amazon-vpc-cni-k8s/pull/3463#pullrequestreview-3700155980), [#3588](https://github.com/aws/amazon-vpc-cni-k8s/pull/3588#discussion_r2955697904), [#3588](https://github.com/aws/amazon-vpc-cni-k8s/pull/3588#discussion_r3321367782) |
| T12 | advisory | Testing evidence in the PR description | What did you run (integration suite, scale test, cluster type), and can the output go in the description? | [#3661](https://github.com/aws/amazon-vpc-cni-k8s/pull/3661#issuecomment-4266023428), [#3437](https://github.com/aws/amazon-vpc-cni-k8s/pull/3437#issuecomment-3275703349), [#3287](https://github.com/aws/amazon-vpc-cni-k8s/pull/3287#issuecomment-2898594621) |
| T13 | blocking | IPv6 / dual-stack parity | Does this hold for an IPv6 cluster, including an IPv6 node in a dual-stack subnet, and does it keep the existing v6 check? Blocking when: a v4-only change alters a code path IPv6 mode shares, or removes a v6 guard without moving it. | [#3827](https://github.com/aws/amazon-vpc-cni-k8s/pull/3827#discussion_r3763615507), [#3827](https://github.com/aws/amazon-vpc-cni-k8s/pull/3827#discussion_r3762793448), [#3380](https://github.com/aws/amazon-vpc-cni-k8s/pull/3380#discussion_r2265414967) |
| T14 | blocking | Security groups and SGP semantics | What SGs does the ENI end up with, does this match how EC2 and the VPC Resource Controller actually treat SGs and branch ENIs, and does it trigger repeated `ModifyNetworkInterfaceAttribute`? Blocking when: an ENI can be created or left with broader SGs than the user configured. | [#3720](https://github.com/aws/amazon-vpc-cni-k8s/pull/3720#issuecomment-4653079580), [#3380](https://github.com/aws/amazon-vpc-cni-k8s/pull/3380#discussion_r2265091197), [#3963](https://github.com/aws/amazon-vpc-cni-k8s/pull/3963#discussion_r4474313272) |
| T15 | advisory | Integration-test framework hygiene | Does the test run in the test namespace, clean up subnets/ENIs it creates, bound remote exec with a timeout, and avoid releasing shared resources too early? | [#3895](https://github.com/aws/amazon-vpc-cni-k8s/pull/3895#discussion_r4340703574), [#3647](https://github.com/aws/amazon-vpc-cni-k8s/pull/3647#discussion_r3156033115), [#3959](https://github.com/aws/amazon-vpc-cni-k8s/pull/3959#discussion_r4462080032) |
| T16 | advisory | Ordering inside CNI ADD | Is validation done before the veth is created, and is the host veth configured (sysctls) before it is brought up, so there is no window or leaked link on failure? | [#3764](https://github.com/aws/amazon-vpc-cni-k8s/pull/3764#discussion_r3604680065), [#3854](https://github.com/aws/amazon-vpc-cni-k8s/pull/3854#discussion_r3938466164), [#3924](https://github.com/aws/amazon-vpc-cni-k8s/pull/3924#discussion_r4471099409) |
| T17 | blocking | AWS partitions | Does this work in CN, GovCloud and new partitions, i.e. is the partition resolved rather than hard-coded? Blocking when: an ARN, endpoint or service name hard-codes the `aws` partition. | [#3874](https://github.com/aws/amazon-vpc-cni-k8s/pull/3874#discussion_r4009947963), [#3874](https://github.com/aws/amazon-vpc-cni-k8s/pull/3874#discussion_r3994076393) |
| — | advisory | General hygiene (naming, constants, redundant code, LLM-style or stale comments, Go idioms, `%w` wrapping) | General Go review; not repository-specific. Most come from two large PRs (#3588, #3380). | [#3432](https://github.com/aws/amazon-vpc-cni-k8s/pull/3432#discussion_r2533099926), [#3117](https://github.com/aws/amazon-vpc-cni-k8s/pull/3117#discussion_r1849126766), [#3588](https://github.com/aws/amazon-vpc-cni-k8s/pull/3588#discussion_r2951058174) |
