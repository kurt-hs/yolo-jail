---
title: "Plan: October 18 serial landing and verification"
status: accepted
stage: DECIDED
next: "Verify the combined candidate, fast-forward it to unchanged main, then run just done on clean main"
summary: "Records the prior model/Mac landing, full patch/live candidate histories, exact review closure, combined validation and remaining platform obligations."
---

# Plan: October 18 serial landing and verification

**Baseline:** primary `main` was clean at `6683038d29e073e015af9396b117fa36f1e9f63d` before this serial integration. This isolated candidate merges the full patch-branch history through `770d9c2c9f83592d2c904de23a1f4fb36bd9cdb0` and the full live-config history through `e80f3a2ae3acc3724b83aaa66b207edd9cbd18f4`; neither history was reduced to its final repair commit. Both are ancestors of the candidate. The candidate's verified fast-forward to main is pending.

## Prior model and Mac landing at `6683038d2`

The model and Mac/recovery implementation landed before this serial candidate. It is not new behavior authored by this candidate. Original Mac commits `98715e67b` and `41dd11e3f` remain through recovery merge `48b78e3d3`; the model implementation, exact-id repair and test-isolation repair remain through `aebeb0e6e` and their ancestors. The combined landing also retains the stronger model/Mac reconciliation from `203182fd4`.

- **Model review:** independent review returned **ACCEPT with notes**, identifying a P2 in all-profile host tests: a negative-only assertion could pass without reaching registry resolution. The committed repair added, for every shipped Pi profile, a positive known qualified reference and a distinct unresolved control; it requires a checked-reference result for the former and the diagnostic for the latter. It did not weaken the existing assertions. `TestCheckResolvesEveryShippedPiProfileModelReference` and `TestPiProfileAndSubagentsExactIDOutranksDefaultAlias` are the named regression evidence.
- **Mac review:** the candidate was independently reviewed before its committed fixes, then proportionately rechecked against those findings; no additional broad panel was started. The stable DM1–DM4 ruling IDs and the Mac design's [§6.2 doorway](../design/macos-user-suite-on-a-developer-mac.md#62-another-sessions-doorway-was-read-as-this-sessions-survivor) and [§6.3 housekeeping](../design/macos-user-suite-on-a-developer-mac.md#63-the-housekeeping-tests-slot-was-not-due-because-its-stamp-is-the-machines) diagnoses remain intact. Changes include exact-count `sudo` allowances, a product-shaped non-root sandbox command/cleanup path, path checks across `/`, process listing for both relevant accounts, and honest countable unified-log skips. Native-Mac execution remains unverified.
- **Shared records:** the model-reference release notes and named model/Mac mappings from this landing are retained. The Mac test/documentation work gets no separate user-facing changelog claim; the live config command does. Census output was regenerated and compared; broad environmental drift was restored rather than committed when it did not represent new test readers.

### Historical implementation/test excerpts

These bounded excerpts come from committed historical diffs, not new implementation claims or raw session transcripts: Mac implementation/test/ruling hunks from `1a9ded40b..4bc941d53`, model implementation and exact-id-test hunks from `4bc941d53..bfb4f6f47`, and the all-profile positive/negative host check from reconciliation `bfb4f6f47..203182fd4`. The later `6683038d2` gate-record commit retained the verification checkpoint. The full bounded excerpt collection and original merge-range patches remain in the run artifact; these source-named call sites and regression shapes preserve the earlier landing's reviewable evidence in-tree.

```diff
--- a/internal/entrypoint/packsurfaces.go
+++ b/internal/entrypoint/packsurfaces.go
@@
+ reportModelReferences(e, packs)
```

The committed Pi derive now checks an exact declared model id before falling back to the provider's default alias; the exact-id precedence is pinned by `TestPiProfileAndSubagentsExactIDOutranksDefaultAlias`.

```diff
--- a/packs/pi/derive.lua
+++ b/packs/pi/derive.lua
@@
   local _, id = yolo.model_for(value, provName)
-  if not id and value ~= "default" then
-    _, id = yolo.model_for("default", provName)
-  end
   if id then return id end
   for _, modelId in pairs(models) do
     if modelId == value then return value end
   end
+  if value ~= "default" then
+    _, id = yolo.model_for("default", provName)
+    if id then return id end
+  end
```

