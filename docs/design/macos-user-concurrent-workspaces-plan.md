---
status: accepted
stage: DECIDED
next: "Build slice 1 (pure workspace-home geometry); slices 5 and 6 each land only after their native Mac verification run"
tags: [macos-user, workspaces, plan]
vantage:
  status-chip: true
---

# Plan: private workspace homes under one guest UID

**Status:** 2026-10-22. Completed against the tree, written against `b0f156fb1`. Nothing is built.
Native Seatbelt enforcement of the new geometry is UNMEASURED: no Mac has run any case below.
Slices 1 to 4 change no production behavior and can land on the Linux gates. Slice 5 switches
the launch HOME, and slice 6 removes the account-home hold. Each of those two lands only after a
human runs its native verification ([V1, V2](#native-verification--a-human-runs-these)) on a
disposable Mac. No owner question is open, and none was raised by this pass.

**Design:** [private workspace homes under one guest UID](macos-user-concurrent-workspaces.md#selected-architecture-and-boundary).

**Precedence:** the design wins on behavior. The tree wins on fact when the drift is cosmetic (a
moved line, a renamed helper with the same role): follow the tree and say so in the report and the
commit. This plan is advice and is the first thing to be wrong. Never twist the code to match
it. **Stop and report, do not commit**, when a task's red test does not fail the way the task
predicts, a named file is dirty with someone else's work, a named symbol is gone, the fix needs a
file outside the task's fence, or a gate fails outside the task's scope.

## Settled — do not re-open

Quoted from the [design](macos-user-concurrent-workspaces.md#selected-architecture-and-boundary)
and its [decision ledger](macos-user-concurrent-workspaces.md#decision-ledger):

- Exactly one `_yolojail` OS user. No account per workspace, and waiting for or refusing a
  second workspace is not the product behavior.
- Each workspace has a private, stable `HOME` and persistent state; neither changes when another
  workspace starts.
- Seatbelt-confined processes and their descendants are denied sibling workspace HOME/state by the
  kernel, including through symbolic links and `..`. Unconfined same-UID processes the host
  controls are trusted and outside the claim ([OQ-MCW2](macos-user-concurrent-workspaces.md#decision-ledger)).
- Sharing stays limited to the selected packs' machine-scope dirs and the existing cache, mise
  and Nix paths and configured grants. No new credential source, grant or permission ([OQ-MCW4](macos-user-concurrent-workspaces.md#decision-ledger)).
- Persistent workspace HOME/state is retained like container `.yolo` state: no automatic
  deletion, expiry or cap, and no cleanup operation in this work ([OQ-MCW3](macos-user-concurrent-workspaces.md#decision-ledger)).
- The account-home hold stays until the replacement is implemented and verified on a Mac.

## Implementation decisions

Each closes an engineering detail the sketch left open. These are mechanism choices under the
rulings above, not owner rulings; the *coined here* ids exist so commits and code comments can
cite them.

| ID *(coined here)* | Decision | Why |
| :--- | :--- | :--- |
| <a id="MCWP-D1"></a>MCWP-D1 | **Workspace HOME is `<resolved workspace>/.yolo/native-home`**, spelled once as a new `paths.WorkspaceNativeHome(workspace)` beside `paths.WorkspaceHomeState`. The workspace is the one `resolvePathAbs` already produces at the top of `BuildRunPlanWithStages`. | Inside the workspace grant the profile already has, so no new setup grant or ACL. A sibling of the existing sidecar, so container-era state keeps its name. HOME cannot be `.yolo/home` itself (raw sidecar names fail the `.config`/`.claude` mapping, and a `.yolo` link back would recurse), and a linked `.yolo` is refused by [`OpenStateDirRoot`](../../internal/paths/statefile.go#L33-L58). |
| <a id="MCWP-D2"></a>MCWP-D2 | **Machine root stays `/Users/_yolojail`** (`macosuser.SandboxHome()`), passed to the bootstrap as a new env var `YOLO_DARWIN_MACHINE_HOME` and to every sandbox argv. Absent, or equal to HOME, means today's single-home layout, byte for byte. | Moves no shared bytes. The directory-service record (`NFSHomeDirectory`) and the bootstrap's identity and scope checks stay on the account. |
| <a id="MCWP-D3"></a>MCWP-D3 | **HOME's links.** Workspace links into the sidecar, unchanged from today's deriver: `.npm-global`, `.local`, `go`, `.yolo/bin`, `.config`, and each selected `scope: workspace` dir. **Machine links** *(coined here: a HOME path linked to an absolute path under the machine root)*: `.cache`, `Library/Caches`, and each selected `scope: machine` dir. Home-root file redirects (core's three and `host_files`) are laid in HOME as today. **No `.yolo/mise` link**: `MISE_DATA_DIR` names `/Users/_yolojail/.yolo/mise` absolutely. | `.cache` and `Library/Caches` are today's machine-wide caches (`printCacheRelocations` says Go's build cache lives in the latter), so keeping them shared preserves established cache policy. A mise link would be a second authority for one value. |
| <a id="MCWP-D4"></a>MCWP-D4 | **Relative pack-hook backing: both links.** Keep the sidecar mirrors (`<sidecar>/<shared> -> <machine>/<shared>`) and add the HOME machine link for the same dir. Pack hook bytes do not change. | The hook writes by the absolute `<HOME>/<shared>` path, which the HOME machine link serves. The relative link it leaves is resolved by the kernel from the physical directory, which is in the sidecar whenever `from` sits under a workspace link, and in HOME otherwise. Depth is preserved because the deriver maps `.x/y` to `x/y`. |
| <a id="MCWP-D5"></a>MCWP-D5 | **HOME propagation: one authority.** A new `macosuser.LaunchHomes(workspace) Homes` (`Homes{Home, Machine string}`) is the only producer. `BuildRunPlanWithStages`, `buildPlan`, every sandbox argv builder, the host-side briefing and agent-env writers, and the session validator read it. Slices 1 to 4 return `{SandboxHome(), SandboxHome()}`; slice 5 returns the native home. | One switch, flipped in one commit, so no consumer can be left on the account home. |
| <a id="MCWP-D6"></a>MCWP-D6 | **Passwd/directory-service lookup is not changed and not compensated.** `getpwuid` keeps answering `/Users/_yolojail`. A tool that reads its home from passwd rather than `$HOME` (OpenSSH's `~/.ssh` is the known one) meets the profile's denial and fails closed. Fixing such a tool is a per-tool redirect later, never a re-allow of the account home. | The design forbids widening the profile, and repointing the record per launch would recreate the shared-home race. |
| <a id="MCWP-D7"></a>MCWP-D7 | **Login rc files** are real files in HOME, written with no symbolic link followed (open beneath an `os.Root` on HOME, `O_NOFOLLOW`, replace by rename). Their content is unchanged: re-prepend `$YOLO_DARWIN_LOGIN_PATH`. The same no-follow rule covers the shared-credentials log at the HOME root. | HOME is agent-writable, and the bootstrap runs unconfined: a link planted at `.zshrc` would otherwise carry its write into a sibling. |
| <a id="MCWP-D8"></a>MCWP-D8 | **Machine-tier writes are serialized** by an exclusive `flock` on `/Users/_yolojail/.yolo/locks/machine-tier.lock`, opened beneath an `os.Root` on `/Users/_yolojail/.yolo` (`O_NOFOLLOW`, created when absent), held only around creating the machine dirs and `InstallDarwinCacheRelocations`. Wait at most 60 s, then fail the boot naming the lock file and "run `yolo` again". Relocations stay last-writer-wins user scope. | Two workspaces' bootstraps now run at once over one `.cache` manifest whose temp-file write is not concurrency-safe. In private mode no confined process can open the lock, so none can hold it to stall another workspace's boot. |
| <a id="MCWP-D9"></a>MCWP-D9 | **Profile in private mode:** after the write deny on `/`, `(deny file-read* file-write* (regex #"/\.yolo(/|$)"))`, then the workspace allows re-open the session's own workspace. The account home is no longer `(subpath)`-allowed; read and write allows name each machine dir; `(literal)` grants the machine root and each intermediate dir; a create/unlink deny anchors each machine dir and the HOME chain (`<ws>/.yolo`, `<ws>/.yolo/native-home`). Zero `MachineAccess` renders today's profile byte for byte. The capture profile gains the same `.yolo` deny. | The `/Users` read deny protects nothing for a workspace sited outside `/Users`, and `(allow default)` would leave a sibling's state readable there. A path-component rule needs no list of siblings and survives `/private` and Data-volume aliases. |
| <a id="MCWP-D10"></a>MCWP-D10 | **Guest yolo judges workspace scope by the account.** `sandboxEnvPairs` emits `YOLO_DARWIN_MACHINE_HOME` (protected) in private mode. `cli.Main` and `cmd/yolo-jaild` call one new `paths` helper that, when that variable names a directory owned by the process euid, applies `paths.GuardScopeUnder` to it. The three per-verb copies (bootstrap, log sink, capture materialize) may stay. | With HOME inside the workspace, every guest verb that creates `.yolo` would otherwise see "workspace CONTAINS your home" and refuse. `yolo-jaild` reaches `nixroots` and `durable`, which create it. Ownership, not the variable, is the identity check, so the host user is unaffected even if it has the variable set. |
| <a id="MCWP-D11"></a>MCWP-D11 | **The host creates HOME, the guest verifies it.** `prepareWsState` makes `native-home` beneath its `os.Root` on `.yolo` (macos-user only), so it inherits the workspace ACL exactly as the sidecar does. The bootstrap refuses when HOME or `<ws>/.yolo` is a symbolic link, or when any directory between HOME and a link it lays is one. | Same crossing as the sidecar today, no new grant. No publication marker is needed: only a launch whose bootstrap succeeded reads HOME, and the workspace launch lock already serializes bootstraps of one workspace. |
| <a id="MCWP-D12"></a>MCWP-D12 | **Native session config moves with HOME.** `nativehome.Root(home, ws)` and every caller take the private HOME, so session homes live at `<HOME>/.yolo-native-sessions/<hash>/<id>`. Session cleanup keeps its `AccountIdle` proof, which is account-wide. | A concurrent sibling keeps the account busy, so cleanup simply waits; that is conservative and blocks no launch. Session teardown never touches persistent HOME. |

## Map

| Path | Change | Slice |
| :--- | :--- | :--- |
| [`internal/paths/paths.go`](../../internal/paths/paths.go#L714-L716) | new `WorkspaceNativeHome`, beside `WorkspaceHomeState` | 1 |
| [`internal/entrypoint/darwinhomelayout.go`](../../internal/entrypoint/darwinhomelayout.go#L154-L221) | new `DeriveDarwinWorkspaceHomeLayout(home, machine, sidecar, writableDirs, sharedDirs)`; `DeriveDarwinHomeLayout` delegates with `machine = home`; new fields `Machine`, `MachineLinks`, `MachineDirs` | 1 |
| [`internal/macosuser/seatbelt.go`](../../internal/macosuser/seatbelt.go#L157-L226) | new `MachineAccess` input, private-mode rules ([MCWP-D9](#MCWP-D9)), exported wrapper for the integration test | 2 |
| [`internal/macosuser/seatbeltcapture.go`](../../internal/macosuser/seatbeltcapture.go#L98-L166) | `.yolo` component deny | 2 |
| `integration/macosuserworkspacehomes_seatbelt_test.go` | new: native kernel cases run as the sandbox account (the V0 instrument) | 2 |
| [`integration/macosuserseatbelt_test.go`](../../integration/macosuserseatbelt_test.go#L939-L1012) | registry test also renders a private-mode profile; register the new ids | 2 |
| [`internal/entrypoint/darwinhomelayout.go`](../../internal/entrypoint/darwinhomelayout.go#L246-L308) | `Apply` lays `MachineLinks`; refuses links on the HOME chain; machine dirs under [MCWP-D8](#MCWP-D8)'s lock; `homeFileThroughLayout` follows machine links and accepts targets under `Machine`; `darwinHomeLayoutFor` reads `YOLO_DARWIN_MACHINE_HOME`; `InstallDarwinCacheRelocations` acts on the machine root | 3 |
| [`internal/entrypoint/darwin.go`](../../internal/entrypoint/darwin.go#L302-L314) | `WriteLoginRC` no-follow ([MCWP-D7](#MCWP-D7)) | 3 |
| [`internal/entrypoint/packhooks.go`](../../internal/entrypoint/packhooks.go#L337) | shared-credentials log opened no-follow | 3 |
| [`internal/paths/workspacescope.go`](../../internal/paths/workspacescope.go#L147-L159), [`internal/cli/cli.go`](../../internal/cli/cli.go#L30), `cmd/yolo-jaild` | guest scope helper and its two callers ([MCWP-D10](#MCWP-D10)) | 3 |
| [`internal/macosuser/macosuser.go`](../../internal/macosuser/macosuser.go#L73-L100) | `LaunchHomes`, `Homes`; `SandboxPathForGuestBin` and `sandboxEnvPairs` take `Homes` (mise from `Machine`, `YOLO_DARWIN_MACHINE_HOME` in private mode) | 4 |
| [`internal/macosuser/envfile.go`](../../internal/macosuser/envfile.go#L291-L313) | `YOLO_DARWIN_MACHINE_HOME` joins `ProtectedSandboxEnvNames` | 4 |
| [`internal/macosuser/runplan.go`](../../internal/macosuser/runplan.go#L459) | every `SandboxHome()` at `:568`, `:631`, `:633`, `:648`, `:819`, `:827`, `:843` and the `""` homes at `:772`, `:786` read `Homes`; `buildBootstrapEnv` (`:1049`, mise at `:1270`) takes the machine root; `RunPlan` carries `Home`, `MachineHome`; new invariant: every sandbox argv carries the same `HOME` | 4 |
| [`internal/macosuser/orchestrator.go`](../../internal/macosuser/orchestrator.go#L664-L686) | `buildPlan` reads `LaunchHomes`; readiness probe passes `MISE_DATA_DIR` from the machine root | 4 |
| [`internal/macosuser/homereadonly.go`](../../internal/macosuser/homereadonly.go#L127-L196) | anchors include `chainBelow(workspace, home)` when HOME is inside the workspace | 4 |
| [`internal/macosuser/sessionfiles.go`](../../internal/macosuser/sessionfiles.go#L409-L430) | `nativehome.Dir(plan.Home, …)` | 4 |
| [`internal/macosuser/jaildaemon.go`](../../internal/macosuser/jaildaemon.go#L463), [`serviceprobe.go`](../../internal/macosuser/serviceprobe.go#L55), [`provision.go`](../../internal/macosuser/provision.go#L148), [`ctxlinks.go`](../../internal/macosuser/ctxlinks.go#L742-L752) | argv builders take `Homes` | 4 |
| [`internal/cli/probeservices.go`](../../internal/cli/probeservices.go#L49) | default home from `LaunchHomes` | 4 |
| [`internal/cli/run/prepare.go`](../../internal/cli/run/prepare.go#L430-L445) | `prepareWsState` creates `native-home` on macos-user; `nativeHomeFor(rt)` (`:793`) becomes per-workspace | 4 |
| [`internal/cli/run/agentenvfiles.go`](../../internal/cli/run/agentenvfiles.go#L112-L114), [`run.go`](../../internal/cli/run/run.go#L1150) | `writeMacosUserAgentEnvFiles(sidecar, home, channel)` | 4 |
| [`internal/cli/run/macoshomeoverlay.go`](../../internal/cli/run/macoshomeoverlay.go#L115) (or `HostContext`) | carry `packload.SharedDirs(packs)` to the plan even when no overlay tree was staged | 4 |
| [`internal/macosuser/macosuser.go`](../../internal/macosuser/macosuser.go#L73-L74) | `LaunchHomes` returns the native home | 5 |
| [`internal/cli/run/accounthomehold.go`](../../internal/cli/run/accounthomehold.go) | deleted, with the `Deps.HoldAccountHome` seam, `PreflightLaunch`'s `hold` parameter and their wiring | 6 |

## Reuse before you write

- **The deriver's own loops.** `DeriveDarwinWorkspaceHomeLayout` is today's body with `machine`
  replacing `home` in the shared-dir loop; do not write a second list. `paths.HomeSurfaces`,
  `paths.HomeFileRedirects`, `packload.WritableDirs` and `packload.SharedDirs` stay the inputs.
- **No-follow writes:** copy the shape of `InstallDarwinCacheRelocations`
  ([`darwinhomelayout.go:832`](../../internal/entrypoint/darwinhomelayout.go#L832-L928)): Lstat the
  real directory, `os.OpenRoot`, compare with `os.SameFile`, write beneath the root, rename into
  place. `writeCacheRelocationManifest` is the rename shape.
- **Link-chain refusal:** extend `linkedSidecarPaths`
  ([`:390`](../../internal/entrypoint/darwinhomelayout.go#L390-L428)) and reuse `LinkedSidecarError`
  for the HOME chain; the message already names `sudo rm` of the link and nothing below it.
- **Bounded lock:** `lockBounded` and `unlockClose` in
  [`accounthomehold.go`](../../internal/cli/run/accounthomehold.go#L183-L214) are the wait and
  release shape; reimplement in `internal/entrypoint` beneath the `os.Root` (that file is deleted
  in slice 6, and `entrypoint` cannot import `internal/cli/run`).
- **Physical path resolution:** `resolveLongestExisting` and `chainBelow` in
  [`homereadonly.go`](../../internal/macosuser/homereadonly.go#L127-L211).
- **Traversal literals:** `ancestorLiterals` in
  [`seatbelt.go`](../../internal/macosuser/seatbelt.go#L513-L535) covers only `/Users/Shared/`;
  write the machine-root literals beside it rather than widening it.
- **Native test fixtures:** the capture suite's account-run controls
  ([`macosusercaptureseatbelt_test.go:184`](../../integration/macosusercaptureseatbelt_test.go#L184-L268)),
  `requireMacosUser` and `runAsSandboxQuiet`
  ([`macosusergate_test.go`](../../integration/macosusergate_test.go#L360-L520)), and
  `macosUserWorkspace` (`:577`) for a workspace under the shared root.
- **Linux twins:** `TestASecondWorkspaceLayoutRepointsTheFirstsLinks`
  ([`darwinhomelayout_test.go:841`](../../internal/entrypoint/darwinhomelayout_test.go#L841-L870))
  is the two-workspace fixture to invert; `TestPackHookLinksAreTheLinksTheHooksLay`
  ([`hostfileredirect_test.go:550`](../../internal/entrypoint/hostfileredirect_test.go#L550)) runs
  the real hooks; `TestNativeSyntheticWorkspaceMirrorsReachOneCredentialAndLockNamespace`
  ([`nativeclaudecoordination_test.go:18`](../../internal/cli/run/nativeclaudecoordination_test.go#L18))
  is the synthetic credential fixture.

## Traps

- **`homeFileThroughLayout` refuses any absolute target outside `l.Home`** (`restart`,
  [`:509-524`](../../internal/entrypoint/darwinhomelayout.go#L509-L524)). Every sidecar mirror
  targets the machine root, so in private mode every `host_files` write through a shared dir is
  refused until `restart` also admits targets under `l.Machine`.
- **`MkdirAll` follows links.** `Apply` and `ensureLayoutSymlink` create parents by path. A link
  the agent planted at `<HOME>/.yolo` would carry `.yolo/bin`'s link into a sibling. Check the
  chain before creating anything, as `linkedSidecarPaths` does for the sidecar.
- **`SandboxMiseData(home)` follows HOME.** Fed the private home, it silently creates a
  per-workspace mise store. Every caller passes the machine root; the install capture passes its
  own staging home, as today.
- **The registry test renders only the legacy profile.**
  `TestMacosUserSeatbeltRegistryMatchesTheProfile` fails "registered but not in the profile" for
  every new id until it also renders a private-mode profile.
- **`.yolo/bin` maps to `yolo-bin`, one level shallower.** A pack hook whose `from` sat under
  `.yolo/bin` would resolve its relative link one level off. No shipped pack does; leave it.
- **HOME is inside the workspace tree, unlike podman's `/home/agent`.** A tool whose cwd is under
  HOME and walks upward (git, mise, npm) finds the workspace's own `.git` and project files.
  `.yolo/.gitignore` already hides `.yolo` from git. Accepted divergence; do not move HOME.
- **`cd ~ && yolo config …` in the guest** finds `<HOME>/.yolo` and treats HOME as a workspace.
  Low harm (a `.gitignore` in `<HOME>/.yolo`); accepted.
- **`workspace_readonly: ["."]`** (or `.yolo`) now makes HOME unwritable too. It already made
  the sidecar, so every agent state dir, unwritable; no new handling.
- **Agent-planted links deeper in the sidecar are followed by unconfined generators today**
  (the residual in [the home tiers' host_files row](../reference/macos-user-home-tiers.md#nothing-is-delivered-through-a-link-the-layout-did-not-lay)).
  This plan neither widens nor closes it; file it in the backlog, do not fix it here.
- **`nativehome.AccountIdle` is account-wide.** With concurrency, session-config retirement waits
  until every workspace's sessions end. That is intended ([MCWP-D12](#MCWP-D12)); do not narrow it.
- **Guest `/tmp` and `/var/folders` stay shared** by every session of the account. They are not
  workspace HOME/state and are outside the claim; do not add them to the `.yolo` rule.

## Tasks

### Slice 1 — pure geometry (independent)

- **Settled:** HOME is `<ws>/.yolo/native-home`; machine dirs are `.cache`, `Library/Caches` and
  the selected `scope: machine` dirs; no mise link; mirrors stay. Do not re-open.
- **Preconditions:** `git merge-base --is-ancestor b0f156fb1 HEAD`; `DeriveDarwinHomeLayout` still
  has signature `(home, sidecar string, writableDirs, sharedDirs []string)`. Otherwise stop.
- **Tests first** — append to `internal/entrypoint/darwinhomelayout_test.go`:

  ```go
  func TestDeriveDarwinWorkspaceHomeLayoutKeepsMachineDirsOutOfTheWorkspace(t *testing.T) {
  	ws := "/Users/Shared/yolo/proj"
  	home, machine, sidecar := ws+"/.yolo/native-home", "/Users/_yolojail", ws+"/.yolo/home"
  	l := DeriveDarwinWorkspaceHomeLayout(home, machine, sidecar,
  		[]string{".claude", ".pi/agent"}, []string{".claude-shared-credentials", ".pi-shared-npm"})
  	if l.Home != home || l.Machine != machine || l.Sidecar != sidecar {
  		t.Fatalf("roots = %q, %q, %q", l.Home, l.Machine, l.Sidecar)
  	}
  	links := map[string]string{}
  	for _, ln := range l.Links {
  		links[ln.Path] = ln.Target
  	}
  	for p, want := range map[string]string{
  		home + "/.npm-global": sidecar + "/npm-global",
  		home + "/.local":      sidecar + "/local",
  		home + "/go":          sidecar + "/go",
  		home + "/.yolo/bin":   sidecar + "/yolo-bin",
  		home + "/.config":     sidecar + "/config",
  		home + "/.claude":     sidecar + "/claude",
  		home + "/.pi/agent":   sidecar + "/pi/agent",
  	} {
  		if links[p] != want {
  			t.Errorf("%s -> %q, want %q", p, links[p], want)
  		}
  	}
  	if len(l.Links) != 7 {
  		t.Errorf("%d workspace links, want 7: %v", len(l.Links), l.Links)
  	}
  	machineLinks := map[string]string{}
  	for _, ln := range l.MachineLinks {
  		machineLinks[ln.Path] = ln.Target
  	}
  	for p, want := range map[string]string{
  		home + "/.cache":                     machine + "/.cache",
  		home + "/Library/Caches":             machine + "/Library/Caches",
  		home + "/.claude-shared-credentials": machine + "/.claude-shared-credentials",
  		home + "/.pi-shared-npm":             machine + "/.pi-shared-npm",
  	} {
  		if machineLinks[p] != want {
  			t.Errorf("machine link %s -> %q, want %q", p, machineLinks[p], want)
  		}
  	}
  	if len(l.MachineLinks) != 4 {
  		t.Errorf("%d machine links, want 4: %v", len(l.MachineLinks), l.MachineLinks)
  	}
  	for _, m := range l.Mirrors {
  		if want := machine + "/" + filepath.Base(m.Path); m.Target != want {
  			t.Errorf("mirror %s -> %q, want %q", m.Path, m.Target, want)
  		}
  	}
  	if len(l.Mirrors) != 2 {
  		t.Errorf("%d mirrors, want one per machine-scope dir: %v", len(l.Mirrors), l.Mirrors)
  	}
  	for _, d := range l.Dirs {
  		if !strings.HasPrefix(d, sidecar+"/") {
  			t.Errorf("Dirs creates %s outside the sidecar; machine dirs belong in MachineDirs", d)
  		}
  	}
  	if want := []string{machine + "/.cache", machine + "/Library/Caches",
  		machine + "/.claude-shared-credentials", machine + "/.pi-shared-npm"}; !slices.Equal(l.MachineDirs, want) {
  		t.Errorf("MachineDirs = %v, want %v", l.MachineDirs, want)
  	}
  	for _, ln := range slices.Concat(l.Links, l.MachineLinks) {
  		if strings.Contains(ln.Path, "/.yolo/mise") {
  			t.Errorf("%s: MISE_DATA_DIR names the machine store; HOME gets no mise link", ln.Path)
  		}
  	}
  }

  func TestDeriveDarwinWorkspaceHomeLayoutWithOneRootIsTodaysLayout(t *testing.T) {
  	home, sidecar := "/Users/_yolojail", "/Users/Shared/yolo/proj/.yolo/home"
  	w, s := []string{".claude"}, []string{".claude-shared-credentials"}
  	got := DeriveDarwinWorkspaceHomeLayout(home, home, sidecar, w, s)
  	if len(got.MachineLinks) != 0 || len(got.MachineDirs) != 0 {
  		t.Errorf("one root must add no machine links or dirs: %v %v", got.MachineLinks, got.MachineDirs)
  	}
  	if want := DeriveDarwinHomeLayout(home, sidecar, w, s); !reflect.DeepEqual(got, want) {
  		t.Errorf("one-root layout differs from today's:\n got %+v\nwant %+v", got, want)
  	}
  }
  ```

  Append to `internal/paths/paths_test.go`:

  ```go
  func TestWorkspaceNativeHomeIsBesideTheSidecar(t *testing.T) {
  	ws := "/Users/Shared/yolo/proj"
  	if got, want := WorkspaceNativeHome(ws), "/Users/Shared/yolo/proj/.yolo/native-home"; got != want {
  		t.Errorf("WorkspaceNativeHome = %q, want %q", got, want)
  	}
  	if filepath.Dir(WorkspaceNativeHome(ws)) != filepath.Dir(WorkspaceHomeState(ws)) {
  		t.Error("the native home is not a sibling of the sidecar")
  	}
  }
  ```

  Red: `go test -short -run 'TestDeriveDarwinWorkspaceHomeLayout|TestWorkspaceNativeHome' ./internal/entrypoint ./internal/paths`
  fails to compile (`undefined: DeriveDarwinWorkspaceHomeLayout`, `undefined: WorkspaceNativeHome`).
  Add imports (`reflect`, `slices`, `strings`) as the file needs.
- **Change:** `paths.WorkspaceNativeHome` beside `WorkspaceHomeState` (`paths.go:714`).
  `DeriveDarwinWorkspaceHomeLayout` holds today's body (`darwinhomelayout.go:163`); when
  `machine != home` the shared-dir loop puts real dirs in `MachineDirs` (not `Dirs`), points
  mirrors at `machine`, and emits `MachineLinks` for `.cache`, `Library/Caches` and each shared
  dir, in that order. `DeriveDarwinHomeLayout` becomes a one-line delegation. Nothing calls the
  new form in production yet.
- **Gates:** the red command green; `go test -short ./internal/entrypoint ./internal/paths ./internal/macosuser`; `just format`.
- **Fences:** no change to `Apply`, the bootstrap, the profile, or any caller.
- **Commit:** `feat(macos-user): derive a private workspace home layout`
- **Report:** the red output, the green gates.

### Slice 2 — the profile, and the native kernel instrument (independent)

- **Settled:** [MCWP-D9](#MCWP-D9). Zero `MachineAccess` keeps every existing profile byte for byte.
- **Tests first** — append to `internal/macosuser/seatbeltdenies_test.go`:

  ```go
  func TestSeatbeltProfileForAWorkspaceHomeAllowsOnlyTheNamedMachineDirs(t *testing.T) {
  	ws := "/Users/Shared/yolo/proj"
  	home := ws + "/.yolo/native-home"
  	m := MachineAccess{Root: "/Users/_yolojail",
  		Dirs: []string{".cache", "Library/Caches", ".yolo/mise", ".claude-shared-credentials"}}
  	p := SeatbeltProfileWithMachine(ws, home, m, nil, HomeReadonly{}, nil, nil, nil)
  	if strings.Contains(p, `(subpath "/Users/_yolojail")`) {
  		t.Error("the private-home profile re-allows the whole account home")
  	}
  	for _, want := range []string{
  		`(deny file-read* file-write* (regex #"/\.yolo(/|$)"))`,
  		`(subpath "/Users/_yolojail/.cache")`,
  		`(subpath "/Users/_yolojail/Library/Caches")`,
  		`(subpath "/Users/_yolojail/.yolo/mise")`,
  		`(subpath "/Users/_yolojail/.claude-shared-credentials")`,
  		`(literal "/Users/_yolojail")`,
  		`(literal "/Users/_yolojail/Library")`,
  		`(literal "/Users/_yolojail/.yolo")`,
  		`(literal "/Users/Shared/yolo/proj/.yolo")`,
  		`(literal "/Users/Shared/yolo/proj/.yolo/native-home")`,
  	} {
  		if !strings.Contains(p, want) {
  			t.Errorf("the profile lacks %s", want)
  		}
  	}
  	at := func(id string) int { return strings.Index(p, "#seatbelt-test-id:"+id+"#") }
  	deny, wAllow, rAllow := at("workspace-state-deny"), at("workspace-write-allow"), at("workspace-read-allow")
  	if deny < 0 || deny > wAllow || deny > rAllow {
  		t.Errorf("the .yolo deny must precede both workspace allows (deny %d, write %d, read %d)", deny, wAllow, rAllow)
  	}
  	for _, id := range []string{"machine-root-anchor-deny", "workspace-home-anchor-deny"} {
  		if i := at(id); i < 0 || i < wAllow {
  			t.Errorf("%s missing or before the write allow it must beat (%d)", id, i)
  		}
  	}
  	if got, want := SeatbeltProfileWithMachine(ws, "", MachineAccess{}, nil, HomeReadonly{}, nil, nil, nil),
  		SeatbeltProfile(ws, "", nil, HomeReadonly{}); got != want {
  		t.Error("zero MachineAccess changed today's profile")
  	}
  }
  ```

  Red: `go test -short -run TestSeatbeltProfileForAWorkspaceHome ./internal/macosuser` (compile
  failure: `undefined: MachineAccess`). Also add a `SeatbeltCaptureProfile` case asserting the same
  `.yolo` regex line precedes its staging allow.
- **Change:** `type MachineAccess struct { Root string; Dirs []string }` (a contract: the
  integration test builds one). `seatbeltProfile` takes it; private mode is `Root != ""`. Rule
  placement per [MCWP-D9](#MCWP-D9); the anchor denies sit with `homeReadonlyDenies`.
  `SeatbeltProfileWithMachine` is the exported wrapper (name is cheap). Extend the registry test
  to union ids from a private-mode render, and add `seatbeltRules` entries for the three new ids,
  each `unproven: "proved by TestMacosUserWorkspaceHomesSeatbelt*, run as the sandbox account"`.
- **New native test**, `integration/macosuserworkspacehomes_seatbelt_test.go`, gated by
  `requireMacosUser`, named `TestMacosUserWorkspaceHomesSeatbelt…` so `-run '^TestMacosUser'`
  picks it up. Fixtures (nonce in every name): workspaces A and B from `macosUserWorkspace`, each
  with `.yolo/native-home/canary` and `.yolo/home/canary`; a neutral root
  `/private/var/tmp/yolo-it-mcw-<nonce>/C/.yolo/native-home/canary` (outside `/Users` and outside
  the writable roots), made `a+rwX` so the bare control can write it; as the sandbox account,
  `/Users/_yolojail/.yolo-it-mcw-<nonce>-sel/f`, `…-unsel/f` and `…-private` (a bare account-root
  file). Render A's profile with `MachineAccess{Root: SandboxHome(), Dirs: [".yolo-it-mcw-<nonce>-sel"]}`.
  Every case runs bare first **as the sandbox account** and must succeed, then under the profile:

  | Case | Under A's profile |
  | :--- | :--- |
  | read, write, create, unlink, rename in A's HOME and sidecar | allowed |
  | the same five on B's HOME and sidecar canaries, by absolute path | refused |
  | read B's canary through `A/../B/…`, an absolute link and a relative link planted in A's HOME | refused |
  | read and write C's canary (neutral root outside `/Users`) | refused |
  | read and write `…-sel/f` | allowed |
  | read `…-unsel/f`, read `…-private` | refused |
  | unlink or rename `/Users/_yolojail/.yolo-it-mcw-<nonce>-sel` itself | refused (anchor) |
  | rename `A/.yolo/native-home` | refused (anchor) |
  | record `echo ~`, `dscl . -read /Users/_yolojail NFSHomeDirectory`, `ssh -G localhost \| grep -i knownhosts` | logged, not asserted ([MCWP-D6](#MCWP-D6)) |

  Clean up as the sandbox account (`runAsSandboxQuiet`). Add a Linux twin that runs only the bare
  controls against a temp tree, as `TestMacosUserSeatbeltContentControlsRunUnsandboxed` does, so
  quoting slips surface without a Mac.
- **Gates:** the red test green; `go test -short ./internal/macosuser ./integration`; `just format`.
- **Fences:** no caller passes a non-zero `MachineAccess` yet.
- **Commit:** `feat(macos-user): render the private-workspace-home Seatbelt profile`
- **Report:** red, green, and the twin's output.

### Slice 3 — the bootstrap lays a private home over the machine root (after 1)

- **Settled:** [MCWP-D3](#MCWP-D3), [D4](#MCWP-D4), [D7](#MCWP-D7), [D8](#MCWP-D8), [D10](#MCWP-D10), [D11](#MCWP-D11).
- **Tests first** (Linux, real filesystem under `EvalSymlinks(t.TempDir())`, in
  `internal/entrypoint/darwinhomelayout_test.go` unless noted):
  1. `TestTwoWorkspaceHomesDoNotRepointEachOther`: lay A then B over one machine root with
     disjoint packs; every link of A still reads its own target; a file written through A's
     `.claude-shared-credentials` is read through B's.
  2. `TestANestedHookResolvesThroughTheSidecarMirror`: run the real `shared_directory` hook for
     `from: ".pi/agent/npm"`, `at: ".pi-shared-npm"`; `os.Stat` through the link reaches
     `<machine>/.pi-shared-npm`.
  3. `TestTheBootstrapRefusesALinkedWorkspaceHome`: a link at `<ws>/.yolo/native-home`, and
     separately at `<HOME>/.yolo`, yields `*LinkedSidecarError` and writes nothing into the
     link's target.
  4. `TestWriteLoginRCDoesNotFollowAPlantedLink`: `<HOME>/.zshrc -> <other>/x`; after the boot
     `<other>/x` is unchanged and `.zshrc` is a regular file.
  5. `TestHostFilesThroughASharedDirReachTheMachineRoot`: extend the existing
     `homeFileThroughLayout` cases with a destination under a shared dir in private mode.
  6. `TestMachineTierStepWaitsForTheLock`: hold the lock file from the test, start
     `InstallDarwinHomeLayout` in a goroutine, assert it has not returned after 200 ms, release,
     assert it returns nil. One run; no loops.
  7. In `internal/cli`, `TestGuestVerbsJudgeScopeByTheAccount`: with the helper's ownership seam
     pointing at a temp "account" dir and `HOME` inside the workspace, `paths.EnsureWorkspaceStateDir`
     succeeds; with the variable unset it refuses as today.

  Red: `go test -short -run 'TestTwoWorkspaceHomes|TestANestedHook|TestTheBootstrapRefusesALinked|TestWriteLoginRCDoesNot|TestHostFilesThroughAShared|TestMachineTierStep' ./internal/entrypoint`
  and `go test -short -run TestGuestVerbsJudgeScope ./internal/cli`.
- **Change:** `DarwinMachineHomeEnv = "YOLO_DARWIN_MACHINE_HOME"` beside `DarwinHomeSidecarEnv`;
  `darwinHomeLayoutFor` (`:1009`) uses the new deriver with it (absent means `e.Home`).
  `Apply` order: HOME-chain refusal, sidecar refusal, `Dirs`, then under the lock `MachineDirs`
  (refusing a link or non-directory at each, as the cache step refuses `~/.cache`) and
  `InstallDarwinCacheRelocations` on `<machine>/.cache`, then `Links`, `MachineLinks`, `Mirrors`,
  redirects. `InstallDarwinHomeLayout` (`:763`) stops calling the cache step separately.
  `homeFileThroughLayout` treats a machine link like a layout link and lets `restart` accept
  targets under `l.Machine`. `RunDarwinBootstrap` refuses a missing or linked HOME when the
  machine variable differs from it, naming `yolo` to retry (the host creates HOME, slice 4).
- **Gates:** red tests green; `go test -short ./internal/entrypoint ./internal/cli ./internal/paths ./cmd/...`; `just format`.
- **Fences:** `internal/macosuser` untouched; production launches still pass no machine variable.
- **Commit:** `feat(macos-user): lay a private workspace home over the machine root`

### Slice 4 — one HOME authority through the plan and host (after 2 and 3)

- **Settled:** [MCWP-D5](#MCWP-D5), [D2](#MCWP-D2), [D11](#MCWP-D11), [D12](#MCWP-D12). Production still
  gets the account home: `LaunchHomes` returns `{SandboxHome(), SandboxHome()}`.
- **Tests first** (`internal/macosuser/workspacehome_test.go`, new): build a plan through
  `BuildRunPlanWithStages` with homes `{ws/.yolo/native-home, /Users/_yolojail}` (a test seam on
  `LaunchHomes`, or a `PlanSession` field; cheap) and assert:
  - bootstrap argv: `HOME=` and `JAIL_HOME=` the native home, `YOLO_DARWIN_MACHINE_HOME=/Users/_yolojail`;
  - launch, provision, daemon and probe argvs: `HOME=` the native home,
    `MISE_DATA_DIR=/Users/_yolojail/.yolo/mise`, `YOLO_DARWIN_MACHINE_HOME=/Users/_yolojail`;
  - `YOLO_DARWIN_LOGIN_PATH` starts with `<native home>/.yolo/bin/block` and contains
    `/Users/_yolojail/.yolo/mise/shims`;
  - the profile has no `(subpath "/Users/_yolojail")` and has each selected shared dir's subpath;
  - `SessionHomeDir` is under `<native home>/.yolo-native-sessions/`, and `PlanInvariants(plan)` is empty;
  - with homes `{SandboxHome(), SandboxHome()}` the plan equals today's (compare against a plan
    built before the change; any existing golden is the oracle).

  In `internal/cli/run`: the briefing home and the agent env file paths follow `LaunchHomes`;
  `prepareWsState` creates `native-home` only for macos-user.
- **Change:** per the [map](#map). `sandboxEnvPairs` takes `Homes` and emits the machine variable
  only when the two differ. New invariant in `PlanInvariants`: every sandbox argv carries one
  `HOME=` value. `claudeSecureStorageDir` keeps the machine spelling
  (`/Users/_yolojail/.claude-shared-credentials`).
- **Tests that will break, and are rewritten to the parameter, not relaxed:** every test that
  hard-codes the runtime HOME. `rg -l 'SandboxHome\(\)' internal --glob '*_test.go'` lists the
  candidates; those asserting a *machine* path (mise, secure storage, capture's denied home,
  preconditions) keep `SandboxHome()`.
- **Gates:** `go test -short ./internal/macosuser ./internal/cli/... ./internal/entrypoint`;
  the integration tests that read the changed symbols (`rg -l 'BuildRunPlan|SeatbeltProfile|LaunchArgv|SandboxHome' integration`,
  then `go test -run '<those tests>' ./integration`); `just format`.
- **Fences:** `LaunchHomes`' production body stays the account home.
- **Commit:** `refactor(macos-user): carry one launch HOME through the plan`

### Slice 5 — switch the launch HOME (after 4, and after V1 passes on its branch build)

- **Change:** `LaunchHomes` returns `{paths.WorkspaceNativeHome(resolvePathAbs(ws)), SandboxHome()}`.
  The hold stays. Update the code comments that state HOME is the account home
  (`darwinhomelayout.go` header, `WriteLoginRC`, `SandboxMiseData`, `printCacheRelocations`).
- **Tests first:** flip slice 4's oracle: the default plan now carries the native home.
  `TestASecondWorkspaceLayoutRepointsTheFirstsLinks` stays (it pins the legacy mode the hold
  protects) until slice 6.
- **Gates:** `just check-ci`; the integration tests that read `internal/macosuser`; nested-jail
  check per [AGENTS.md](../../AGENTS.md#testing) (plan only; Seatbelt is unreachable there).
- **Commit:** `feat(macos-user): give each workspace its own HOME`

### Slice 6 — remove the account-home hold (after 5, and after V2 passes on its branch build)

- **Change:** delete `accounthomehold.go` and its test, `Deps.HoldAccountHome`, the
  `account_home` step in `RunMacosUser` (`orchestrator.go:946-961`), the `hold` parameter of
  `PreflightLaunch` (`orchestrator.go:2323`), and the wiring in `commands.go:1472` and
  `autocapture.go:136`. Replace
  `TestASecondWorkspaceLayoutRepointsTheFirstsLinks` with slice 3's non-repoint test as the
  evidence.
- **Gates:** `just check-ci`; full `go test -count=1 -timeout 0 ./integration`.
- **Commit:** `feat(macos-user): run workspaces concurrently`

### Slice 7 — delete the single-home mode (after 6; optional cleanup)

`DeriveDarwinHomeLayout`'s one-root path and the `machine == home` branches have no production
caller left (the install capture lays no layout). Delete them and their tests.
Commit: `refactor(macos-user): drop the shared account-home layout`.

## Native verification — a human runs these

Not executed here. Use a disposable Mac where `yolo macos-setup` already ran. No credentials, no
real agent session, no `sudo -v`, no account or ACL changes beyond what setup made. Record for
each run: the commit, `sw_vers`, `uname -m`, the rendered profile, the physical roots, and every
case's stdout and status.

- **V0 — kernel feasibility, after slice 2 lands.** Dispatch the
  [macos-user workflow](../../.github/workflows/macos-user.yml) on that commit (or run
  `YOLO_TEST_MACOS_USER=1 go test -count=1 -timeout 0 -v -run '^TestMacosUserWorkspaceHomesSeatbelt' ./integration`
  on the Mac). Every case in slice 2's table must pass with its control. A failure here (the
  regex not matching, physical resolution through links, a missing traversal literal) changes
  slice 3 before it is built, so run V0 before starting slice 3 where possible. Count the cases
  run: zero is not evidence.
- **V1 — single workspace, on slice 5's branch build.** `just build-dev-bundle` on the Mac, then
  with the emitted launcher in a fresh workspace under `/Users/Shared/yolo`: the full
  `^TestMacosUser` suite green; `yolo -- sh -c 'echo $HOME; readlink ~/.config ~/.cache'` shows
  the native home and the two expected targets; `yolo -- bash -lc 'echo $PATH'` and
  `yolo -- zsh -lc 'echo $PATH'` start with the session prefix; a fake executable placed in
  `~/.local/bin` by one launch is found by the next; a second launch of the same workspace still
  waits on the workspace lock.
- **V2 — concurrent workspaces, on slice 6's branch build.** A and B launched together, then in
  reverse order. Each holds a shell that re-reads its HOME, a private canary, `~/.npm-global`,
  `~/.local`, `~/go` and login PATH before and after the other's bootstrap; nothing changes.
  From A's shell, slice 2's sibling cases against B's real paths are refused. A selected shared
  dir written from A is read from B. An interrupted bootstrap (Ctrl-C), a killed host `yolo`, and
  a detached child left running each leave both homes intact; session teardown removes only
  session files. Run both relaunches with `cache_relocations` configured, and confirm neither
  bootstrap fails on the lock.

## Ships with

- **Tests:** unit cases named in each slice; the integration test from slice 2; V0 to V2 above.
  `TestMacosUserHomeRootHostFilesArePerWorkspace` should pass unchanged (its link is relative);
  if it does not, that is a finding, not a test to relax.
- **Docs that describe the old layout:**
  [`macos-user-home-tiers.md`](../reference/macos-user-home-tiers.md) (the layout, HT-D15 and
  "What is still shared"), [`jail-home.md`](../reference/jail-home.md),
  [`macos-user-provisioning.md`](../reference/macos-user-provisioning.md),
  [`settings-per-setup.md`](../../userguide/reference/settings-per-setup.md),
  [`macos.md`](../../userguide/guides/macos.md). Updated in slice 5 (HOME) and slice 6
  (concurrency); check each behavioral claim, not only links.
- **Changelog:** one `### Added` entry at slice 6 (two workspaces at once on `macos-user`), one
  `### Changed` line at slice 5 if a user can notice HOME moving (per-workspace `~/.ssh`,
  `~/Library` outside caches, and a passwd-home tool failing).
- **Messages:** `accountHomeLive` and its siblings go with slice 6; `printCacheRelocations` names
  the private home in slice 5.
- **Graduation:** after slice 6, fold the design into
  [`macos-user-home-tiers.md`](../reference/macos-user-home-tiers.md) per `system-doc`, delete the
  design and this plan, and update the roadmap entry in the same commit.

## Don't

- Don't change the directory-service record, add a setup grant, or re-allow any part of
  `/Users/_yolojail` beyond the named machine dirs to make a passwd-home tool work ([MCWP-D6](#MCWP-D6)).
- Don't link the whole workspace `.yolo`, or set HOME to `.yolo/home`.
- Don't migrate, copy, chown or delete anything in the old account home or an existing sidecar.
  Old account-home links and rc files stay where they are, unused.
- Don't add any reaper, prune rule or `yolo stores` reclaim for `native-home` ([OQ-MCW3](macos-user-concurrent-workspaces.md#decision-ledger)).
- Don't remove the hold in the commit that switches HOME, and don't land either before its V run.
- Don't reuse session cleanup (`nativehome.ClaimGone`, pack-tree proofs) for persistent HOME.

## Blockers

None. No design question is open; V1 and V2 are hardware gates on two named slices, not questions.
