# Using AGENTS.md and the review guide

This repository has two files meant to be used while you write and review changes:

| File | What it is | Who reads it |
|---|---|---|
| `AGENTS.md` | Build and test commands, CI layout, and the hard rules that most often break IP assignment, pod connectivity, upgrades or rollbacks when missed | Coding agents load it automatically (see [Tools](#tools)). Humans: read it once. |
| `docs/review-guide.md` | The full checklist: ipamd contracts, CNI plugin and host datapath contracts, config and CI, recurring review themes. Each item is tagged `[blocking]` or `[advisory]` and cites code at a pinned commit. | Read the sections for the files your change touches. The routing table in section 1 maps files to entries. |

## While you write the change

1. **Find the entries that apply.** Look up the files you are changing in the routing table at the top of `docs/review-guide.md` and read those entries first. If you use a coding agent, ask it to do the same:

   ```text
   # A prompt for your coding agent before it starts editing
   Read AGENTS.md and the entries of docs/review-guide.md that the routing table
   names for the files this change will touch. List the [blocking] items that
   apply before you write code.
   ```

2. **Check the modes your change reaches.** Most review rounds here come from a path that works in one mode and not another. For each, either test it or say in the PR why the change cannot reach it:

   - IPv4 and IPv6 clusters
   - prefix delegation on and off
   - custom networking
   - security groups for pods, strict and standard mode
   - more than one network card
   - iptables-legacy, iptables-nft and nftables nodes, for host-rule changes

3. **Go through the hard rules in `AGENTS.md`** before you push. They are the checks that most often cost a review round: DEL that fails on missing state, plugin settings read from env, checkpoint format changes, ENI loops missing a filter, EC2 calls on the hot path, and edits to generated files (`config/`, `pkg/vpc/vpc_ip_resource_limit.go`, `*.pb.go`, mocks).

4. **Run the local checks PR CI runs**, then regenerate what CI does not check:

   ```bash
   # The checks PR CI runs, without a cluster
   make check-format vet build-linux unit-test helm-lint

   # Race detection on the packages with concurrency; native only
   make unit-test-race

   # Regenerate mocks and the gRPC code; the tree should be clean afterwards if nothing changed
   make generate && git status --porcelain
   ```

## Before you open the PR

Go through the entries of `docs/review-guide.md` that the routing table names for your files, and check the items that apply to your change. Look up the relevant entries rather than handing the whole guide to an agent: it is long, and the essentials are already in `AGENTS.md`.

If you ask an agent for a self-review, keep the guide out of its prompt and give it the severity bar instead:

```text
# A self-review prompt; run it on your branch before opening the PR
Review the diff between origin/master and HEAD. Report a finding as blocking only
if it gives pods wrong or duplicate IPs or wrong connectivity, leaks IPs, ENIs or
host network state without bound, leaves a node unable to assign IPs or route pod
traffic until someone intervenes, or leaves nodes broken after an upgrade or
rollback. Everything else is advisory; keep advisory findings to one line each.
For any finding about the checkpoint, ENI tags or filters, prevResult, the
conflist, rpc.proto or rule priorities, check the matching entry in
docs/review-guide.md before reporting it.
```

Then fill in the PR template (`.github/PULL_REQUEST_TEMPLATE.md`), in particular:

- **Testing done.** Which `test/integration` suites you ran, on which IP family, OS or AMI, and iptables backend. PR CI runs no cluster, and the post-merge run covers only a small IPv4 set (see section 5 of the guide), so the output you paste is often the only cluster evidence a reviewer has.
- **Upgrades and downgrades.** What happens on upgrade from the last release and on rollback to it: the checkpoint, ENIs and their tags, host rules, and the conflist.
- **New dependencies.** Every new EC2, IMDS or Kubernetes API call, with its call rate per pod or per pool-manager pass, and any IAM policy or RBAC change it needs.
- **Before and after numbers** for anything that changes API call rates, pool sizing or pod startup latency.

## When you review someone else's PR

The same applies: use the guide to check specific items the diff touches, not as a checklist to run in full. An item's tag is its worst case; rate each finding by its own reach and whether it self-corrects. An agent tends to report more concerns than a human reviewer would, so drop the ones you wouldn't raise yourself.

## Tools

Which tools load `AGENTS.md` without extra setup, per their documentation (checked November 2026):

| Tool | Loads `AGENTS.md` | Otherwise |
|---|---|---|
| Claude Code (v2.1.277 and later) | Yes, when the repository has no `CLAUDE.md` | If you add a `CLAUDE.md`, start it with `@AGENTS.md` ([docs](https://code.claude.com/docs/en/memory)) |
| Kiro | Yes ([docs](https://kiro.dev/docs/steering/)) | |
| GitHub Copilot coding agent and code review on github.com, VS Code agent mode | Yes ([docs](https://docs.github.com/en/copilot/reference/custom-instructions-support)) | |
| Copilot code review in VS Code | No, it reads `.github/copilot-instructions.md` only | Paste the review prompt above, or point it at the files |
| Amazon Q Developer CLI | Not by default | Add `file://AGENTS.md` to your agent's `resources` |

## Keeping the guide current

- **Citations** are `path:line` at the commit named in section 1 of the guide. When master moves far enough that lines drift, check every citation at the new commit and update the commit. Find a moved line by the quoted symbol.
- **New entries** follow the shape of sections 3 and 4 (**Rule.**, **Why.**, **Cites.**, **Check.**), with a severity tag, a bullet in the section 2 checklist and a row in the section 1 routing table. Add an entry when a review finds a repository-specific problem a strong general reviewer would likely miss. General review hygiene goes in the single list in section 2, not in new entries.