```diff
--- a/internal/cli/check/modelreferences_test.go
+++ b/internal/cli/check/modelreferences_test.go
@@
+func TestCheckResolvesEveryShippedPiProfileModelReference(t *testing.T) {
+    cfg := `{"packs": ["pi", "openai-auth", "zai", "openrouter", "kilo", "llamacpp", "bedrock",
+      "cerebras", "meta-provider", "wire-bridge"]}`
+    knownScope := "openai-codex/gpt-6.1-sol[1m]"
+    unknownScope := "openai-codex/yolo-no-such-model"
+    for _, profile := range []string{"codex", "zai", "openrouter", "kilo", "llamacpp", "bedrock",
+        "bedrock-bridge", "cerebras", "meta"} {
+        t.Run(profile, func(t *testing.T) {
+            out, _ := runPacksCheck(t, cfg, profile, scopePack(t, knownScope))
+            if strings.Contains(out, "matches no model") ||
+                strings.Contains(out, "Model reference: 1 model reference not checked") ||
+                !strings.Contains(out, "Model reference: every model pi's config names resolves in its registry (1 reference checked)") {
+                t.Fatalf("shipped profile %q must check and resolve the qualified scope reference %q:\n%s", profile, knownScope, out)
+            }
+            out, _ = runPacksCheck(t, cfg, profile, scopePack(t, unknownScope))
+            if !strings.Contains(out, "which matches no model in pi's registry") ||
+                strings.Contains(out, "Model reference: 1 model reference not checked") {
+                t.Fatalf("shipped profile %q must detect the unknown qualified scope control %q:\n%s", profile, unknownScope, out)
+            }
+        })
+    }
+}
```

The excerpt omits no assertion in this function: the positive known input requires checked registry resolution, and the separate unknown input requires the warning rather than an unchecked result, across all nine shipped Pi profiles.

The Mac ruling that constrained test privilege is retained in the actual landed Decision Ledger row:

```diff
+| OQ-DM1 | **The suite never needs passwordless root `sudo`** (option B, remove the need). The 2026-10-18 ruling rejects tests requesting root privileges not required by the product. Every privileged step goes through what yolo itself installs, the one run-as rule, built by `macosuser.RunAsSandboxArgv`, or is the invoking user's own; a capture plan's prepare steps run exactly as production runs them; the CA test uses a CA the System keychain already holds and never adds one; a guard test fails on a sudo the suite spells itself | 2026-10-18 | [§3.1](#31-class-a--six-tests-spent-a-privilege-yolo-macos-setup-does-not-install), [§3.3](#33-class-c--one-root-sudo-probe-that-read-as-a-missing-precondition), [§5](#5-the-shape) | Candidate implementation; native-Mac runtime unverified |
```

The Mac integration guard parses the test package on all hosts and reports an unapproved sudo literal with the owner and allowed replacement:

```diff
--- /dev/null
+++ b/integration/macosusernorootsudo_test.go
@@
+                if _, ok := allowed[key]; ok {
+                    used[key]++
+                    return true
+                }
+                t.Errorf("%s spells a sudo command (%s) in %s. The macos-user suite takes no "+
+                    "privilege `yolo macos-setup` does not install (OQ-DM1): run a step as the "+
+                    "sandbox account through sandboxCommand, which builds the one run-as rule's "+
+                    "argv (macosuser.RunAsSandboxArgv), and every other step as yourself.",
+                    fset.Position(lit.Pos()), v, name)
```

The Mac cleanup path first removes as the host user, then uses the product's guest command when needed; no new root grant was added. The host runner is constructed through the product's run-as argv:

```go
func sandboxCommand(ctx context.Context, argv ...string) *exec.Cmd {
    full := macosuser.RunAsSandboxArgv(nil, argv...)
    cmd := exec.CommandContext(ctx, full[0], full[1:]...)
    cmd.Dir = "/"
    cmd.Stdin = nil
    return cmd
}
```

The cleanup helper first removes the fixture as the invoking user, makes owned directories searchable/removable and retries, then calls only the product-declared guest cleanup commands with a deadline before one final host retry:

