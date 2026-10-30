---
title: "Mac native checks: two runbook fixes, and a workspace whose parent the sandbox cannot enter"
date: 2026-10-29
tags: [lab-report, macos, macos-user]
summary: "The inherited-scope test fix and the launch-setting warning fix hold natively. New: a macos-user workspace outside the shared root under a private parent loops between two refusals that name the same command."
---

# Mac native checks: two runbook fixes, and an unshared parent

**Run 2026-10-29, late, on the maintainer's Mac** (macOS 26.5, arm64). The source is `main` at
`3cad08c6a`, deployed with `just deploy` as `yolo-jail 0.15.1+804.g3cad08c6ad15`. This follows
[the native comparison runbook run](2026-10-29-mac-native-witnesses-runbook.md), whose two new defects
have since been fixed on `main`. The disposable account `yolo-witness` from that run has been removed
by the maintainer (`id yolo-witness`: no such user; `/Users/yolo-witness` is gone).

**Terms.** *macos-user* is the backend that runs the agent under macOS Seatbelt as a dedicated
account, `_yolojail` ([macOS backends](../../userguide/guides/macos.md)). The *shared root* is
`/Users/Shared/yolo`, the directory `yolo macos-setup` shares with that account through a
group access-control entry (ACE) that new children inherit. A *precondition* is one of the checks
a macos-user launch makes before it builds anything
([`preconditions.go`](../../internal/macosuser/preconditions.go)).

## 1. Results

