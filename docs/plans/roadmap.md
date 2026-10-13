---
title: "Roadmap"
status: accepted
stage: CURRENT
tags: [roadmap, priorities]
summary: "The order in which yolo-jail's open work should be taken up, with the reason for each place; each linked document owns its own state and next step."
vantage:
  status-chip: true
---

# Roadmap

**Reconciled:** 2026-10-13

This page owns the order in which open work is taken up, and why. Each linked document owns its state, next step and
gates; read it before starting, because a place on this list is not permission to build.

## Ordering basis

**Verified live defects first**, each found in the code during this reconciliation, with fixes an agent can make now
ahead of fixes awaiting a ruling. **Then work that unblocks other work**: a ruling or step holding a later build.
**Then smaller independent steps**: builds, measurements, re-verification, graduation. **A ruling that only closes a
document ranks last.** Work needing a Mac, a live human session, a real rootless host or an outside account follows
under [External waits](#external-waits).

## Immediate continuation

Take these current launch problems before the older queue below. Each source document owns
candidate state, acceptance gaps and verification; a place here does not make its code landed.

1. [Enable profile-served daemons on attach without ending sibling sessions](../design/attach-daemon-activation.md)
   — the reported late Bedrock selection reaches a restart prompt; settle additive activation
   before treating restart as the remedy for an already-authorized missing daemon.

2. [Finish ongoing launch preparation and explicit update-and-wait](../design/launch-update-preparation.md)
   — reuse the admitted-build pipeline before optimizing isolated waits; its
   [tests-first source handoff](../design/launch-update-preparation-plan.md) starts with ongoing
   tree scheduling, then freshness and adapters; only ordinary warm CLI/MCP replacement stays in the
   [capture sketch](../design/launch-program-capture-plan.md).
   [Source research](../research/launch-preparation.md) bounds current timing claims.

Pi's profile-set extension is built, children of pi-subagents included; what it cannot close is
listed as accepted limits in
[its design's §3.4](../design/simultaneous-auth-and-pack-isolation.md#34-what-the-extension-cannot-close),
and the one call left there, a pi-subagents registry several extensions can share, is in item 28.

Native readiness and credential-store work retain their places below: assess
[the privilege prerequisites](../research/sandvault-macos-privileges.md) without broad root
cleanup, and rule [keychain scope/unlock/migration](../design/keychain-from-a-jail.md) without
sharing host credentials. Keep [storage relocation](shared-tool-store-relocation.md) and
[OrbStack](../research/orbstack-as-a-podman-host.md) separate from these urgent launch repairs.

## The queue

1. [Finish the patched-fork mode's verification](../design/patched-forks.md), with [its pi-extension
   companion](../design/patched-extensions.md) — the compatibility release is published; keep the remaining
   verification ahead of dependent work. Beside it: [the build line shown by its
   digest](../reference/report-tiers.md#why-its-this-way), built 2026-10-08 with `yolo pack status`; [whether a Mac fork build
   installs its base's `node_floor` first](../design/forked-programs-as-packs.md#OQ-FP11); his test with the migration kit.
2. [Per-jail host daemons (HD-R1)](../design/host-daemon-ownership.md) — built on branch
   `worktree-agent-a1e0c197b66474918` and held, not landed: it awaits the maintainer's choice of how old jails coexist
   with it. Next because it retires the machine-wide credential daemons; [who keeps Claude's login fresh with no jail
   running](../design/host-daemon-ownership.md#OQ-HD9) is still to rule, with the design's two other questions.
3. Rulings that hold a build: [whether a project's mise pin of `pnpm` stays hidden](../design/program-delivery.md#OQ-PD19),
   with [autoprune's `$GOBIN` class](../design/program-delivery.md#OQ-PD20) and [a pack's agent-CLI pin](../design/program-delivery.md#OQ-PD21);
   [whether an agent CLI updates once per machine](../design/program-delivery.md#OQ-PD23), as each workspace downloads
   every release itself; [how a host daemon hands the terminal a line](../design/boundary-broker.md#OQ-BB10), which the
   broker's no-notifier notice waits on; [whether a pack service's in-jail daemon is disclosed](../design/trust-paths.md#OQ-TP11),
   as no launch line names the wire bridge; and [whether copilot and oh-omp alone get `-p bedrock`](../design/bedrock-plumbing.md#OQ-BR24).
4. Rulings that hold a defect's fix: [the Kilo special-casing](../design/gateway-provider-packs.md), while claude gives
   every Kilo model without a declared window a 1M-token context; [`--no-daemon` for in-jail `codex`](../research/codex-background-service.md#OQ-CDX3),
   since `codex agents` starts the daemon yolo turns off; [whose OpenAI login host pi uses](../design/pi-host-openai-auth.md),
   since its first refresh replaces yours, with its catalog-refresh question; and [a leftover `mcp-adapter.json` at the
   host](../reference/mcp-configuration.md#OQ-MC2), which host apply never deletes and pi keeps loading, with
   [OQ-MC1](../reference/mcp-configuration.md#OQ-MC1).
5. [Rule whether a successful `yolo host apply --assert` names a next command](../reference/happy-path-principle.md#the-rules)
   — rule 2 calls such a success a dead end, `--assert` ends at its counts, and the line waits on a ruling against [the
   verdict block](../reference/report-tiers.md#the-verdict-block)'s "Only the dry run has a footer"; this entry is the
   work's only home. Rule [the dry run's exit](../reference/report-tiers.md#OQ-RO8) with it: the code exits 1 where
   [OQ-RO5](../reference/report-tiers.md#why-its-this-way) says 0, and the launch gate reads that exit.
6. [Complete the remaining build-update slices](../design/pi-extension-store-builds.md), through
   [ongoing preparation](../design/launch-update-preparation.md) — the Podman built-tree detached
   slice is in main; host delivery/reporting, Apple Container check-only, fork/capture timing and
   native/performance acceptance retain their scope rather than disappearing into a tree-only fix.
7. [Rule how the Claude footer moves into a Claude Code plugin](../research/claude-code-extensions-footer.md), with [how
   yolo manages Claude Code mods](../research/claude-code-mods-management.md) — the plugin ends a process per refresh,
   and the mods rulings rest on that route.
8. Rule [the VM-local disks design](../design/vm-local-volumes.md): [default or opt-in](../design/vm-local-volumes.md#OQ-VL1),
   [`~/.cache`](../design/vm-local-volumes.md#OQ-VL2), [database folders](../design/vm-local-volumes.md#OQ-VL3) and
   [how a new disk fills](../design/vm-local-volumes.md#OQ-VL4),
   with [the probe that checks its premise](../research/apple-container-file-cost.md#5-a-planned-probe-on-the-mac-runner)
   and [VZ's memory request](../research/macos-vm-memory-reclaim.md#6-draft-feedback-assistant-request) to file — the
   largest Mac speedup measured waits on it (pip install 8.35 s on a shared folder, 2.01 s on Apple Container's own disk).
   Beside it, the maintainer's 2026-10-08 request: [tools from a private Homebrew tap in every jail](../design/private-tap-programs.md),
   whose build waits on [a pack or a config key](../design/private-tap-programs.md#OQ-PT1),
   [brew's copy at the host](../design/private-tap-programs.md#OQ-PT2), [the version policy](../design/private-tap-programs.md#OQ-PT3),
   [never `brew tap`](../design/private-tap-programs.md#OQ-PT4) and [a launch without the token](../design/private-tap-programs.md#OQ-PT5).
9. Approve [the drafted rewordings](../research/macos-backend-performance.md#9-corrections-the-results-feed) of
   [the direction's ruled sentences](../reference/macos-no-vm-direction.md#what-the-numbers-bear-on) — the
   benchmark's other corrections are made, and the two-jail test passes in CI again.
10. [Time what a launch's spans leave out](../reference/perf-logging.md) — built 2026-10-08: the provisioning stage
    writes its duration to the jail perf log, and the Apple Container delivery test reads every dotted span. That test's
    Mac half is compiled, not yet run; one Mac run shows the build skip's saving.
11. Record the Mac runs of 2026-10-02 and 2026-10-03 where each doc's `next` names them — agent work now, ahead of the
    rulings below because the context-mount cases free one: [the AWS doorway](../design/host-notch-services.md), [its
    Bedrock design](../design/sso-backed-bedrock.md), [A2's twin](macos-revival-and-distribution-plan.md), [the
    render-mark twin](handoff-guest-notch-macos.md) and [its design](../design/notch-scoped-config-contributions.md#43-render-mark-parity-on-macos-user),
    [item 1's twin](handoff-macos-user-open-threads.md) and [its runbook row](runbooks/macos-user-manual-checks.md),
    [DP-B6](../design/declaration-parity.md), [the footer](../design/agent-footer.md#what-i-would-build-in-order), [the
    build sandbox](../design/macos-user-build-step-threat-model.md), [the context mounts](../design/context-mounts.md#4-staging-and-the-tests-that-pin-each-piece),
    [Cachix](handoff-cachix-cache.md), [the in-VM copier](../research/macos-layer-reusing-image-delivery.md), [the login
    seed](../design/base-home-legacy-state.md) and [the storage classes](../design/durable-scratch-space.md).

    Review [the bounded privilege lifecycle](../design/macos-user-privilege-lifecycle.md), informed by
    [SandVault's setup-authorized macOS privileges](../research/sandvault-macos-privileges.md),
    with the native readiness work: reported startup denials and exit prompts expose prerequisites
    that account existence and compilation do not establish. Borrowing its setup/runtime split
    must not authorize arbitrary root operations or account-wide termination of sibling workspaces.
12. [Rule the macos-user workspace root](../design/configurable-workspace-root.md), [its whitelist](../design/configurable-workspace-root.md#OQ-CW2)
    first — a `/var/root` home passes until it replaces the blacklist, and tightening later withdraws what users rely on.
13. [Decide whether a workspace config may feed host dotenvs and mounts into a jail](../research/agent-safehouse.md#OQ-AS3)
    — a committed config still can, and notch convergence's `env_sources` item waits on it.
14. [Rule which packs a workspace config may declare](../reference/pack-system.md#OQ-PK1), then [retire `mcp_presets`](../design/mcp-presets-removal.md)
    — the retirement rests on that boundary and on the wrapper's browser report, and ends macos-user's refusal of presets.
15. [Rule the slot split's migration window](../design/slots-and-contributions.md#OQ-D6), then [the manifest
    language](../design/manifest-language.md) with [the slots' other calls](../design/slots-and-contributions.md) —
    `exposes` and [the pi extension-tree rework](../design/pi-pack-extensions.md) wait on the first.
16. Provider rulings later work builds on: [whether the natively-implements rule reaches a list a pack declares](../design/pi-codex-provider-shadowing.md#OQ-4),
    which holds what [its broad reading](../design/pi-codex-provider-shadowing-plan.md) left in place, and [what `-p`
    names](../design/providers-and-profiles-redesign.md), which the provider reference's rewrite waits on.
17. [Notch convergence's held items](notch-convergence.md) — `env_sources` waits on item 13, the one `rmw` rule on [a
    table not declared in full](../design/config-ownership-and-promotion.md#OQ-CO15) (rule [OQ-CO16](../design/config-ownership-and-promotion.md#OQ-CO16)
    with it), and `yolo host -- claude` joining the shared login on the credential view's measures, below.
18. [Rule the provisioner's remaining calls](../design/provisioner-sets.md), corporate-CA trust first — it shipped on its
    leanings under delegation (PS-D10, reversible), so a ruling confirms or undoes what every macos-user launch now does.
19. [Rule what the environment manager promises at each notch](../design/environment-manager-user-stories.md) — Q1b
    decides [`yolo check --at`](../design/yolo-as-environment-manager.md) and Q7 [the Linux guest notch](environment-manager-plan.md).
20. [Rule the keychain from a jail](../design/keychain-from-a-jail.md), which settles [Copilot's machine-wide
    login](../research/copilot-token-storage.md). Review its broader native-credential-store direction with the
    sandbox account's missing-keychain reports: storing suitable tokens responsibly must not wait on Copilot alone,
    and the account's unlock, workspace scope and migration choices need to be considered together.
21. [Rule the starting tab's Ctrl-C in a shared jail](../design/jail-lifetime-last-session-wins.md#OQ-JL10) — the
    keeper at `yolo host` is ruled; its build waits on [the sidecars and doorbell](../design/agent-event-watchers.md).
22. [The boot snapshot and diagnostic dial](../design/diagnostics-past-the-boundary.md) — a refused boot keeps no record
    of the jail's state, and the readiness act built 2026-10-06 adds one more refusal.
23. [Rule the add-only model lists](../design/model-lists-and-pickers.md) and [web search on Bedrock](../design/bedrock-web-search.md)
    — what most agents' menus show turns on the first, and no agent gets search from Bedrock on runtime.
24. [Decide whether pack-declared traps fold into the agent directory map](../design/agent-directory-map.md) — the map
    would supersede [the traps design](../design/pack-declared-file-diagnostics.md), so rule it before building either.
25. [Measure the in-jail nix root watcher end to end](../design/in-jail-nix-roots.md#8-what-is-built) — built
    2026-10-08; it needs one fresh jail on a host running this yolo: a `nix build` in the workspace, then
    `yolo nix-roots list` and the host's `nix-store --query --roots`.
26. [Review H4's root-owned capture store on macos-user](install-capture.md#build-order), [rule whether integration
    sharding unparks](integration-parallelism.md) with [the test suite's other levers](test-suite-speed.md), and [name
    the real forked program](../design/forked-programs-as-packs.md), the last input its step 7 needs.
    Beside suite speed, [make completion change-aware and read-only](../design/change-aware-completion.md),
    with [its sketch](../design/change-aware-completion-plan.md) and [input research](../research/completion-check-inputs.md)
    — avoid irrelevant repeated gates without changing source landing or CI coverage.
    Keep its [unconnected component experiment](../../tools/completion-experiment/README.md) separate
    from production completion; input closure, baseline publication and command dispatch remain open.
27. [Rule how a jail reaches a worktree's `.git` outside it](../research/herdr-integration.md), with the other herdr calls
    — measured: a read-only bind refuses every commit, a read-write one lets a jail prune the worktree.
28. Calls left at one decision each, each with its facts and a leaning: [the `:ro` degradation rows](../design/composed-file-permissions.md),
    [whether messages name the guide's URL](../design/docs-website.md#OQ-DW3), [the macOS nix build sandbox](../design/macos-user-build-step-threat-model.md),
    [array-append pinning](BACKLOG.md#E5), [the pack system's other calls](../reference/pack-system.md), [relocating
    `.yolo` by a link](../reference/jail-home.md#OQ-JH1), [whether to ask pi-subagents for a registry several
    extensions can share](../design/simultaneous-auth-and-pack-isolation.md#34-what-the-extension-cannot-close), [the shared tool store's relocation contract](../design/shared-tool-store-relocation.md)
    with [its build handoff](shared-tool-store-relocation.md), and, once item 11 records its cases, [context sources inside a
    home](../design/context-mounts.md).
    Beside the storage rulings, [rule how one workspace splits across storage tiers](../design/storage-tiers.md), with its
    [implementation sketch](../design/storage-tiers-plan.md) — the maintainer asked for it 2026-10-09, and its disk-presence
    and backend calls are the same as the tool store's, so rule them together.
    Beside them, [isolate ordinary jail caches](../design/cache-isolation.md) — private warm state
    needs safe reclamation alongside it, not another unbounded storage surface.
29. [Rule whether yolo may hold an editor preference](../design/baked-editor-preference.md) — the maintainer asked for
    it, and its first ruling decides an image change every jail pays. Beside it, also asked for by the
    maintainer: [how a SandVault or Safehouse user tries yolo on their existing setup](../design/sandvault-safehouse-compat.md#OQ-NB1),
    then [which backend a SandVault setup gets](../design/sandvault-safehouse-compat.md#OQ-NB2) and
    its two scope calls, [credentials](../design/sandvault-safehouse-compat.md#OQ-NB3) and [the trial's `.yolo/`](../design/sandvault-safehouse-compat.md#OQ-NB5) — the first decides the command
    surface the import and trial build on.
30. [Build the rest of the broker's step 2](../design/boundary-broker.md#11-recommendation-and-the-first-build-slice) —
    first among the builds, as the maintainer put the broker on the plate for the week of 2026-09-28. Beside it,
    [workspace widening](../design/workspace-widening.md) graduates once [OQ-WW1](../design/workspace-widening.md#OQ-WW1) is ruled.
    Also beside it, asked for by the maintainer on 2026-10-09: [rule the paste-to-host stopgap](../design/host-run-mailbox.md#OQ-HX1),
    which removes the copy-back of a host command's output until the broker's store and doorbell exist; that one
    question decides whether anything is built. Whether the broker may later approve such a command from a
    notification is [its own question](../design/boundary-broker.md#OQ-BB14), and waits for the doorbell.
    Also asked for on 2026-10-09: [image paste in every agent](../design/image-paste.md), where [how far Ctrl+V is
    consumed](../design/image-paste.md#OQ-PA2) shapes the key handling, with [closing macos-user's possibly open
    pasteboard](../design/image-paste.md#OQ-PA1) and [the default](../design/image-paste.md#OQ-PA3); its first slice,
    the host reader and `yolo paste`, waits on none of them.
31. Speed builds the maintainer asked for on 2026-10-04: [Apple Container's stock-image skip, then the skip for a launch
    declaring `packages:`](../reference/image-staging-vs-baking.md), as a nix build that finds nothing to do is 3.2 to
    3.5 s of a 6.9 s fresh launch; and [Podman Machines pinned to applehv](../research/macos-vm-runtime-comparison.md#32-on-a-shared-mac-folder),
    as Podman's installer gives libkrun, the slowest shared folder measured. Then
    [make OrbStack a working Podman host](../research/orbstack-as-a-podman-host.md), with [its native verification
    plan](../research/orbstack-as-a-podman-host-plan.md), as its measured shared-folder speed and memory return offer
    another Mac option without restoring Docker first.
32. Builds no ruling holds: [the readiness act on macos-user](../design/jail-notch-readiness.md#JR-D2) — **built 2026-10-06**: host-side admission starts the existing confined stage for an absent/unknown selected program and its generated bootstrap runs the shared readiness act; fixture tests are in `integration/macosuserprogramreadiness_test.go`, with native macOS execution pending; [a provider set's overlay modifier](../design/active-provider-sets.md#13-what-was-built-2026-09-29), which reads only the
    primary; [the empty-list line in `yolo check`](../design/model-lists-and-pickers.md#72-composition-rules);
    [macos-user's item 2 twin](handoff-macos-user-open-threads.md);
    [the color pass's Group B](cli-visual-polish.md); [the unit suite's clock waits](test-suite-speed.md#retaken-2026-10-01-and-which-targets-hold);
    [the Bedrock user-guide recipes](../design/bedrock-plumbing.md#12-what-i-would-build-in-order); [the disk levers'
    re-measure](../design/disk-levers-and-backfill.md); and the pi guide's line on [a package the refresh missed](../design/pi-extension-lifecycle.md).
33. Verify [the exact-commit release gate](../design/pre-tag-release-gate.md) before the next publication request,
    because offline fixtures do not prove live Actions or publisher permissions. What the 2026-10-05 and 2026-10-06
    builds still owe: read the first scheduled macos-user nightly with [the vendor
    installs](../reference/agent-install-in-ci.md), and the linux/arm64 copilot cell for [GitHub's own installer](native-installer-migration.md);
    rule the silent drops [the setup census](../design/backend-parity.md#42-what-was-built-2026-10-05-and-what-it-found)
    found; graduate into system docs [config ownership](../design/config-ownership-and-promotion.md), [pi's package
    folder](../design/pack-pi-resources.md), [the extension model defaults](../research/extension-model-defaults.md), and
    [`packages` attribute paths](../design/package-nested-attribute-paths.md) once a Mac has run a dotted entry.
34. Rulings nothing shipped waits on. Gating one later step: [the host-file permission asymmetry](BACKLOG.md#E2) with
    [its host-side twin](pack-host-management-plan.md), [the skills fan-out](BACKLOG.md#OQ-S4), [sharing a claimed
    generic name](../design/credential-sources-separation.md#OQ-ES6), [the pack-file conventions](../design/pack-conventions.md),
    [loophole env and guest fields](../design/loophole-packaging.md), [workspace MCP files](../design/workspace-mcp-sources.md),
    [Seatbelt deny-default](../research/agent-safehouse.md), [host apply's posture](../design/host-render-target.md),
    [the I/O priority default](../design/io-priority.md). Only closing a document: [an unmatched-reference hatch](../design/reference-mismatch-diagnostics.md),
    [path mirroring](../design/workspace-path-mirroring.md), [pack telemetry](agent-config-packs.md), [AWS's key
    pair](../design/agent-auth-modes.md#OQ-9), [stateful-surface comments](BACKLOG.md#OQ-E4), [the upstream mise
    issue](../design/jail-state-separation-design.md), and [macos-user's stale home-root file and two workspaces at
    once](../reference/macos-user-home-tiers.md).
35. The four calls the 2026-09-26 open-source audit left to the maintainer: whether one public research doc stays
    public, the scratch-directory `.gitignore` entries, a `last-release` recipe from the template, and when to retire
    the Python-version migration; the audit's report is outside this repository, so this is their only record.
36. Decide how far one login is shared — with the host, and between two agents of one provider
    ([universal-login-sharing.md](../design/universal-login-sharing.md)): the machine store already shares an agent's
    login across every jail, the host notch and the pi/Muse pair are the two gaps, and [its sketch](../design/universal-login-sharing-plan.md)
    waits on all three questions. Rule the canonical copy's location with it, so a `yolo` state reset cannot destroy a login.
37. Work the asks a downstream pack consumer is still waiting on ([downstream-asks.md](downstream-asks.md)) — nine
    carries, each with the shape a landed version takes; four are owned by [the patched-fork
    design](../design/patched-forks.md) and [the model-list design](../design/model-lists-and-pickers.md), the
    other four by [the tracker's own questions](downstream-asks.md#open-questions) (one of them a small
    diagnostics build), and one is [already answered](downstream-asks.md#4-a-reduced-window-child).

## External waits

38. One session at a Mac, with `sudo`'s password and the runner stopped, completes [the benchmark](../research/macos-backend-performance.md#8-results)
    — [auto-capture](../design/program-delivery.md#decision-ledger), macos-user's `go test`, Podman Machine on applehv
    and [NFS from macOS's `nfsd`](../research/macos-vm-runtime-comparison.md#6-is-there-an-open-stack-with-faster-shared-folders)
    — first, as [the comparison's refresh](../reference/macos-no-vm-direction.md#when-to-refresh-this) waits on it. It
    also runs `yolo host -- claude` and `yolo host -- agy` for [the host floor's installer agents](../design/host-tool-provisioning.md#HP-D2),
    and a macos-user launch with a dotted `packages` entry, which no CI job runs.
39. [The two-jail check](../research/macos-backend-performance.md#7-found-on-the-way-two-apple-container-jails-may-mount-one-ext4-disk)
    has held since each workspace got its own `/mise` disk: run 37832291377 (2026-10-08, `container` 1.1.0)
    logged `AC-KEEPER sweep-spares-a-kept-jail (JL-D7) VERDICT: HOLDS`, and the sweep is strict since. The next
    `apple-container.yml` dispatch re-asks [the keeper's remaining Apple Container runs](../design/jail-lifetime-last-session-wins.md#7-what-i-would-build-in-order);
    someone at the runner Mac upgrades `container` to 1.5.0 (its kernel prompt is interactive) before the VM-local probe.
40. The next `macos-user.yml` run carries the Mac checks built since 2026-10-04: [I/O priority](../design/io-priority.md#9-what-i-would-build-in-order),
    the macos-user keeper, [the capture fixtures](install-capture.md), the host floor's fixture capture, the Mac floor's
    fork build and [the guest notch](environment-manager-plan.md); each doc's `next` names its test.
41. A human with a subscription login runs [the credential-view runbook](runbooks/claude-credential-view-measures.md)
    for [Claude's login without interception](../design/claude-login-without-interception.md), on a Mac and for a day
    on a rootless host, which releases [`yolo host -- claude` joining the shared login](../reference/claude-oauth-interposition.md),
    and records one browser login and one shared expiry for [the OpenAI service](../design/openai-auth-broker-plan.md).
42. A real rootless Linux host, which a nested jail is not, clears [a real reboot](../design/podman-reboot-readiness.md#testing-and-the-real-host-check),
    [a low-space collection with a jail up](storage-lifecycle.md), [the keeper's scope move and logout](../design/jail-lifetime-last-session-wins.md#8-what-done-looks-like),
    and HD-R1's first launch once item 2 lands.
43. One live agent session per check, which no test may start: [the footer](../design/agent-footer.md#21-as-built),
    [Claude's LSP plugin](../reference/mcp-configuration.md#lsp-claudes-route-is-a-generated-plugin), [the via route](../design/wire-bridge-gateway.md#41-how-it-is-built),
    [menus under a model list](../design/model-lists-and-pickers.md#13-build-order-and-what-done-looks-like), [a switch in
    a provider set](../design/active-provider-sets.md#9-what-done-looks-like), [Copilot's config migration](../design/agent-directory-map.md#74-copilot),
    [a Kilo session](../design/gateway-provider-packs.md), [the restart prompt](../design/attach-skew-and-contract-guardrails.md),
    [pi loading a pack's folder](../design/pack-pi-resources.md), [host Chrome from `yolo host -- claude` and under
    macos-user](../design/mcp-presets-removal.md#13-what-i-would-build-in-order), and [codex, opencode and pi on
    Bedrock](../design/bedrock-plumbing.md#8-behaviour-this-design-fixes) under SSO. Check [Pi launch selection](../design/pi-launch-selection-flags.md)
    and [simultaneous native authentication](../design/simultaneous-auth-and-pack-isolation.md) in a live session;
    launch arguments and source inspection do not establish authentication isolation.
44. A human with the cloud account clears [a Bedrock API key or static key pair](../design/bedrock-plumbing.md#64-the-credential-three-are-supported),
    [an SSO lapse mid-turn](../design/sso-backed-bedrock.md#11-evidence-and-how-to-re-check-it), [settings-only Bedrock mode](../reference/providers.md#two-channels-split-by-payload-type),
    [an AgentCore gateway](../design/bedrock-web-search.md) and [a Claude usage-limit response](../design/wire-bridge-gateway.md#5-part-4--the-subscription-arm-and-opt-in-failover-ruled).
45. The maintainer's own commits and launches clear [the `matt` pack's `briefing/`](../reference/pack-system.md#briefing),
    [its posture list at the host](../design/notch-scoped-config-contributions.md#5-fastest-path-to-the-motivating-case),
    [a dropped pack's host output](../reference/pack-system.md#retiring-a-dropped-packs-host-output), [the fzf
    pack](handoff-fzf-pack-adoption.md), his pack's move to pi's package folder, [the listen-port fix where reported](../reference/wire-bridge.md#what-can-hold-the-listen-port-before-the-bridge-does)
    and [the runner Mac's published-ports change](runbooks/mac-actions-runner.md).
46. A live `llama-server` and one manual turn per agent clear [the `llamacpp` provider](../../packs/llamacpp/README.md),
    whose spellings have never met a server; copilot is the one to try first.
47. The mise use record's first 30-day window gives [the disk footprint's re-measure](../design/minimal-disk-footprint.md)
    its first offer to check, on the host.
48. Nix dropping Intel Macs, when nixpkgs 26.05's security fixes stop on 2026-12-31, brings back [what replaces the
    Intel macOS runner](../research/macos-support-matrix.md#OQ-MX1), deferred until then.
49. The maintainer files the upstream reports drafted with the VM-local design, on [VZ's virtio-fs cache policy](../research/apple-container-file-cost.md#21-per-file-cost-not-bandwidth)
    and [VZ keeping freed guest memory](../research/macos-vm-memory-reclaim.md#5-adding-it-to-apple-container-ourselves)
    — last, since only Apple can ship either fix.
50. Native macOS verification of [JD-10's guest pack-tree lifetime](../design/jail-daemon-on-macos-user-plan.md#JD-10): observe root ownership and modes and Seatbelt execution while overlapping launches use distinct trees, A's daemon restarts from A's unchanged module after B changes or drops its module, and capture leaves both trees unchanged. The source gates passed, but no native execution has established these properties; they remain **UNMEASURED**.

## Boundaries

- **Not committed to:** [`further-roadmap-ideas.md`](further-roadmap-ideas.md), a shelf of unverified candidates.
  **Iced**, with what would thaw each: [`yolo cache relocate`](cache-relocation.md) (someone wanting it), [trust-gated
  grants in a local config](../design/workspace-config-trust.md) (a read-write mount that cannot move into the
  workspace), [in-jail nix on the container Macs](setup-support-gaps.md#2-ranked-gap-backlog) (a user for whom
  `packages:`, mise and macos-user all fall short) and [a central host watcher](../research/central-yolo-watcher.md#7-what-would-have-to-be-true-before-building-one)
  (a duty that must hold a listener across launches).
- **Ids Vantage cannot index**, reached here by name: [Q1 to Q9](../design/environment-manager-user-stories.md#Q1),
  [9.2 and 9.6](../design/host-render-target.md#9.2), [CFP-1 to CFP-3](../design/composed-file-permissions.md#CFP-1),
  [Q2 and Q3](../design/macos-user-build-step-threat-model.md#Q2), [SS-6](../design/jail-state-separation-design.md#ss-6),
  [E1, E2, E5 and CO](BACKLOG.md#E1), [the host-files mode](pack-host-management-plan.md#open-questions), and two
  outside this queue: [SH-1](macos-revival-and-distribution-plan.md#oq-sh-1--is-macos-user-self-hosting-worth-pursuing-at-all-maintainer),
  which blocks nothing, and [JD-4](../design/jail-daemon-on-macos-user-plan.md#decision-ledger), a maintainer follow-up.
- **With no id at all:** [Go comments citing sections by number](README.md#the-dangling-n-citations-in-go-comments),
  [check 2's two marker conventions](README.md#check-2-has-two-known-errors-and-a-convention-question-under-each),
  [the storage lifecycle's `min-free` values](storage-lifecycle.md#open-questions), the unruled half of
  [OQ-ST2](../design/synced-skill-trees.md#OQ-ST2) on user-reserved skill children, and whether a bare `-p` whose
  packs install no CLI should say it selects nothing (`checkProfileTargets`, `internal/cli/run/packs.go`).

## Disposition of open items (2026-10-13)

Written after the build-plan synthesis (its HEAD `4ba625fd0`) and the landing pass that followed it on this branch —
the patched-fork background timing (`ab95926c0`), the broker's first user-scope slice (`c177ab83e`), the startup
banner's dimming (`d8f26fc72`), the unit-suite seams (`1382499b0`), the pi-guide sentence (`a1c876717`) and the
native-addon measurement record (`2f7e7d5ff`). It gives every open item — the immediate-continuation list, the queue
(1–37) and [External waits](#external-waits) (38–50) — exactly one class, against the tree as it stands:

- **A — actionable now**, with the smallest next change named;
- **D — decision needed**, with the exact question and who answers it;
- **X — external/blocked**, with the resource named.

No item is moved, deleted or re-scoped, and [Boundaries](#boundaries) is a shelf, not a queue. Where an item mixes
classes, its sub-parts are named and the cross-cutting ones repeat in the two lists below.

### Immediate continuation

| Item | Class | Disposition |
|---|---|---|
| IC1 — profile-served daemons on attach | D | `OQ-AD1`: activate late-selected profile daemons additively, or refuse conflicting late bridge plans in this slice? **Maintainer** (synthesis leaning: refuse). Today's restart prompt is the ruled `OQ-SK1` disposition, not a defect. |
| IC2 — ongoing launch preparation and update-and-wait | A | Start the tests-first source handoff at ongoing tree scheduling, then freshness and adapters; reuse the admitted-build pipeline before optimizing isolated waits. |

The section's other pointers are not items of their own: Pi's profile-set extension call is item 28, native readiness
and the credential store are items 11 and 20, and storage relocation and OrbStack are items 28 and 31.

### The queue

| Item | Class | Disposition |
|---|---|---|
| 1 — finish the patched-fork mode's verification | A | Run the required nested-jail verification of the patched-fork timing (W9 #2); the digest build line is a small build. #3 is X (a real rootless host or CI), #8–10 are X (a Mac), and `OQ-FP11`/`OQ-PFK5`–`OQ-PFK7` are D. |
| 2 — per-jail host daemons (HD-R1) | D | How do old jails coexist with per-jail daemons, and who keeps Claude's login fresh with no jail running (`OQ-HD9`)? **Maintainer**. Built and held on a branch. |
| 3 — rulings that hold a build | D | `OQ-PD19`–`OQ-PD23` (project mise pin of pnpm; autoprune's `$GOBIN` class; a pack's agent-CLI pin; whether an agent CLI updates once per machine), `OQ-BB10`, `OQ-TP11` and `OQ-BR24`. **Maintainer**. |
| 4 — rulings that hold a defect's fix | D | The Kilo special-casing (claude gives undeclared-window Kilo models 1M context), `OQ-CDX3` (`--no-daemon` for in-jail codex), whose OpenAI login host pi uses, and the leftover `mcp-adapter.json` (`OQ-MC1`/`OQ-MC2`). **Maintainer**. |
| 5 — `yolo host apply --assert`'s next command and the dry run's exit | D | May a successful `--assert` name a next command (happy-path rule 2), and does the dry run exit 0 or 1 (`OQ-RO8`)? **Maintainer**. |
| 6 — remaining build-update slices | A | Next is host delivery/reporting; the fork-timing half landed (`ab95926c0`). Apple Container check-only and native/performance acceptance are X (a Mac). |
| 7 — Claude footer plugin and mods management | D | Move the footer into a Claude Code plugin, and how yolo manages Claude Code mods. **Maintainer**. |
| 8 — VM-local disks and the private Homebrew tap | D | `OQ-VL1`–`OQ-VL4` and `OQ-PT1`–`OQ-PT5`. **Maintainer**. The Apple Container file-cost probe is X (a Mac). |
| 9 — macOS direction rewordings | D | Approve the drafted rewordings of the direction's ruled sentences. **Maintainer**. |
| 10 — what a launch's spans leave out | X | One Mac run for the Apple Container delivery test's Mac half; the rest is built. |
| 11 — record the Mac runs of 2026-10-02/03 | X | A Mac (and the named live-session checks). Reviewing the bounded privilege lifecycle with native readiness is D. |
| 12 — macos-user workspace root | D | `OQ-CW2`, the whitelist, before it replaces the blacklist. **Maintainer**. |
| 13 — workspace config feeding host dotenvs and mounts | D | `OQ-AS3`: do `mounts` and `env_sources` become user-scope-only? **Maintainer**. |
| 14 — which packs a workspace config may declare | D | `OQ-PK1`; the `mcp_presets` retirement follows it. **Maintainer**. |
| 15 — the slot split's migration window | D | `OQ-D6`, then the manifest language and the slots' other calls. **Maintainer**. |
| 16 — provider rulings | D | Does the natively-implements rule reach a pack-declared list (`OQ-4`), and what does `-p` name? **Maintainer**. |
| 17 — notch convergence's held items | D | The one `rmw` rule (`OQ-CO15`/`OQ-CO16`). **Maintainer**. `env_sources` waits on item 13; the shared-login credential view is X (a human session). |
| 18 — the provisioner's remaining calls | D | Corporate-CA trust first; it shipped on leanings under delegation (PS-D10, reversible). **Maintainer**. |
| 19 — what the environment manager promises at each notch | D | Q1b (`yolo check --at`) and Q7 (the Linux guest notch). **Maintainer**. |
| 20 — the keychain from a jail | D | Keychain scope, unlock and migration, which settles Copilot's machine-wide login. **Maintainer**. |
| 21 — the starting tab's Ctrl-C in a shared jail | D | `OQ-JL10`; its build then waits on the sidecars and doorbell. **Maintainer**. |
| 22 — boot snapshot and diagnostic dial | D | `OQ-DB2`: is the boot snapshot always-on (leaning: on give-up paths only)? **Maintainer**. The §4.6 byte-cap adoption on the eight unbounded sinks is a separate A fragment. |
| 23 — add-only model lists and Bedrock web search | D | Rule the model lists and web search on Bedrock. **Maintainer**. |
| 24 — pack-declared traps vs the agent directory map | D | Does the map supersede the traps design? Rule before building either. **Maintainer**. |
| 25 — in-jail nix root watcher measurement | A | Run a workspace `nix build`, then `yolo nix-roots list` and `nix-store --query --roots`. Run isolated: it mutates `/workspace`. |
| 26 — H4, suite levers, the forked program, completion | D | Name the real forked program (source, build line and `produces`) for step 7, and rule whether integration sharding unparks. **Maintainer**. The H4 review of the root-owned capture store is X (a Mac); the native-addon measurement is recorded as blocked on a build line (`2f7e7d5ff`). |
| 27 — how a jail reaches a worktree's `.git` | D | Read-only refuses every commit, read-write lets a jail prune the worktree. **Maintainer**. |
| 28 — calls left at one decision each | D | The `:ro` degradation rows, message URLs (`OQ-DW3`), the macOS nix build sandbox, array-append pinning (E5), the pack system's other calls, relocating `.yolo` by a link (`OQ-JH1`), a pi-subagents shared registry, the shared tool store's relocation contract, storage tiers, and cache isolation. **Maintainer**. |
| 29 — editor preference and SandVault/Safehouse compat | D | May yolo hold an editor preference, and how a SandVault/Safehouse user tries yolo (`OQ-NB1`, then `OQ-NB2`/`OQ-NB3`/`OQ-NB5`)? **Maintainer**. |
| 30 — the rest of the broker's step 2 | A | Next slice is the request store and grants (§7) with `yolo approve`; the first user-scope slice landed (`c177ab83e`). `OQ-BB10` is D; the D-Bus notifier is X (a Mac) and the answer ping waits on `yolo notify`. |
| 31 — speed builds the maintainer asked for | X | A Mac: Part A's `container image tag` argv is unverified from Linux and only a real Mac dispatch proves it. |
| 32 — builds no ruling holds | D | The W1(e) `yolo run` progress golden sign-off (human approval of the frozen `testdata`). **Maintainer**. Sub-parts already closed: the startup banner (`d8f26fc72`), the unit-suite seams (`1382499b0`) and the pi-guide line (`a1c876717`); (b)/(c)/(g) are built with stale lines. Still A: the Seatbelt 0644 test (writable off-Mac). Still X: the Mac readiness run and the quiet-host re-measure. |
| 33 — the exact-commit release gate | X | Live GitHub Actions and publisher permissions (offline fixtures cannot prove them), plus the first scheduled macos-user nightly on a Mac runner. |
| 34 — rulings nothing shipped waits on | D | The host-file permission asymmetry (E2), skills fan-out (`OQ-S4`), shared claimed names (`OQ-ES6`), pack-file conventions, loophole env/guest fields, workspace MCP files, Seatbelt deny-default, host apply's posture, and the I/O priority default. **Maintainer**. |
| 35 — the four calls from the open-source audit | D | The audit report is outside this repository, so this is their only record. **Maintainer**. |
| 36 — how far one login is shared | D | Sharing with the host and between two agents of one provider, plus the canonical copy's location so a state reset cannot destroy a login. **Maintainer**. |
| 37 — downstream pack consumer asks | A | The one small diagnostics build among the tracker's four questions; four carries are owned by items 1 and 23. |

### External waits

All of 38–50 are X; each names the resource that unblocks it.

| Item | Resource |
|---|---|
| 38 | A Mac, with `sudo`'s password and the runner stopped (also `yolo host -- claude`/`agy` and a dotted `packages` entry). |
| 39 | A Mac / the next `apple-container.yml` dispatch, with `container` 1.5.0 at the runner. |
| 40 | The next `macos-user.yml` run on a Mac runner. |
| 41 | A human with a subscription login — a Mac and a day on a rootless host. |
| 42 | A real rootless Linux host (a nested jail cannot see the class) or CI. |
| 43 | One live agent session per check (no test may start one). |
| 44 | A human with the cloud/AWS account. |
| 45 | The maintainer's own commits and launches. |
| 46 | A live `llama-server` and one manual turn per agent. |
| 47 | The mise use record's first 30-day window, on the host. |
| 48 | Time — nixpkgs 26.05's support ending 2026-12-31; not before. |
| 49 | The maintainer files the upstream reports; only Apple can ship either fix. |
| 50 | A Mac — native execution of JD-10's guest pack-tree lifetime. |

### Decisions needed (all answered by the maintainer)

The ruling set, in the order it unblocks work:

- `OQ-DB2` — is the boot snapshot always-on? (item 22)
- `OQ-BB10` — how a host daemon hands the launching terminal a line (items 3, 30)
- `OQ-AD1` — activate late profile daemons additively, or refuse conflicting late bridge plans? (IC1)
- The motivating fork's name — its source, build line and `produces` list (item 26)
- `OQ-FP11`, `OQ-PFK5`, `OQ-PFK6`, `OQ-PFK7` — the four open patched-fork rulings (item 1; the last two filed 2026-10-13 by a downstream consumer)
- The W1(e) `yolo run` progress golden sign-off (item 32)

Plus the queue's remaining rulings: items 2, 3, 4, 5, 7, 8, 9, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 23, 24, 27, 28, 29, 34, 35 and 36.

### External resources

- **A Mac** — items 1, 8 (probe), 10, 11, 17 (credential view), 26 (H4), 31, 32 (readiness), 38, 39, 40, 41, 50.
- **A quiet host** — the W1(f)/W1(h) re-measures (items 32 and 8).
- **A real rootless host or CI** — items 1 (#3), 39, 40, 42.
- **A live human session / account** — items 41, 43, 44, 46.
- **The maintainer's own machine, commits or upstream filing** — items 45, 47, 48, 49.

### Actionable now, in land order

The remaining A work, cheapest first: item 32's Seatbelt 0644 test → item 22's byte-cap fragment → item 37's small
diagnostics build → item 30's request store and grants → item 6's host delivery/reporting → IC2's ongoing tree
scheduling → item 1's nested-jail verification (after item 6's landed half) → item 25's measurement **last and
isolated**, as it mutates `/workspace`.