```diff
--- a/integration/macosusergate_test.go
+++ b/integration/macosusergate_test.go
@@
+func removeSandboxWritten(dir string) error {
+    if err := os.RemoveAll(dir); err == nil {
+        return nil
+    }
+    _ = filepath.Walk(dir, func(p string, fi os.FileInfo, err error) error {
+        if err == nil && fi.IsDir() {
+            _ = os.Chmod(p, fi.Mode().Perm()|0o700)
+        }
+        return nil
+    })
+    if err := os.RemoveAll(dir); err == nil {
+        return nil
+    }
+    for _, cmd := range macosuser.CaptureScratchGuestCommands(dir) {
+        ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
+        c := exec.CommandContext(ctx, cmd[0], cmd[1:]...)
+        c.Dir = "/"
+        _ = c.Run()
+        cancel()
+    }
+    return os.RemoveAll(dir)
+}
```


- **Initial review:** blocked on two P1 classes—relative-locator resolution/build state could cross clone boundaries, and the offline/default check required a fresh fetch rather than trusting the recorded verdict. The early candidate was not accepted.
- **Repair sequence:** the full retained history adds clone isolation, record-only reporting, offline verdict handling, and host-to-guest mapping of the branch identity. A later caller audit still **BLOCKED** with a P1: default `yolo pack series check` did not hydrate the host mapping and could reject a correct host record in a distinct guest checkout.
- **Final caller repair:** `hydrateSelectedWorkspacePackMappings` runs after loading the explicitly named manifest and before constructing its forks. It transfers only a host-established identity whose recorded workspace-relative path resolves to the exact directory the command named and whose matching contribution still declares a relative branch. The named manifest remains authoritative; another clone cannot borrow that mapping. Production regression `TestSeriesCheckHydratesOnlyTheExactSelectedWorkspacePackIdentity` uses distinct host/guest checkouts and repository identities, deletes mirrors, denies all git invocations, checks good and stale recorded verdicts, and rejects an unrelated clone. The record-only path does not fetch or touch mirrors.
- **Final review:** the caller reviewer returned **ACCEPT**, limited to this narrowed closure. It independently ran the seven targeted CLI, entrypoint, packsrc and packload regressions; no platform/VM runtime was claimed. Exact spelling, consumer mapping and test details remain in [downstream ask 7](downstream-asks.md#7-the-carry-key) and [PF-D98](../reference/patched-forks.md#PF-D98).

## Live-config-apply history and review disposition

- **Initial review:** blocked on four P1s: a mount-record write failure could falsely complete a removal; broker lock acquisition ignored deadlines; absent/unreadable broker scope could be treated as already applied; and process identity was checked after namespace mutation.
- **First repair review:** confirmed those four areas were repaired, then found a new P1: successful mount mutation invalidated its own record, preventing ordinary add/remove/replacement recovery.
- **Final repair:** `updateCtxMountRecord` restores only the launch token read from the inspected running jail and only while the record remains the empty-token invalidation tombstone. A competing launch token is not replaced. The regression drives production apply through invalidation, writer, reader and baseline fold for add→remove and remove→replacement-add; the failure regression keeps a failed write pending rather than folding it.
- **Final closure review:** returned **ACCEPT**, limited to the narrowed mount-record closure. The review confirmed tombstone/token behavior and the exact production lifecycle, plus deadline-bounded broker-scope transactions, missing/erroring live scope remaining pending, and process/mount-namespace identity pinned before source/destination mutation. It also accepted the task-owned configuring skill: one host approval gate, no alternate asker, no restart offer, and no additional confirmation.
- **Retained focused regressions:** `TestConfigApplyRestoresTheInvalidatedMountRecordThroughRealApply`, `TestInvalidatedCtxMountRecordCannotClaimStaleMounts`, `TestConfigApplyRecordFailureInvalidatesMountStateAndKeepsRemovalPending`, `TestLiveScopeTransactionsHonorContextWhileSourceLockIsHeld`, `TestApplyPlanRequiresVerifiedLiveScopeAfterApprovalIsUnchanged`, `TestInjectCtxMountRefusesChangedRuntimePIDBeforeRunningVerb`, and `TestAttachRejectsAChangedProcessOrMountNamespaceBeforeSourceMutation`. The final review's package selection passed; it does not prove a real broker exchange or rootless runtime behavior.
- **Merge conflict reconciliation:** retained both sides of the `docs/plans/BACKLOG.md` conflict: the prior mise-shim finding and the three live-review discoveries. No candidate source was discarded in resolving it.

## Candidate disposition and boundaries

| Slice | Candidate disposition |
| :--- | :--- |
| Model-reference registry, exact-id resolution, host/boot reporting and all-profile controls | Already on main at `6683038d2`; preserved unchanged. |
| Mac test privilege, log-skip, floor and recovery fixes | Already on main at `6683038d2`; preserved unchanged. |
| Relative patch-branch feature and exact named-pack offline caller closure | Included by merge of the full `770d9c2c9` history; final caller review accepted. |
| Live config apply, broker live-scope changes, `/ctx` mount injection and bounded repair | Included by merge of the full `e80f3a2ae` history; final closure review accepted with the limits above. |
| Primary movement | Still pending until the final candidate passes gates and main is confirmed unchanged and clean; then fast-forward only. |

The serial merge retains the existing model/Mac mappings, the exact selected-pack mapping, and bounded broker scope, inspected mount-token restoration and process-identity pinning. It does not widen the model-reference claims, give a patch identity to another pack, authorize a restart offer, or change the primary ref before candidate verification.

## Validation and remaining obligations

- Candidate verification passed before any primary mutation. `just format`, `git diff --check`, and `just check-ci` all returned success; check-ci included Linux and Darwin vet/staticcheck, short tests, changelog and site gates, closed-tree checks, and release checks. The authored docs were separately checked with `uvx vantage-check` (8 files, clean). The one early affected-package run exposed two ordinary `internal/cli/run` issues: the new backend parity needed its rationale, and a stale mount-token mismatch needed its accurate reason. Both were fixed at their production seam; the final affected package selection passed: `go test -count=1 ./internal/cli/check ./internal/cli ./internal/cli/run ./internal/entrypoint ./internal/packdecl ./internal/packload ./internal/packsrc ./internal/hostfloor ./internal/ctxinject ./internal/brokerscope ./internal/ghbroker`.
- `python3 scripts/completion-census.py` was run. Its generated reader map had broad environment-dependent churn; comparison found no actual new readers beyond the existing map, so unrelated output was restored. The shared Go cache was preserved. `just build-go` succeeded, then the candidate's own `dist-go/linux-amd64/yolo` ran in a nested jail from a throwaway workspace with a temporary HOME, `YOLO_REPO_ROOT` pointing at this checkout, and no selected packs: `candidate-nested-ok home=/home/agent workspace=/workspace`.
- The required combined integration suite ran once against the final candidate source and passed: `YOLO_NO_PROGRAM_READINESS=1 go test -count=1 -timeout 0 ./integration` → `ok ... 722.513s`. The nested smoke exercises the candidate binary and boot path but its Podman-in-Podman environment is structurally rootful; it is not rootless integration evidence. After confirming main remains clean at `6683038d2`, fast-forward only; `just done` remains due on clean main after that mutation.
- **Outstanding:** rootless Podman integration on both required architectures; real-broker widening/narrowing after a request; a jail's `CAP_SYS_ADMIN` attempt to remount an injected read-only mount; native-Mac execution of the Mac integration tests. These remain explicit runtime/platform obligations, not claims inferred from the combined Linux suite.
- No push, tag, release, deployment, credential change, cache cleanup, or Pi core/extension work is authorized.

## Primary landing boundary

1. The only primary mutation is `git -C /workspace merge --ff-only <verified-candidate-tip>`, after confirming primary is still at `6683038d2` and clean. If main has moved, reconcile and regate; never reset or force it.
2. Record the candidate's exact ancestry, complete diffstat, validation commands and limits in the final serial landing report. The plan's candidate-pending status is intentionally a checkpoint; a later ordinary docs update can record the completed primary fast-forward.
3. Run `just done` only after the primary fast-forward, on clean main. Do not push or publish.
