---
title: "Native per-session Pi profile homes: build plan"
date: 2026-10-20
status: accepted
stage: BUILT
next: "Run the independent final review, then measure native runtime on a Mac"
depends-on: [macos-user-session-profiles.md]
tags: [plan, macos-user, pi, sessions]
---

# Native per-session Pi profile homes: build plan

**Status:** 2026-10-20. Grounded against `f8478d3b`; implementation through `46b45756c`.
MEASURED in synthetic Linux native-plan/Darwin-bootstrap/capture fixtures and a real shell-child
exec fixture. Darwin arm64/amd64 builds, scoped vet/staticcheck and targeted regression gates pass.
UNMEASURED: actual native runtime/Seatbelt and Pi descendants. Independent final review is
parent-owned; no release publication or primary landing is claimed.

**Design:** [native contract](macos-user-session-profiles.md), with the [settled session-profile rules](per-session-profiles.md#11-decision-ledger). The design wins on behavior, the tree on facts; this plan is advice. Stop and report any substantive disagreement rather than changing behavior silently.

## Map and reuse

| Seam | Change |
| :--- | :--- |
| [runplan](../../internal/macosuser/runplan.go) | Carry the existing 16-hex ID through trusted bootstrap argv and child env; wrap launch/provisioning after the env reader. |
| [Darwin bootstrap](../../internal/entrypoint/darwin.go), [boot table](../../internal/entrypoint/bootsteps.go) | Explicit ordinary-session option, not an env-inferred pass; capture/sealed bootstrap stays excluded. |
| [session homes](../../internal/entrypoint/sessionhome.go), [capture](../../internal/entrypoint/sessioncapture.go) | Reuse declared links, render redirects, private baselines and normal overlay capture; add native workspace-qualified paths and positive-gone filtering. |
| [internal dispatch](../../internal/cli/internal.go) | Account-scoped command exec restores only ID/declared relocations from private metadata and inherits the home lease. |
| [Native session helper](../../internal/nativehome/nativehome.go) | Workspace-qualified account-owned root, lease/launch marker and account-only process census. |

## Constraints and traps

- **Must:** keep `HOME` constant and the existing cross-workspace refusal. No changes to Pi, account setup, root grants, or host native Pi settings.
- The host-owned native record protects staging only; its foreground lock alone cannot prove detached children gone. **Must:** combine a config-home lease with an account-only process census. Unknown retains bytes. Descriptor inheritance alone is insufficient because programs can close extra FDs.
- **Must:** write the launched marker only when the actual command acquires its lease, not during bootstrap or provisioning. Otherwise another launch can mistake the bootstrap-to-command interval for retirement.
- **Must:** use the same workspace namespace for production and enumeration; sidecars already live under the workspace. Do not enumerate another workspace's leftovers.
- **Must:** prevent shared render on override or failed home construction. Capture only baseline-backed, completed native renders and preserve parse/write failures and strays.
- Advice: reuse the existing shipped Pi fixture and real boot-table selection in the session-home tests. Add real Darwin-bootstrap caller coverage, not only helper tests.
- First native session may have no shared baseline: seed an empty private baseline so normal captured overlays survive later entries without rendering shared profile config. Capture legacy shared stateful sources through the normal managed delta path, advancing only sidecars. Keep sources/auth untouched and preserve dropped inventories with their baselines.

## Tasks

### 1. Identity and plan proof — independent

Tests first: append this test to the native session-file tests, then run the targeted command below. It must fail before plumbing exists.

```go
func TestNativeSessionPlanCarriesTrustedConfigIdentity(t *testing.T) {
    p := sessionPlan(t, "0123456789abcdef")
    if !containsArg(p.BootstrapArgv, "YOLO_SESSION_ID="+p.SessionID) {
        t.Fatal("bootstrap lacks the existing native session identity")
    }
    if !SandboxEnvFileSets(p.EnvFileContent, "YOLO_SESSION_ID", p.SessionID) {
        t.Fatal("children lack the same native session identity")
    }
}
```

Change: ordinary native plan/bootstrap flag and trusted child ID. Capture's shared bootstrap builder must not acquire an ID. Plan invariants pin bootstrap and command wrappers, including provisioning. Override any composed ID with the minted ID.

### 2. Private render and capture — after 1

Tests first: real boot-table Darwin sessions A/B/default retain A's bytes, shared links, override, malformed ID, failed construction; normal capture carries user settings into a later private render without changing a live snapshot. Test first-run/no baseline and a failed capture retry.

Change: generic declared home producer/consumer use workspace-qualified native root; enable the Darwin session step and capture wrapper. Export metadata only after successful render. Keep incomplete homes for retry, not capture.

### 3. Command and lifetime — after 2

Tests first: shell/child inheritance without agents; held lease, absent lease, unknown/live account census, incomplete launch, strays and cross-workspace retirement. Existing 16-hex record sweep tests remain unchanged.

Change: confined command wrapper restores ID/relocation and inherits a shared lease; provisioning uses the same relocation but does not mark a session launched. Account-scoped bootstrap cleanup requires both lease-free and no other account processes; holds lease through capture/removal.

## Gates and fences

```console
$ go test ./internal/macosuser -run 'NativeSession|TwoSessions|SessionRecord'
$ go test ./internal/entrypoint -run 'Darwin.*Session|SessionHome|SessionCapture'
$ go test ./internal/cli -run 'DarwinBootstrap|NativeSession'
$ GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build ./cmd/yolo
```

No real agent/API tests, publication, installation, deployment or primary landing. Use standard shared Go caches. Final combined gates and independent high-effort review belong to the parent. Native runtime/Seatbelt is unavailable here: record it as an unmeasured lane limit, not a release blocker. Keep the multi-workspace concurrency plan untouched. Update these two documents only to the state actually built.

## Completion evidence and remaining measurement

- Red identity proof: the native plan test failed with “bootstrap lacks the existing native
  session identity” before plumbing, then passed with the existing minted ID.
- [Native plan tests](../../internal/macosuser/sessionfiles_test.go): trusted identity,
  declaring/nondeclaring plans, command/provisioning wrapper invariants and old record grammar.
- [Darwin bootstrap tests](../../internal/entrypoint/darwinsessionhome_test.go): private A/B/default
  snapshots, shared absent-file links, first-run capture, legacy customization/non-re-adoption,
  later user edits, unknown/live descendants, failed parse retry, override/refusal, no-pack,
  changed inventory/workspace fences and strict render-lock refusal.
- [Native lease tests](../../internal/nativehome/nativehome_test.go): launch versus provisioning,
  absent/held leases, strict identity, namespace/link safety and unknown sibling retention.
- [Command tests](../../internal/cli/nativesessionexec_test.go): real dispatcher/exec, shell and
  child inheritance and lease release, without starting an agent or calling an API.
- Native runtime, Seatbelt physical symlink access, account census and real Pi foreground or
  detached children remain Mac-only measurement. The multi-workspace plan is unchanged.
- The published style-guide fallback was used initially; Vantage 0.10.0 became available
  before finalization. Both lane documents pass the repository's pinned checker.

Scope stayed in native plan/bootstrap, generic home/capture helpers and these two lane docs;
no changes to Pi core/extensions, HOME/account setup, root grants, host native config writeback,
publication or the unresolved multi-workspace architecture.
