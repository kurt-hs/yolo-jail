---
status: draft
stage: SKETCH
next: "Resolve the design's MCW2/MCW3 questions and obtain disposable native layout/Seatbelt evidence; then rebase this engineering sketch before a build handoff"
depends-on:
  - macos-user-concurrent-workspaces.md#OQ-MCW2
  - macos-user-concurrent-workspaces.md#OQ-MCW3
tags: [macos-user, workspaces, sketch]
vantage:
  status-chip: true
---

# Concurrent native workspaces — engineering sketch, not a build handoff

**Status:** 2026-10-21 — UNBUILT / NOT A BUILD HANDOFF. The initial reader inventory was checked against
`31d61546ccf7aa5ae763f4e5541011e007e5cb10`; the focused follow-up below adds three omitted HOME consumers identified in review and checked
against the source candidate `44c73a75db21bb5df84e356c83751d03d1ff88b2`. No macOS runtime trial was
performed. This bounded engineering investigation advances the selected architecture without completing the implementation
plan or authorizing code, account setup, permissions, credentials, or cleanup changes.

**Design:** [private workspace homes under one guest UID](macos-user-concurrent-workspaces.md#selected-architecture-and-boundary).
**Precedence:** the design wins on behavior; the tree wins on facts; this sketch is advisory and
must be refreshed at handoff. The [sharing ruling](macos-user-concurrent-workspaces.md#decision-ledger)
leaves backing/link geometry to engineering. The [home-tier ruling](../reference/macos-user-home-tiers.md#decision-ledger)
is settled, not an additional owner gate.

> [!IMPORTANT]
> **Do not re-open:** exactly one `_yolojail` OS user; stable private workspace HOME and persistent
> state; kernel denial of sibling home/state for confined processes; only selected pack machine
> scope and existing configured sharing. Accounts per workspace and waiting/refusal as the final
> concurrency policy are not alternatives. Keep today's account-home hold until its replacement
> is implemented and kernel-verified. This sketch answers neither
> [OQ-MCW2](macos-user-concurrent-workspaces.md#OQ-MCW2) nor
> [OQ-MCW3](macos-user-concurrent-workspaces.md#OQ-MCW3).

## Reader inventory at the checked tree

These are existing readers/writers, not an approved edit map. A future planner must carry one
workspace HOME consistently through them; merely changing the final agent's environment misses
bootstrap, stage admission, read-only protection, daemons, and session-home cleanup.

| Reader / writer | Checked source and present coupling | Engineering consequence |
| :--- | :--- | :--- |
| Account identity and directory-service home | [`SandboxHome`, `CreateUserCommands`, `ProvisionHomeCommands`](../../internal/macosuser/macosuser.go#L73-L159) keep `/Users/_yolojail`, including `NFSHomeDirectory`. [`darwinBootstrapIdentityRefusal` and scope guard](../../internal/cli/internal.go#L350-L467) deliberately use that account root, not rebound HOME. | Keep identity/setup and credential-boundary checks anchored to the account. Do not repoint the directory-service record per launch or replace its identity check with an agent-editable private-home owner check. |
| Runtime HOME and protected environment | [`LaunchArgvWithGuard`, `sandboxEnvPairs`, `SandboxPath`](../../internal/macosuser/macosuser.go#L1307-L1448), [`protectedSandboxEnvKeys` / `ProtectedSandboxEnvNames`](../../internal/macosuser/envfile.go#L285-L315) and [`DarwinBootstrapArgv`](../../internal/macosuser/runplan.go#L364-L397) set/protect HOME, PATH, mise and login PATH across `env -i`. | Thread the same workspace HOME into launch, provisioning, service/daemon environments and bootstrap. Keep secrets in the existing session env file, not argv. Do not use `sudo --login`. |
| Selected credential-store machine backing | [`run.go`](../../internal/cli/run/run.go#L1052-L1058) derives the selected Claude secure-storage directory from the account home's machine-scope root and places that path in the launch environment. | Retain this selected machine backing and its established path when HOME is rewired. Do not mechanically relocate every `SandboxHome()` use; this credential-store backing is deliberately distinct from workspace-private HOME. |
| Plan and pre-install readiness | [`buildPlan`](../../internal/macosuser/orchestrator.go#L655-L676) computes HOME/PATH and probes programs; [`BuildRunPlanWithStages`](../../internal/macosuser/runplan.go#L803-L828) separately supplies bootstrap HOME, launch defaults, native session root and profile HOME. | Resolve once before probes and plan assembly; no readiness probe through the previous workspace's account-home links. |
| Launcher-owned provider/agent environment files | [`writeMacosUserAgentEnvFiles`](../../internal/cli/run/agentenvfiles.go#L107-L117) stores files in the workspace sidecar, while [`agentFileLines`](../../internal/cli/run/agentenvfiles.go#L399-L418) embeds the supplied HOME in their exported paths. [`run.go`](../../internal/cli/run/run.go#L1136-L1143) is the production caller; [`agentenvfiles_test.go`](../../internal/cli/run/agentenvfiles_test.go#L154-L165) currently pins the account-home path. | Keep sidecar ownership and replacement semantics, but generate embedded provider/agent paths against the resolved target-workspace HOME. Updating launch HOME alone leaves these launcher-authored exports pointing at account `.config`. |
| Briefing destination HOME | [`prepare.go`](../../internal/cli/run/prepare.go#L173-L178) supplies `nativeHomeFor(rt)` as `BriefingInput.Home`; [`nativeHomeFor`](../../internal/cli/run/prepare.go#L787-L797) currently returns `SandboxHome()` for native runtimes, which [`briefing.go`](../../internal/jailcontent/briefing.go#L701) consumes. | Resolve this input to the target workspace HOME so generated briefing paths follow the same HOME as the launch, while container backends remain unchanged. |
| Other plan consumers | [`buildBootstrapEnv` / `MCPServersAt`](../../internal/macosuser/runplan.go#L608-L631) receive `SandboxHome()` explicitly; [`JailDaemonArgv` / `ProbeServicesArgv` call sites](../../internal/macosuser/runplan.go#L758-L785) pass empty HOME, selecting the account default. | Supplying private HOME only to the agent leaves MCP paths, daemon PATH and service probes on the wrong home. Keep declared MCP/endpoint access policy unchanged. |
| Native generators | [`runDarwinBootstrap`](../../internal/cli/internal.go#L362-L421) rebinds HOME/JAIL_HOME before [`DarwinEnvFrom`, `RunDarwinBootstrap`](../../internal/entrypoint/darwin.go#L58-L136); [`Env`](../../internal/entrypoint/env.go#L36-L56) derives npm prefix, Go path and mise. | The explicit home argument and environment must agree before any generator loads or writes home-derived paths. |
| Private directories and root-file redirects | [`DeriveDarwinHomeLayout`](../../internal/entrypoint/darwinhomelayout.go#L163-L218), [`HomeSurfaces` and `HomeFileRedirects`](../../internal/paths/paths.go#L744-L799) map private dirs into the existing sidecar. | Keep its physical `npm-global`, `local`, `go`, `yolo-bin`, `config` and selected workspace pack state; move only the logical home root and its links. |
| Root-level configured files and login rc | [`WithHostFileRedirects`, `DarwinLoginRCFiles`](../../internal/entrypoint/darwinhomelayout.go#L1034-L1065) derive redirects using `StagingFor`; [`WriteLoginRC`](../../internal/entrypoint/darwin.go#L281-L314) writes three real root files. | Keep root-file redirects private and preserve the existing refusal of configured login rc destinations. Login shells must read the private rc and re-prepend this session's PATH, not the account's stale rc. |
| Pack sharing hooks | [`linkIntoSharedDir`](../../internal/entrypoint/packhooks.go#L271-L319) requires declared shared state and calculates the target with `filepath.Rel`; [`packHookLinks`](../../internal/entrypoint/darwinhomelayout.go#L646-L685) predicts those links for guarded writes. | Keep hook bytes backend-independent; give both the logical home and the physical sidecar a selected shared-dir link to the same backing. |
| mise and cache | [`SandboxMiseData`](../../internal/macosuser/macosuser.go#L76-L100), [`buildBootstrapEnv`](../../internal/macosuser/runplan.go#L1245-L1259) currently derive mise from HOME; [`InstallDarwinCacheRelocations`](../../internal/entrypoint/darwinhomelayout.go#L815-L885) opens a real HOME cache and refuses a symlink when laying relocations. | Decouple machine backing from private HOME explicitly. A new HOME cache link cannot simply be fed to today's relocation writer; retain its real-root / handle checks at the backing. |
| Content install and kernel write protection | [`InstallHomeOverlay`](../../internal/entrypoint/darwin.go#L220-L242), [`overlayLinks.route`](../../internal/entrypoint/darwinoverlay.go#L228-L288), [`ResolveHomeReadonly`](../../internal/macosuser/homereadonly.go#L122-L170). | Delivery and profile must share the new geometry, physical paths and anchors. Preserve handle-based containment and destination-only replacement; never replace whole agent state. |
| Native per-session config and validation | [`nativehome.Root`, `Prepare`, `ClaimGone`, `Enter`](../../internal/nativehome/nativehome.go#L36-L136), [`RunDarwinBootstrap`](../../internal/entrypoint/darwin.go#L120-L136), and [`sessionFileInvariants`](../../internal/macosuser/sessionfiles.go#L390-L406) derive and validate native config roots from HOME plus workspace/session identity; [`runplan.go`](../../internal/macosuser/runplan.go#L1753-L1757) invokes that validator. | Thread the resolved workspace HOME through native session-root derivation and validation. Keep this distinct from persistent HOME: no session teardown deletes workspace bytes. Existing cleanup also requires account-idle proof, not just a free lease. |
| Shared Nix | [`hostNixReal`, `resolveHostNix`](../../internal/macosuser/hostnix.go#L64-L109) deliver the resolved `/nix/store` client only with a daemon socket; the environment defaults to daemon use. | No HOME relocation should turn this into a local store, open the host user's profile/home, or widen Nix rights. Keep user-configured environment precedence. |
| Install capture / sealed build | [`SeatbeltCaptureProfile`](../../internal/macosuser/seatbeltcapture.go#L91-L166), [`BuildCapturePlan`](../../internal/macosuser/capture.go), [`ProvisionArgv`](../../internal/macosuser/provision.go#L99-L153). | Capture stays a flat throwaway HOME with no ordinary sidecar, no credential backing access and no new machine-path allows. Normal provisioning uses the selected workspace's session profile. |
| Admission and lifetime | [`HoldAccountHome`](../../internal/cli/run/accounthomehold.go#L3-L42), [`RunMacosUser`](../../internal/macosuser/orchestrator.go#L933-L946), [`workspace_lock` and release](../../internal/macosuser/orchestrator.go#L1175-L1205), [`session records`](../../internal/macosuser/sessionfiles.go#L3-L50), [`guest tree holder proof`](../../internal/macosuser/packtreeproof.go#L3-L36). | Host lock release proves neither guest exit nor persistent-home reclaimability. Keep the per-workspace staging-through-provisioning lock; inventory detached children before any migration touches shared paths. |

**Passwd/home lookup is a separate native fact.** A changed HOME does not change `getpwuid` or
macOS home-directory APIs. The [capture profile's existing warning](../../internal/macosuser/seatbeltcapture.go#L76-L84)
already names this incompatibility. The [guest UID probe](../../internal/macosuser/guestprocs_darwin.go#L69-L82)
uses directory services via `id`, because pure-Go account lookup is not a reliable Mac directory-service
probe. Source review cannot certify shells, native libraries or vendor tools that ignore HOME.

## Concrete backing and link proposal

**Advice, not selected path schema:** use a real `<workspace>/.yolo/native-home` as the persistent
logical HOME; retain `<workspace>/.yolo/home` as the physical private state sidecar. This avoids
renaming the state shared with container backends or introducing an account/setup-owned home root.
The existing guest bootstrap already writes workspace state; creating a sibling real directory is
an engineering candidate within that crossing, not permission to change its ACLs.

Keep the current real machine directories in `/Users/_yolojail` as their backing. This avoids
moving shared credentials or tool/cache bytes during the first transition. The account home is
**not** the new runtime HOME and must not be re-allowed as a whole. No automatic adoption of
unclassified account-root files is proposed.

Illustrative geometry for workspace A (B has a distinct workspace prefix):

```text
A/.yolo/native-home/                         real, persistent HOME
  .npm-global -> A/.yolo/home/npm-global      existing private program bytes
  .local      -> A/.yolo/home/local
  go          -> A/.yolo/home/go
  .config     -> A/.yolo/home/config
  .yolo/bin   -> A/.yolo/home/yolo-bin
  .claude     -> A/.yolo/home/claude           only if selected pack declares it
  .claude-shared-credentials -> /Users/_yolojail/.claude-shared-credentials
  .cache      -> /Users/_yolojail/.cache       existing machine cache backing
  .yolo/mise  -> /Users/_yolojail/.yolo/mise   existing machine tool backing
  .gitconfig  -> .config/git/config
  .npmrc      -> .config/yolo-home/<slug>     only the configured StagingFor redirect
  .zprofile, .zshrc, .bash_profile            real, generated in this HOME
A/.yolo/home/
  claude/.credentials.json -> ../.claude-shared-credentials/.credentials.json
  .claude-shared-credentials -> /Users/_yolojail/.claude-shared-credentials
```

The credential paths are a **geometry example**, not a new grant or a test using live tokens.
Only selected `SharedDirs` get links/allows; a launch without that pack gets neither. A selected
nested workspace state dir follows the existing deriver's physical mapping, not a guessed
leading-dot convention in a second implementation.

**Why both shared links:** the hook writes via HOME, but the kernel resolves `..` from the
physical private state directory. In this example it reaches the sidecar's mirror, not the
logical HOME. The existing [relative-link measurement](../reference/macos-user-home-tiers.md#the-mirror-and-the-relative-credential-link-it-exists-for)
explains the failure a single home-root link would leave. Keep `filepath.Rel` and mirror depth
consistent for configured packs too, including a `shared_directory` hook nested below two levels.
A disposable Linux VFS probe on 2026-10-21 confirmed that two synthetic private homes retain
distinct canaries while a nested `../../.synthetic-shared` hook reaches the same backing through
the sidecar mirror. This validates link resolution only, not Seatbelt or macOS compatibility.

### Profile and bootstrap implications

- **Constraint:** allow the current workspace/private HOME and the *physical* selected machine
  directories and existing configured context/cache targets. Ancestors get traversal literals,
  not a writable/readable subtree. Never allow all of `/Users/_yolojail`, `/Users/Shared`, or a
  parent containing sibling workspaces to compensate for a missing link.
- **Constraint:** retain terminal content write denies and anchor create/unlink denies after the
  allows. Resolve existing base aliases (`/var` versus `/private/var`) and derive paths from the
  same layout used by delivery. A writable shared-dir link is not authority to follow an arbitrary
  link during an unconfined bootstrap write.
- **Advice:** keep `MISE_DATA_DIR` naming the real account machine backing and derive PATH's mise
  shims from that same value. Otherwise today's `SandboxMiseData(newHome)` quietly installs a
  separate store. The optional home-relative mise link must not become a second authority.
- **Advice:** perform cache relocation updates on the explicitly known real machine cache root,
  then expose the home cache link. Do not relax today's arbitrary-symlink refusal. Existing
  user-scope relocation targets/manifest semantics remain; the machine update must be serialized
  separately from private bootstrap so two different workspaces do not race its link/manifest writes.
- **Constraint:** no path-only containment checks before writes. Preserve the real `.yolo` and
  sidecar checks and opened-root routing. A candidate HOME under `.yolo` needs equivalent
  anti-link and anchor treatment; a symlink planted at HOME or its ancestor must not redirect
  bootstrap into B. This is especially important because bootstrap itself runs outside Seatbelt.

The profile currently [re-allows the supplied home as a whole](../../internal/macosuser/seatbelt.go#L166-L220).
Passing the proposed private HOME without explicit machine target clauses would break sharing;
passing the account home instead would preserve the unwanted broad allowance. Neither is a fix.
A physical home on another volume also needs the existing volume/context policy checked. With
`allow default`, the `/Users` read deny is not sibling protection for a neutral workspace root
outside `/Users`, or one reopened by a later broad allow. The planner must prove a deny/re-allow
boundary for every supported root, including backing state, without enumerating only currently
known siblings or narrowing existing workspace siting policy. The default `/Users/Shared/yolo`
geometry is the first disposable case, not proof for every permitted siting.

## Serial transition and conservative lifetime sketch

This is transition safety advice, **not** a new migration command or reclamation policy.

1. **Before changing any global link or backing:** retain the current hold and obtain a complete
   guest-idle/liveness answer, including bootstraps, provisioning, supervisors and detached children.
   The hold's own [SIGKILL limit](../../internal/cli/run/accounthomehold.go#L37-L42) means a free host
   lock is insufficient. An unknown answer retains old paths/bytes and postpones the transition;
   it is not a permanent second-workspace admission policy.
2. **Prefer no shared-byte migration:** leave real account machine dirs and existing sidecar bytes
   in place. Create a private home without repointing the legacy account's workspace links. Generate
   only owned root redirects/rc files there. Keep unclassified root files and old session homes;
   do not copy credentials opportunistically, recursively chown, delete or silently adopt them.
3. **Serialize publication for each workspace:** the existing workspace launch lock covers host
   content staging through bootstrap and provisioning, then releases before the agent. Build and
   check the new layout before publishing a usable private home; failed/partial generation must
   not make the next launch mistake it for complete. Keep previous paths and retryable bytes on
   failure. Any publication marker is implementation detail, not a new user config framework.
4. **During mixed-version transition:** old launchers still mutate the global links. New homes must
   not use those links for private state, readiness, login rc or native session roots. Retain the
   guard until this separation and native denial are demonstrated; do not remove it in the first
   geometry patch. Rollback keeps both homes and cannot retarget a running process's paths.
5. **Do not reuse session reaping for persistent HOME:** session env/profile records, account-owned
   native config leases and guest pack-tree claims have different owners/lifetimes. Tree-holder
   ancestry/start-time evidence conservatively retains unknown detached processes; it is not a
   workspace deletion authorization. Session cleanup may conservatively retain config while any
   guest is running; that need not block concurrent launches. Persistent HOME/state removal remains
   [OQ-MCW3](macos-user-concurrent-workspaces.md#OQ-MCW3), regardless of free leases or absent host PID.

## Disposable native verification handoff — not executed

Use synthetic `A-private`, `B-private` and `selected-shared` canaries, never credentials or a
real agent invocation. Record candidate SHA, macOS version/architecture, actual physical roots,
rendered profile and stdout/status for each case. A bare control must succeed **as the same guest
UID** before a denial is credited to Seatbelt. Follow the existing
[Seatbelt control pattern](../../integration/macosuserseatbelt_test.go#L746-L785) and
[capture guest-account fixture](../../integration/macosusercaptureseatbelt_test.go#L42-L106),
not a test that proves only Unix mode denial as the host user.

| Future disposable case | Observation required |
| :--- | :--- |
| A and B held concurrently, then relaunched in reverse order | Each shell continuously reads its own HOME, private canary, npm/Go/local paths and login PATH before/after the other's bootstrap. No account-home retarget; `bash -lc` and `zsh -lc` keep the selected prefix. Same-workspace staging remains serialized. |
| HOME versus directory-service lookup | Shell `$HOME`, `~`, and explicit passwd/native home lookup are recorded separately. The directory-service home remains the account's. Tools using it must not cause a broad account-home re-allow; compatibility is a native evidence gap, not a reason to change the account record. |
| Sibling HOME/state denial in both directions | Read, write, create, unlink and rename B's private canary from A by absolute path, `..`, and absolute/relative symlinks planted under A; repeat B→A. Controls succeed, confined attempts fail for the kernel-policy reason. Include physical aliases and nested neutral-root workspace paths. |
| Selected shared positive / unselected negative | Synthetic declared machine file works by logical hook path, physical mirror and backing; nested hook link resolves. A different unselected synthetic machine dir remains unreadable/unwritable. A bare account-root private file remains denied. |
| Protected content and unconfined delivery | Skills/briefing read works; writes and anchor replacement fail. Planted links at private HOME, `.yolo`, sidecar or destination ancestors cannot route bootstrap writes into B; previous state beside/above destinations survives failed install. |
| Install prefixes, mise, cache, configured files | Fake executable payloads establish private prefixes without starting agents. Both homes reach one allowed machine mise/cache backing, configured relocations and declared root-file redirects only; empty pack selection and no host files also boot. Relocation manifest updates do not race. |
| Nix and capture regression controls | Existing store client/daemon behavior still works without host-home access. Capture's flat staging HOME is writable, both workspace homes and machine credential backing denied; sealed build keeps its existing loopback/Nix restrictions. No actual installer or API call is needed for this path test. |
| Interrupted launch / detached child / unknown process table | Failed bootstrap, host death, inherited lease, detached child closing descriptors, and unreadable liveness evidence retain required paths and bytes. A completed session removes only session-owned artifacts; no case implicitly reclaims persistent HOME. |

**Existing native commands, for an authorized operator on an already provisioned disposable Mac:**
the [manual-checks runbook](../plans/runbooks/macos-user-manual-checks.md#02-the-job-that-runs-them)
names the suite below and its prerequisites; it is not a request to dispatch or perform setup.
The first command checks the native generator/staging layer, not sibling kernel denial.

```console
$ go test -count=1 ./internal/entrypoint ./internal/macosuser
$ YOLO_TEST_MACOS_USER=1 go test -count=1 -timeout 0 -v -run '^TestMacosUser' ./integration
```

The existing [macos-user workflow](../../.github/workflows/macos-user.yml) is the scheduled/manual
route. Its current cases are regression instruments, **not** tests of this unbuilt geometry;
future concurrent disposable cases must be added before reporting it verified. A declared run
that executes zero cases is not evidence. Do not run `sudo -v`, `macos-setup`, dispatch CI, or
change account permissions/credentials from this sketch. If a host cannot create/use the proposed
real HOME with its existing workspace crossing, record the failed operation and missing ownership/
ACL fact; do not experiment with root or add a setup grant.

## What remains a ruling, what remains engineering

- **Owner:** [OQ-MCW2](macos-user-concurrent-workspaces.md#OQ-MCW2) asks whether the security claim
  includes arbitrary same-UID processes outside Seatbelt. The geometry supplies no such isolation.
  A broader ruling requires the boundary to be reconciled before a build handoff, not silently
  weakened or implemented by more OS users/privileges.
- **Owner:** [OQ-MCW3](macos-user-concurrent-workspaces.md#OQ-MCW3) asks which explicit event, if any,
  permits persistent workspace reclamation. This sketch retains bytes and supplies no cleanup event.
- **Engineering:** backing/link construction, shared cache-update serialization, physical path
  derivation, HOME propagation, non-destructive publication and reader tests can be specified
  against this proposal without another owner schema/framework decision. Exact leaf/helper naming
  is cheap at handoff; sharing scope and retained state are not.
- **External runtime:** passwd/native-library behavior, existing workspace ACL usability, kernel
  symlink/physical denial and simultaneous launch stability need authorized native evidence.
  Prior Apple Container success and Linux rendered-profile tests do not settle these facts.

**First small build slice after the rulings and native feasibility evidence:** complete the plan
against then-current HEAD, starting with a pure workspace-HOME/explicit-machine-backing derivation
and geometry fixtures covering core dirs, selected/unselected shared dirs, nested hooks, root files
and physical read-only anchors. Keep production launch dispatch and `HoldAccountHome` unchanged
in that slice. Subsequent reader wiring and native kernel cases, not that pure test commit, justify
switching HOME or removing the hold. The existing `.local` fork-sidecar deferral is untouched.
