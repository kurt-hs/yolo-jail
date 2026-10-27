---
title: "Readable config: post-Mac writer handoffs"
date: 2026-10-27
status: draft
stage: DECIDED
next: "Root admits an eligible clean integration base after item16 and item2-after16; only then allocate the four conditional writer jobs"
depends-on:
  - ../design/readable-config-plan.md
  - ../design/readable-config.md
  - ../design/config-commands.md
  - ../design/config-migration.md
  - ../design/config-project.md
  - ../design/config-user.md
  - ../design/pack-manifests.md
tags: [config, handoff]
summary: "Source-grounded, conditional implementation briefs; no implementation or runtime acceptance is claimed."
---

# Readable config: post-Mac writer handoffs

**Prepared:** 2026-10-27, against published source `cea3797a2ad0d5691e39152043b65ec659cb440d` (tree `183ce21922ea3a90b8fbe87db88323ee5cf070cd`). The production-source comparison to `1faabc0d43e7e8d7056cd48d8cb224e435513ac9` is documentation-only; the inspected Go/config/build-source paths are unchanged. These are conditional preparations, not implementation, item2 dispatch, an integration receipt, or `BUILT` evidence.

**Authority:** the accepted [component implementation plan](../design/readable-config-plan.md) and its full requirement-to-component matrix govern all behavior, ownership, sequencing, gates, and completion evidence. The [accepted design](../design/readable-config.md) and its settled rulings govern behavior. This file adds no policy or architecture and does not replace that plan. `DECIDED` describes the bounded engineering handoff only; it does not admit a writer or authorize code changes.

**Admission fence:** item16's judgment and Root's existing item2-after16 eligibility remain prerequisites. Root must allocate a clean stable integrated base containing the committed producer imports, record its actual SHA/tree, and grant the individual writer fences below. The four jobs are not an alias for item2, do not claim item2 completion, and cannot begin against an unadmitted `SKETCH` or against the current main-only source. No fresh source-wide research, model/adviser round, or implementation review is requested here.

**Source custody:** published current source is `cea3797a2ad0d5691e39152043b65ec659cb440d`; the committed producer lookup source is `d8ed0c9dd820886d249c66d662474673cd2b2c7a` (tree `72e5f49583d488b2929c7f79a682f5cd30af888d`). D8 is a private lookup snapshot, not main, not the eligible integrated base, and not all44 acceptance. Its reported delta is 55 paths, +8,549/-375; those counts describe that source snapshot only. Its receipts are not rerun or independently accepted here. Import only the required committed deltas into Root's post-Mac base; do not check out or import its parent wholesale.

The post-Mac source comparison and symbol inspection confirm current host protections that later activation must preserve: `internal/cli/host.go` calls `hostClaudeView`, `prepareHostSessionHomes`, and retains leases across exec; `internal/packdecl/contributes.go` carries and copies `SandboxTmpdirEnv`; `internal/packdecl/packdecl.go` has the generic install descriptor. D8 changed `contributes.go` for its metadata work but did not change the generic descriptor file. These are integration constraints, not license to replace current Mac behavior with a pre-Mac candidate.

## Shared writer contract

- **Preconditions:** run only after Root's explicit admission of a clean stable base with the named predecessor commits/API. Verify the owned file fence is clean and named symbols remain. Record base SHA and tree before editing.
- **Stop conditions:** a foreign dirty owned file, missing API/symbol, a test that is not red as specified, changed semantics, an unrepresentable contract, an out-of-fence edit, or an out-of-scope gate failure stops the job. Return exact evidence to Root; do not stub an absent dependency, guess an API, weaken a test, or enlarge ownership.
- **Authority:** accepted design behavior wins; current tree facts win over this map; this handoff is advice and may be stale. Do not twist code to match it. Factoring/names are implementer choices unless a typed seam is explicitly required below.
- **Execution:** each writer owns focused red-to-green tests and one coherent commit; format only changed Go files. Commands below are future gates, not claimed runs. Keep ordinary HOME and shared cache defaults; no install/deploy, source-bundle build, agent/API calls, or permission changes.
- **Return:** base and resulting commit/tree; changed paths and `git diff --stat`; exact red and green commands/results; affected all44 matrix rows; residual limitations; no staged files. Root owns serial integration and the single end review per lane.

## Four conditional, disjoint implementation jobs

### I2a — consume D2 row metadata in real folds and truthful injection attribution

**Order:** after D/E-I producer imports; independent of H and J/check runtime work. This is the first small consumer unit, not general I2 completion.

