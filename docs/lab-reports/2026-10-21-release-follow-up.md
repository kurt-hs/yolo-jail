---
title: "Release follow-up and recovered work"
date: 2026-10-21
status: open
tags: [lab-report, release, verification, macos-user]
summary: "Separates the published v0.15.0 evidence from current-source CI failures and the native, rootless, and owner-authorized follow-ups that remain open."
---

# Release follow-up and recovered work, 2026-10-21

A [lab report](README.md). This record distinguishes an immutable publication from a later
workflow target, records confirmed outcomes, and routes unfinished work to its canonical home.
It does not add a release gate or authorize live-agent, credential, or native-Mac trials.

## Observed

The prior work inventory recorded 59 rows: 45 complete and 14 unfinished. Those are inherited
roll-up counts, not a row-by-row re-audit. The completed outcomes that can be tied to current
source and publication evidence are grouped below; unfinished items keep their existing owners,
dependencies, and authorization boundaries.

### Published release and completed source outcomes

- **v0.15.0 is published** from immutable source commit
  [`b01c111`](https://github.com/mschulkind-oss/yolo-jail/commit/b01c1110e84b3900af88c0b39b156a512c7c0130)
  at 2026-10-20 22:39:23 UTC. The [release](https://github.com/mschulkind-oss/yolo-jail/releases/tag/v0.15.0)
  has four platform archives, checksums, and a publication claim. Release request
  [39503499835](https://github.com/mschulkind-oss/yolo-jail/actions/runs/39503499835),
  the targeted Release run [39508882822](https://github.com/mschulkind-oss/yolo-jail/actions/runs/39508882822),
  Publish run [39509720806](https://github.com/mschulkind-oss/yolo-jail/actions/runs/39509720806)
  (all 13 jobs succeeded), and Homebrew run
  [39509723871](https://github.com/mschulkind-oss/yolo-jail/actions/runs/39509723871) succeeded.
- **Do not conflate target with workflow HEAD.** The tag and release request identify `b01c111`;
  Release, Publish, and Homebrew jobs ran from workflow HEAD `94989f1` while explicitly targeting
  that tag/request. A separate request, [39504109994](https://github.com/mschulkind-oss/yolo-jail/actions/runs/39504109994),
  failed eligibility for target `94989f1`; it was not a failed publication of v0.15.0.
- **Current source is later, but runtime and test inputs match the published tag.** The checkout
  starts at `31d61546ccf7aa5ae763f4e5541011e007e5cb10`. `git diff v0.15.0 -- cmd internal integration
  packs go.mod go.sum vendor flake.nix flake.lock` is empty; the complete diff from the tag is
  documentation-only. This is a tree comparison, not an ancestry inference.
- **The refresh-fallback sequencing repair is already shipped.** Commit
  [`6ddb485`](https://github.com/mschulkind-oss/yolo-jail/commit/6ddb485188c638a9586469ae023f9e5a6e2cc0cc)
  is in v0.15.0. The generated-launcher regression
  [`TestAFailedBackgroundRefreshRefusesWhenTheRetryFails`](../../internal/entrypoint/prelaunchrefreshtiming_test.go#L334-L373)
  and production caller [`prelaunchrefresh.go`](../../internal/entrypoint/prelaunchrefresh.go#L613-L628)
  match the tag. The test-first fix prevents `YOLO_USE_INSTALLED` from queuing another refresh
  after handling a failed background result; this record does not claim a new fix.
- **The selected-pack root retention fix and its caller test are shipped.** Commit
  [`bd08859`](https://github.com/mschulkind-oss/yolo-jail/commit/bd08859d6f73458bea33fb30c379a4e047f98338)
  is an ancestor of v0.15.0. [`Check`](../../internal/cli/check/check.go) retains staged selected
  pack roots through later report sections, and
  [`TestCheckKeepsSelectedForkRootThroughTheHostFloor`](../../internal/cli/check/selectedpackroot_test.go)
  exercises the production `Check` order and verifies cleanup afterwards. These source and test
  files are unchanged from the tag.
- **Per-session profile isolation and owned-host capture are built for the container and
  owned-host paths.** The [settled profile design](../design/per-session-profiles.md)
  records per-session entry snapshots and rendered homes, with host boundary and capture
  outcomes; those fixtures do not prove a real agent's subagent behavior.
- **The native Pi per-session adaptation is implemented, with native runtime still unmeasured.**
  The [macos-user profile record](../design/macos-user-session-profiles.md) reports synthetic
  bootstrap, capture and child-inheritance fixtures plus Darwin cross-builds; it does not
  establish Seatbelt behavior on a Mac or real Pi descendants.
- **Synthetic Claude coordination is completed within its authorized boundary.** The
  [native Claude record](../design/macos-user-claude-refresh.md#the-native-measurement-contract)
  documents the synthetic-first contract and passing Linux fixture slice. It is not a real
  Claude login, refresh, Keychain or vendor-lock measurement.
- **Developer-Mac suite repairs have a recorded native run.** The
  [suite record](../design/macos-user-suite-on-a-developer-mac.md#12-what-done-looks-like)
  reports `TestMacosUser` passing on the host of record (150 passed, 4 skipped, 0 failed)
  with the narrow-sudo cell passing. This is that dated suite evidence, not a run performed
  for this follow-up.
- **The repaired developer-Mac live-config unit cases are recorded as passing** at `94989f1`:
  the [live-config design](../design/live-config-apply.md) and
  [canonical roadmap](../plans/roadmap.md#immediate-continuation) retain the still-owed rootless,
  real-broker, and parity evidence. Native unit success does not discharge those separate checks.

## Current CI finding

CI run [39529735230](https://github.com/mschulkind-oss/yolo-jail/actions/runs/39529735230) targets
current main `31d61546ccf7aa5ae763f4e5541011e007e5cb10` and failed. The Go and macOS unit jobs
show the same four distinct failures, each reproduced in both jobs: `TestAnAttachToAJailTheLastReleaseLaunchedNeverRidesAlong`,
`TestShippedPacksDecodeUnderTheLastRelease`,
`TestModelListCheckPackWideMetadataPreservesTheLastReleasesProgram`, and
`TestTheJudgeAcceptsWhatTheLastReleaseReportsOnABareMac`. Their release-decoder fixtures attempt
to build or archive the ancestral v0.2.0 tree, which predates the required `go.mod` and `packs/`
inputs; the recorded diagnostics are missing-module and missing-pack-tree errors. This is a
historical-baseline fixture incompatibility, not evidence that the v0.15.0 publication failed.

A separate code writer is repairing those fixtures. At this report's snapshot, that repair has not
landed or passed the gate: do not claim CI is green, the repair is complete, or landing is done.
The run's integration summary failed because its required shard set was incomplete; it is not a
completed integration result on current source. The [CI run record](https://github.com/mschulkind-oss/yolo-jail/actions/runs/39529735230)
is the evidence to recheck after the repair.

## Open work and disposition

The [roadmap](../plans/roadmap.md) orders the shared backlog; each linked design owns its detail.
These are follow-ups with their stated dependencies, not newly declared release holds.

- **Real broker after config widening:** still needed by the
  [live-config design](../design/live-config-apply.md), alongside rootless M1 on both required
  architectures and remount/parity checks. The nested jail cannot stand in for a real rootless
  host or a real broker request. Live-config's current-source/native result is not the same as
  hosted landed-evidence transport; preserve the Mac result with its source identity and durable
  acceptance evidence rather than treating a local note as the hosted witness.
- **External GitHub broker fixture authorization is separate and still owed.** Before an
  external fixture is run, obtain explicit approval for scope A/B and evidence of the broker's
  lifetime, current-authority checks and capability witness, as described by the
  [broker handler contract](../design/boundary-broker.md#L1264-L1270). A live-config request
  after widening does not authorize or discharge this distinct fixture and witness.
- **Rootless delivery and loopback reachability:** use the
  [image delivery reference](../reference/image-staging-vs-baking.md) and
  [reachability reference](../reference/loopback-tls-reachability.md). A nested jail's forced
  rootful and shared-network modes cannot verify the rootless store-delivery or host-to-jail
  loopback-forwarding branches. Obtain evidence from a real rootless host or CI before claiming
  those behaviors verified.
- **Externally handled Pants/VPN DNS report:** the existing
  [DNS lab report](2026-10-19-pants-dns-under-vpn.md) remains explicitly outside this work. It is
  not fixed here, and this follow-up does not reopen or investigate it.
- **Native Claude vendor behavior and credential view:** the
  [native Claude measurement design](../design/macos-user-claude-refresh.md) still requires a
  native measurement of Claude's file-lock/re-read and file-versus-Keychain behavior. Testing the
  already-built view requires a separately authorized disposable-login trial; the
  [owning view design](../design/claude-login-without-interception.md#11-decision-ledger) keeps it
  off by default until its measures and the independent real-rootless-host day are satisfied.
  This report performed no agent/API call and read no credentials.
- **Concurrent macOS workspaces:** the architecture is already selected: one guest user, distinct
  persistent private workspace homes/state, and only the selected shared machine paths. That
  decision and its boundaries live in the
  [concurrent-workspace design](../design/macos-user-concurrent-workspaces.md#selected-architecture-and-boundary)
  and [home-tier ledger](../reference/macos-user-home-tiers.md#decision-ledger). MCW4's sharing
  policy is settled; the physical backing and relative-link geometry remain engineering work, not
  a new permission grant. [OQ-MCW2](../design/macos-user-concurrent-workspaces.md#OQ-MCW2) remains
  the same-UID/outside-Seatbelt threat-claim question, and
  [OQ-MCW3](../design/macos-user-concurrent-workspaces.md#OQ-MCW3) remains the reclamation policy.
  The [implementation sketch](../design/macos-user-concurrent-workspaces-plan.md) is not a build
  handoff: concrete layout, reader inventory, lifecycle, and authorized native sibling-denial
  proof remain before implementation.
- **Native Pi/fork and remaining Mac launch evidence:** the
  [Linux fork report](2026-10-19-linux-fork-materialization-verification.md) and
  [roadmap](../plans/roadmap.md#immediate-continuation) retain the native Pi/fork runtime work and
  the Mac launcher findings. The `.local` sidecar defect remains explicitly owner-deferred and
  unverified; do not silently promote it into a fix. The roadmap still records the Apple Container
  running-builder false refusal, the remaining cancellation diagnostic, the podman symlinked
  workspace failure, and the `yolo host --` home-path refusal. Check each current disposition in
  the roadmap before taking it up.
- **Watcher ownership:** a watcher that wakes an agent must be owned by the current live session;
  an old-session-only promise is insufficient. Keep this criterion with the
  [agent-event watcher design](../design/agent-event-watchers.md), not as an implementation
  claim in this release report.

## Fresh observer verification (2026-10-21 03:00–03:08 UTC)

This later read-only observation supersedes the current-status snapshot above. At
03:04 UTC, fresh local probes and a separate remote ref read both identified main
as `31d61546ccf7aa5ae763f4e5541011e007e5cb10`.

The matching-SHA [regular CI run](https://github.com/mschulkind-oss/yolo-jail/actions/runs/39529735230)
failed in `check-go` at “Quality gate (vet + staticcheck + gofmt + short tests)”
and in `check-macos` at “Unit tests (macOS)”; `integration-complete` also failed
its “Every integration shard succeeded” aggregate. `secrets-scan` passed. These
are observed job/step outcomes, not diagnoses: no failure cause is inferred here.
The matching-SHA [Apple Container parity run](https://github.com/mschulkind-oss/yolo-jail/actions/runs/39530138735)
passed, including its parity and archive-delivery checks. Earlier test-name and
cause details above were not re-established by these fresh reports and should
not be treated as a current diagnosis.

The parent session's native schedule history independently records five
completed natural observer fires at 03:00:44, 03:02:44, 03:04:44, 03:06:44,
and 03:08:44 UTC; each generated an automatic completion notification in the
owning parent session. The first three fires correspond to separate
timestamped read-only GitHub observations summarized here. The observer was
configured every two minutes, bound to its creating session, non-quiet, with
overlap skipped and no catch-up. Its prior-generation schedule was paused with
its definition and history retained. These are schedule-history fire times,
not claimed workflow-completion times; this records event delivery, not a
promise that a process survives restart.

The observations used read-only local Git probes and authenticated GitHub reads
through the existing broker. No token or credential was read, copied, or
logged; no GitHub write, retry, cancellation, dispatch, or release action was
performed. The immutable v0.15.0 publication at `b01c111` remains distinct
from current-main CI, as recorded above.

## Limits

This report records public project outcomes and evidence, not private session transcripts. It makes
no claim that all 45 historical completed rows were re-audited, no claim that current CI passed, and
no new release hold. Native Claude use, credentials, interactive agents, live VPN/DNS investigation,
rootless host trials, and primary-branch landing were not performed here. The durable project queue
remains the [roadmap](../plans/roadmap.md).
