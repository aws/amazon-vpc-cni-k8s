# Native nftables connmark restore contract

## Problem

The VPC CNI owns only the bits selected by its connmark mask, normally
`0x00000080`. Other networking components can use the remaining packet-mark
bits.

VPC CNI v1.23.0 restored the conntrack mark with this native nftables rule:

```nft
meta mark set ct mark & 0x00000080
```

That assignment replaces the complete packet mark. For example, a packet with
Calico's eBPF-seen mark `0x01000000` becomes `0x00000000` or `0x00000080`.
Calico then drops it as traffic from a workload that was not processed by BPF.

## Required behavior

For packet mark `P`, conntrack mark `C`, and the VPC CNI-owned mask `M`, restore
must produce:

```text
P' = (P & ~M) | (C & M)
```

This gives two invariants:

```text
P' & ~M == P & ~M
P' &  M == C &  M
```

The first invariant is the interoperability contract: VPC CNI must not change
packet-mark bits it does not own.

## Fix

Native nftables netlink bitwise expressions combine a register with constants;
they do not directly express the full two-register masked assignment above.
The implementation therefore copies each owned bit with two mutually exclusive
rules.

For the default `0x80` bit, the rules are equivalent to:

```nft
ct mark & 0x00000080 == 0x00000000 \
    meta mark set meta mark & 0xffffff7f

ct mark & 0x00000080 == 0x00000080 \
    meta mark set meta mark | 0x00000080
```

The implementation emits this pair for every bit in `M`, so arbitrary
multi-bit connmark masks retain the same semantics as masked iptables
`CONNMARK --restore-mark`. This costs two rules per set mask bit; the normal
one-bit mask therefore adds two rules, while the theoretical 32-bit maximum
adds 64.

During reconciliation, only the complete new rule shape is accepted. The
v1.23.0 overwrite rule is classified as stale, causing the base chain to be
flushed and reinstalled safely during upgrade.

## Test contract

### Unit behavior

Given the default VPC CNI mask `M = 0x80`:

| Initial packet mark | Conntrack mark | Required packet mark |
| --- | --- | --- |
| `0x01000000` | `0x00000080` | `0x01000080` |
| `0x01000080` | `0x00000000` | `0x01000000` |
| `0x01000000` | `0x00000000` | `0x01000000` |
| `0x01000080` | `0x00000080` | `0x01000080` |

The tests must also cover a multi-bit mask and prove both invariants for every
owned bit.

### Reconciliation

Tests must prove that:

1. The new clear and set rules are recognized only when every expression,
   register, mask, comparison, and operation has the expected shape.
2. The v1.23.0 full-overwrite rule is not recognized as valid.
3. A chain containing the v1.23.0 rule is flushed and rebuilt with the safe
   rules.
4. Rule order is validated from the sequence returned by nftables, never from
   rule handles.
5. Repeated setup is idempotent and preserves rule order.
6. CIDR reconciliation and cleanup behavior remain unchanged.

### Kernel integration

In an isolated network namespace with native nftables:

1. Setup creates the fib-local return, workload-veth jump, and both restore
   rules in the required order.
2. A second setup does not duplicate rules.
3. CIDR changes reconcile correctly.
4. Seeding the actual v1.23.0 overwrite rule and running setup replaces it
   with exactly one valid clear/set pair.
5. Cleanup removes the VPC CNI table.

### EKS interoperability

The end-to-end contract uses the real chained topology, not an injected or
synthetic nftables hook:

1. Run VPC CNI v1.23.0 as the primary CNI.
2. Run Calico v3.31 in Amazon VPC chained mode with the eBPF dataplane.
3. Disable kube-proxy so Calico provides Kubernetes Service handling.
4. Confirm the stock image installs
   `meta mark set ct mark & 0x00000080`.
5. Pin client and endpoint pods to different named nodes, recreate the client
   or otherwise use fresh five-tuples, and record before/after counters.
6. Generate pod-to-Service traffic and confirm Calico's
   `From workload without BPF seen mark` drop counter increases.
7. Replace only the `aws-node` image with an image built from the v1.23.0 tag
   plus this patch. Validate current main separately so unrelated commits
   cannot explain the A/B result.
8. Confirm the legacy rule is reconciled to the masked clear/set pair.
9. Recreate the same client placement and repeat the traffic with fresh flows.
   Require:
   - Service and DNS connectivity succeeds.
   - Calico's mark-loss drop counter does not increase.
   - `aws-node` and Calico remain ready.
   - Direct pod connectivity and normal VPC CNI SNAT behavior still work.
   - A primary-ENI NodePort or LoadBalancer return-path test to a pod on a
     secondary ENI still succeeds.