| Fix | Commit | Native result |
| :--- | :--- | :--- |
| Inherited-scope test probes the delivered path | `14c00e994` | Passes ([§2](#2-inherited-scope-test)) |
| No unknown-setting warning for a setting the launch obeys | `d366fd0df` | Holds ([§3](#3-launch-setting-warning)) |
| A pruned image's stale delivery record cannot skip its reload | `6b0e53ed4` | Not checked: needs an Apple Container launch with an image load ([§5](#5-not-done)) |

New defect: a workspace outside the shared root, under a directory the sandbox account cannot
enter, gets a refusal whose command does not help, then a second refusal naming the same command
([§4](#4-new-defect-a-workspace-under-a-private-parent-loops)).

## 2. Inherited-scope test

`14c00e994`'s message says "The macOS test itself is unverified natively after this change."

```
YOLO_RUNTIME=macos-user go test -count=1 -v \
  -run 'TestMacosUserInheritedUserScopeArrivesReadOnly|TestMacosUserOverlayInstallKeepsTheAgentStateBesideItsSkills' ./integration
```

Both subtests of `TestMacosUserInheritedUserScopeArrivesReadOnly` (`toml` and `jsonc-fallback`) ran,
none skipped, and the test passed in 11.05s. `TestMacosUserOverlayInstallKeepsTheAgentStateBesideItsSkills`
passed in 5.77s. Package result: `ok … 30.386s`.

## 3. Launch-setting warning

A macos-user launch from a fresh workspace under the shared root, with `{}` as its config:

```
YOLO_RUNTIME=macos-user YOLO_CLAUDE_CREDENTIAL_VIEW=0 yolo -- true
```

It printed no `not a recognized launch setting` line, and once the patch-series hatches in
[§6](#6-also-seen) were added it exited 0. A control launch with `YOLO_NOT_A_REAL_SETTING=1` still
warned, now with the new next step:

```
yolo: warning: YOLO_NOT_A_REAL_SETTING is not a recognized launch setting; unset YOLO_NOT_A_REAL_SETTING. Run `yolo config-ref` for the documented launch settings.
```

## 4. New defect: a workspace under a private parent loops

**Not fixed.** Reproduced twice: in a session scratch directory under `/private/tmp/claude-501`
(mode `drwx------`), and in a plain `mktemp -d` workspace under `$TMPDIR`
(`/var/folders/j6/…/T`, also `drwx------`). Both are outside every user's home, so the
workspace-location precondition passes.

**Steps and output** (the `mktemp -d` case):

1. `cd "$(mktemp -d)" && echo '{}' > yolo-jail.jsonc && YOLO_RUNTIME=macos-user yolo -- true`
   exits 1 with the workspace-sharing refusal. It says *"Most likely this project was MOVED or
   copied into /Users/Shared/yolo"*, which is false (the workspace is not under the shared root),
   and names `yolo macos-fix-permissions <workspace>`.
2. `yolo macos-fix-permissions <workspace>` exits 0 and reports `✓ Applied shared-group ACLs`.
3. The same launch again exits 1 later, after the build. Six boot steps fail with
   `mkdir /private/var/folders/j6/…/T/tmp.DszVREfxtJ: permission denied` (`darwin_home_layout`,
   `generate_mise_config`, `prepare_session_homes`, `session_render_lock`, `install_home_overlay`,
   `generate_bootstrap_script`). The refusal says *"this is usually the workspace ACL"* and names
   the same `yolo macos-fix-permissions` command as its fix, spelled with `/private`.

Running that command again changes nothing, so the user goes back to step 3 every time. This breaks the
rule that every stop names a next step that works
([happy-path principle](../reference/happy-path-principle.md)). The sharing precondition also exists
to stop exactly this, a launch that "would fail partway through provisioning", per its own refusal
text.

**Cause, from the source.**

- The sharing precondition (`PreconditionWorkspaceShared`, the last launch precondition in
  [`preconditions.go`](../../internal/macosuser/preconditions.go#L348-L395)) asks only whether the
  workspace directory itself carries a `_yolojail` ACE (`WorkspaceGrantedScript`, one
  `ls -lde <workspace> | grep`). It does not ask whether the sandbox account can reach the workspace
  through its parents.
- Its comment assumes every workspace that passes the location check is under the shared root
  ("after setup it is the only one left"). Its refusal text hard-codes that case.
- `yolo macos-fix-permissions <path>` applies the ACE to the tree at that path only. It does not
  change a parent. So after step 2 the precondition passes, while the sandbox account still cannot
  get past the parent (`drwx------`, owned by the user).

**What a human should decide.** The happy-path rule needs a next step that works. One option is
to refuse a workspace outside the shared root with a plain message saying it must live under
`/Users/Shared/yolo`. The other is to check that each parent directory can be entered, and name
the one that blocks the sandbox account. Self mode (`"macos_user_mode": "self"`) skips this
precondition by design, so neither case affects it.

## 5. Not done

- **The stale delivery record.** `image-delivery/10c5c729c035f116.delivered.json` from the earlier
  prune is still present. `6b0e53ed4` says a record whose image is gone is deleted when an Apple
  Container launch builds its delta archive, and that no launch can skip its load because of it.
  No Apple Container launch has run since, so this run did not observe that deletion.
- **[OQ-MT6](../design/macos-native-tls.md#OQ-MT6)** (a company-installed certificate) still needs
  a network that inspects HTTPS. **[OQ-MT11](../design/macos-native-tls.md#OQ-MT11)** is still
  unreachable behind the Copilot compatibility guard.

## 6. Also seen

The maintainer's own `matt` pack stopped every launch: three extension patch series no longer
apply to new upstream releases. These are user-pack maintenance, not yolo defects.

| Extension | Upstream that refuses the series | Conflicting patch |
| :--- | :--- | :--- |
| `matt/pi-subagents` | v0.84.0 (`0398b9e8`) | `0002-feat-subagents-load-optional-child-extensions-into-f.patch` (`src/runs/shared/child-session.ts`) |
| `matt/pi-archimedes` | v2.12.0 (`bb2ce4cd`) | `0010-test-declare-fast-check-in-test-owning-packages.patch` |
| `matt/pi-session-search` | v1.6.1 (`727d18d2`) | `0002-feat-index-session-state-entries-and-tool-call-argum.patch` |

`YOLO_ALLOW_PATCH_FAILURES=1` let the launch continue with the last good builds of the second and
third. `pi-subagents` has no earlier admitted build, so that hatch does not cover it; its refusal
offers `YOLO_ALLOW_OLDER_UPSTREAM`, `YOLO_SKIP_PATCHES` and `YOLO_SKIP_EXTENSIONS` instead. This
run used `YOLO_SKIP_EXTENSIONS=matt/pi-subagents` for the throwaway launches only. Each refusal
prints the git commands that rebase its series.

## 7. Cleanup

- Removed the three throwaway workspaces (the scratch directory, the `mktemp -d` directory and
  `/Users/Shared/yolo/warn-check`) and the throwaway SSH key for the removed account.
- Kept: `/Users/Shared/yolo/witness-account.sh`, the account script, for a future disposable run.