**Fence:** `internal/packload/packload.go` (selected posture/launch/injection hunks only); `internal/packoverlay/packoverlay.go` (metadata selection hunks only); `internal/entrypoint/shell.go` (injection attribution rendering only). Tests: existing `internal/packload/launchflagposture_test.go`, `launchinjectionrecord_test.go`, `internal/packoverlay/postureoverlay_test.go`, `tomlcomposition_test.go`; new `internal/packload/readableflagfold_test.go`, `internal/packoverlay/readableposturemetadata_test.go`, and `internal/entrypoint/readableflagdisclosure_test.go`. Root transfers the shell attribution hunk from general entrypoint ownership only. No temp/install/security/boot edits; `host.go` remains excluded because it already consumes common disclosure lines.

**Reuse and change:** consume D's actual manifest row `Origin`, `Location`, and `Replace` metadata for both own- and foreign-surface posture folds. New-format repeated-bin contributions append by default; explicit replace replaces; the later legacy row retains legacy replacement behavior. Preserve ordered flag tokens and identical-flag suppression; do not invent a new argv parser or de-duplicate user arguments. Reuse `agentcfg.Overlay.AppendArrays` and the existing Compose/RMW path, not a second composition engine. Remove byte-rematching only where authoritative metadata supersedes it. Injection records must attribute the flags actually added to their real contributors and source rows: a final singular `Pack` cannot truthfully attribute a multi-pack append. Existing consumers keep compiling; do not silently label plural provenance as singular. Physical source paths derive from `Pack.Root` plus the authored manifest row, not the staged slug.

**Tests first:** exercise real DecodeTOML → Pack → InjectLaunchFlags with two selected agy packs: permission flags followed by `--model gemini-3.8-flash-medium`; append retains both; `replace = true` resets; both notches gate correctly; a later legacy row still replaces; already-typed flags do not create invented attribution. Duplicate bins within/across packs preserve row order and ownership. Collector → Compose and actual RMW repeated render cover posture-array append/dedup, replace, own/foreign target paths and higher managed precedence. Retain guarded, legacy and identical-suppression regressions.

**Gates:** `go test -mod=vendor -short -count=1 ./internal/packload ./internal/packoverlay ./internal/agentcfg`; focused entrypoint `-run 'Test(RMW.*|.*ReadableFlag.*)'`; `go build -mod=vendor ./internal/cli ./internal/cli/run ./internal/entrypoint`. Run wider entrypoint/callsite tests only if owned changes require them. **Commit:** `feat(pack): fold readable posture metadata with truthful attribution`.

### H1a — path-aware boot-baseline fold, not whole-parent acceptance

**Order:** after B canonical-reader import; independent of I2a, J, and check6. This is not complete H1 inheritance delivery.

**Fence:** `internal/config/drift.go`, `internal/config/driftfold_test.go`; `internal/cli/run/liveapply.go` only the `foldApplied` and returned-applied-value hunks; its existing fold tests and new `internal/cli/run/readablebaselinefold_test.go`. Root transfers that narrow applied-record hunk to H. Mechanics renderer/applyhostskills and unrelated liveapply hunks remain fenced. `snapshot.go` is excluded so G's fatal guard stays untouched.

**Reuse and change:** adapt the existing baseline fold to canonical nested path identity and set/delete exactly that path; update the actual `foldApplied` producer's value/presence record. Preserve missing-baseline refusal and the macos-user session-digest/no-shared-fold guard. Callers and tests use canonical `install.nix` and `machine.ports`; legacy JSONC enters through B. A failed `machine.resources.memory` must not make successful `machine.ports` look failed or mark memory applied by folding the whole parent. Do not add another snapshot composer.

**Tests first:** rewrite the existing `TestFoldBootBaselineKeyReplacesOneKey` expectation to canonical nested data, retaining its unfolded sibling and deletion assertions; do not delete it. Add exact-path set/removal, `install.mise` sibling preservation, missing-record zero-write, direct `foldApplied` mixed success/failure/next-session behavior, baseline-write failure omitted from returned facts, unknown/not-running and macos-user no-write. If the current planner still supplies only top-level change keys, report that narrower limitation for H1b; do not claim nested timing is active everywhere.

**Gates:** `go test -mod=vendor -short -count=1 ./internal/config -run 'Test(FoldBootBaseline|TheDriftDiff|ConfigPartChanged)'`; run focused apply tests with `-run 'Test.*(FoldApplied|ReadableBaseline|BaselineFold).*'`; `go build -mod=vendor ./internal/config ./internal/cli/run`. **Commit:** `fix(config): fold applied canonical paths without accepting siblings`.

### H-check6 — migrate concrete package/GPU check consumers

**Order:** after B canonical reader; independent of I2a, H1a, and J2a. This is a narrow repair, not all of H2. Root explicitly transfers these check hunks from K's broad check fence to H for this job; K must not edit them concurrently.

**Fence:** `internal/config/derived.go` (`EffectivePackages` only), new `internal/config/readablederived_test.go`; `internal/cli/check/sections_devices.go` (`gpuConfig` and directly affected diagnostic hunks), `section_packageprofile.go`; tests `guestnotch_test.go`, `inheritedscope_test.go`, `section_packageprofile_test.go`, and new `readabledevices_test.go`. No broker/model-catalog/check-dispatch edits, image/backend or `host.go` changes.

**Reuse and change:** concrete check consumers read canonical `install.nix` and `machine.gpu`, including bool and rich-object forms. Preserve rich package objects/platform filtering/AMD VAAPI additions; baked-image gate versus runtime/platform; host packages inert by design; unsupported Linux guest report; inherited absent-key silence; and in-jail host-fact reporting. Do not expose the private validation projection as a runtime map or mutate canonical input. Legacy fixtures remain explicit format 1 and are normalized by standard readers. Direct old-map unit fixtures must be deliberate compatibility cases, never TOML aliases.

**Known red → production owner:**

| Reported red | Narrow repair |
| :--- | :--- |
| `TestCheckOnLinuxDoesNotResolveTheGuestNotchToMacosUser` | Canonical `EffectivePackages` lookup to section-package-profile count; keep Linux guest mechanism unchanged. |
| `TestInJailCheckDoesReportThoseKeysWhenPresent` (GPU) | `machine.gpu` lookup to the same in-jail host-fact section. |
| `TestPackageProfileGateIsTheBakedImageNotThePlatform` | Canonical package count; retain `PrimBakedImage`. |
| `TestPackageProfileReportsHostPackagesAsInertByDesign` | Canonical count to existing host-inert info branch. |
| `TestPackageProfileReportsHostPackagesAsInertWhateverTheRuntime` | Same; do not infer runtime at host. |
| `TestPackageProfileWarnsThatNothingMaterializesTheDeclaredPackages` | Same count to no-materialization warning. |

**Tests first:** retain all six cases. Add standard `LoadConfig` → actual check-caller pairs for TOML and legacy, rich nix/platform/VAAPI fixture, `machine.gpu` bool/object, filtered absent-GPU silence, and unchanged `SnapshotJSON`. A changed diagnostic must preserve asserted backend behavior, not blanket-delete literals. Keep B's authored-refusal regression separate.

**Gates:** `go test -mod=vendor -short -count=1 ./internal/cli/check` (all six must pass; report other failures); config focused `-run 'Test.*(EffectivePackages|ReadableDerived|StandardReaderCommonValidator).*'`; `go build -mod=vendor ./internal/config ./internal/cli/check ./internal/cli/run`. **Commit:** `fix(check): consume canonical install and machine declarations`.

### J2a — explicit-target effective edit verification on existing J1

**Order:** after committed B/F/G imports. The byte-edit J1 library exists on the lookup snapshots; do not rebuild J1. This first job targets an explicit existing file only. Automatic origin-target resolution is later B→J work and must not be inferred from display strings.

**Fence:** new `internal/configedit/effective.go`, `effective_test.go`, `effective_layers_test.go`; modify `internal/configedit/apply.go` only if needed for the pre-write validation/reread seam, and its `apply_test.go`. No config schema/provenance/CLI changes. New API names are implementer factoring choices, not claims about existing API.

**Reuse and change:** add a layer-aware adapter for a selected explicit file/workspace/scope plus mutation. Validate intended effective result before touching bytes; use real standard readers and shared F/G semantics where applicable; install effective recomposition into existing `Apply`'s locked verification. Recompute expectations from the lock's reread preimage, not stale outer reads. Dry-run/no-op still validates without writing. Reuse `SnapshotJSON`; no second parser, serializer or filesystem writer. Preserve J1's restoration. The adapter cannot grant approval or access host user files from a jail.

**Tests first:** explicit symlink target in user include/config.d chain; project/local after included layers; only target bytes change, unrelated comments/other files remain byte-identical, stale `ExpectedBytes` refuses. Effective change differs from isolated document change under union. Cover `[]` clear, table-only `off`, literal `env.set.X = 'off'`, `env.unset`, explicit native, bad key/type/user-only scope refusal. After-write effective mismatch restores exact original. Two processes serialize and second expectation comes from reread state. Do not parse formatted `Sources.Locations` or trust `LaunchOriginsJSON` labels for automatic target authorization: they are diagnostic-only; B must transfer an actual typed writable-origin contract before J2b.

**Gates:** `go test -mod=vendor -short -count=1 ./internal/configedit`; `go test -mod=vendor -race ./internal/configedit`; focused B/F/G producer regressions for sections actually touched; `go build -mod=vendor ./internal/configedit`. **Commit:** `feat(config): verify explicit targeted edits against effective layers`.

## Serial integration and remaining all44 delivery

These four jobs do not discharge the accepted plan's complete matrix or follow-ons. Every all44 requirement row remains owned by the accepted plan; Root records row-level candidate evidence only after integration.

1. **Item16 → eligible item2 / Root custody:** import D8's committed deltas onto post-Mac source; retain the current Mac `SandboxTmpdirEnv` field/copy, generic install descriptor, `hostClaudeView`, selection environment, session-home preparation/retention, and credential-view behavior. In particular, extend current `internal/cli/host.go`; do not paste a pre-Mac host/provider candidate. Preserve `internal/cli/hostlaunchcontext.go`. Do not overwrite current entrypoint tmpdir/security/fork-launcher changes, source dependency/vendor state, or `packs/claude/pack.json`. Record actual imported APIs and targeted producer/Mac/strict-release regressions. This handoff does not say item16 was cleared.
2. **Root integration:** after a real item16 judgment and eligible item2 path, allocate a stable import SHA/tree and the four disjoint fences above. I2a, H1a, check6, and J2a may proceed in parallel only after those prerequisites. Integrate serially; rerun impacted callers on each import. One end review per lane; no universal all-green runtime-base barrier and no fresh consensus/review rounds.
3. **H1b after canonical contracts:** finish nested-schema census/filter, TOML generated user files and origins syntax, preflight/nested delivery/cleanup, record/assembled/boot canonical JSON, own-workspace shortcut, explicit user-layer recursion, digest-bound host provenance and Mac stale-session safety. Library record conversion/read/write seams precede L2. H1a alone is not inheritance delivery.
4. **F/G/H2 coordinated activation:** F connects actual profilechannel, provider env files, launch/attach assembly and host receivers to producer semantics, typed flags/native/default/profile gates and context. Keep one host composition and existing services/adapters/options/subagents. Declaration never implies selection. G adds the approved typed extra-repository carrier through approval record, effective/live scope, git/gh use and withdrawal; `AdditionalRepositories` is requested data, not approval. Test each broker independently for no-Git/no-remotes/explicit extras, decline/no-TTY zero-grants, drift/removal/revocation, and fatal-before-write. Anonymous HTTPS-first remains; private live-read recheck, SSH and each push remain hard scope/grant preconditions. H owns launch/attach/apply and actual outcomes; failed/unknown/not-running operations cannot count as applied. Attach reads the running staged tree and never restages. No second facts renderer.
5. **J2b and L1/L2:** a typed writable-origin contract precedes automatic section target/removal. H1 record APIs plus J locks and stable B/D/E/F/G/I semantics precede whole-graph migration. Do not admit L1 before the migration package exists. L1/L2 preserve every comment (including templates), canonical equality through both readers, physical symlink targets, includes/backups, preflight/collisions, zero writes on loss and dual-reader window. Missing optional files are reported; an unrepresentable per-repository grant gives exact differences and no writes. No file is merged and no authority is blessed.
6. **K/M/N/O remain mandatory:** K owns the registry/six surface verbs, old refusal/help, named edits through existing approval, init/no-scaffold/info-exclude, and H-owned launch migration offer/manual `convert_later`. M owns shipped TOML config/manifests/nested human-authored loophole assets, modes/hints/embed/bundle census and strict-release fixtures; preserve post-Mac fields. N owns `config_ref` and overlapping references/userguide/README/AGENTS/help/advice, routing owner-rendered text through its owner and refreshing the completion census when required. O owns serial E2E editing/migration/launch/attach/apply and both-backend/broker cases. The full accepted all44 matrix remains binding; no row is removed because this packet has four small units.
7. **Root acceptance only:** one final clean combined `just check-ci` and full integration run after all implementation is integrated, plus matched absolute launcher/nested validation, source bundle and completion receipt where the accepted plan requires them. Nested podman cannot certify rootless-only or reachability behavior; a real host/CI result is needed for those claims. No item24 source-Goal-2 advancement, BUILT status, or all44/native claim is made here.

## Validation limits of this handoff

This is a conditional preparation document. It did not execute tests, builds, whole-tree gates, integration, runtime/API probes, native checks, or implementation review. Existing D8 receipts are source provenance only, not independently accepted evidence. Reported H/check reds are prior observed findings, not reproduced here. Writable-origin typing, physical authored list-index attribution, and the approved typed extra-repository record remain engineering seams to be delivered by their named predecessors; they are not new owner questions or product decisions. No implementation, profile/config, source, tests, generated census, or user-home files were changed by preparing these briefs.
