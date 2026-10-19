---
status: in-review
stage: DESIGN
next: "Rule OQ-MCR1 and OQ-MCR2 after native Mac measurements; do not build before owner discussion and the 0.15 release"
summary: "Native Claude refresh coordination without reviving the rejected TLS terminator"
---

# Native Claude refresh needs a single writer, not a native hostname interception

**Status:** 2026-10-19. Design only, checked against `23eb5097b`; no native Claude login or refresh measured here. No build authorization; this work must land after 0.15 and owner discussion.

> **In short.** The Mac's already-started Claude broker can coordinate a login only if Claude actually consumes its refresh-token-free credential view instead of refreshing a different store. A running host half or a shared account home alone is not proof that this happens.

**Why it matters.** Two native sessions can still redeem the same single-use refresh token. A broad proxy or a second host login is not a substitute for proving which native store Claude reads.

**The shape.** Existing host broker and per-workspace views, gated by native store measurements and the owner decisions below.

**Start at [the native boundary](#the-native-boundary)** — it distinguishes the working host half from the missing client coordination.

**Needs your ruling:** [OQ-MCR1](#OQ-MCR1), [OQ-MCR2](#OQ-MCR2).

**Reads with:** [the companion sketch](macos-user-claude-refresh-plan.md) (not build-authorized), [the existing view design](claude-login-without-interception.md) (precedent and unfinished measurements), [native home tiers](../reference/macos-user-home-tiers.md) (workspace boundary).

---

## The native boundary

The exact rejection is **the native hostname-intercepting TLS terminator, not the whole broker**. [The native daemon ruling](../reference/macos-user-nix-and-features.md#the-oauth-terminator-stays-declined) says not to start or defer the terminator: the sandbox account cannot bind its fixed port 443, and a native backend cannot emit the container's `--add-host` redirection, so even a running listener would receive no Claude traffic. Root-global DNS/hosts changes would widen the effect beyond the jail. The [backend matrix](../reference/agent-credentials.md#where-each-agents-credentials-live) records the host broker starting and an endpoint ACL-granted on native Mac, but its jail terminator declined; the earlier statement that the *whole broker* is off was explicitly retracted in [the native reference](../reference/macos-user-nix-and-features.md#the-oauth-terminator-stays-declined). The endpoint is not a vendor-supported Claude refresh URL.

The selected [Claude pack](../../packs/claude/pack.json) declares a workspace `.claude`, a machine `.claude-shared-credentials` directory and its `shared_credentials` hook. Its [broker manifest](../../packs/claude/loopholes/claude-oauth-broker/manifest.jsonc) is enabled by default, declares `claude-oauth-refresh`, and *still* declares `intercepts`, CA state and a jail `oauth-terminator`. On native Mac that declaration does not cause interception. For the default path [the launch points secure storage at the shared directory](../../internal/cli/run/claudesecurestorage.go); that co-locates Claude's file and vendor refresh lock, but does not show that native Claude uses the file rather than Keychain, or that its lock coordinates every client. [The view selection](../../internal/cli/run/claudecredentialview.go) is opt-in on every backend (`YOLO_CLAUDE_CREDENTIAL_VIEW=1`) and mutually excludes that shared-store pointer. The [view design's native row](claude-login-without-interception.md#52-what-each-setup-gets) labels native coordination inferred, not measured. Do not claim the default host daemon serializes the native client's direct calls.

[The existing view design](claude-login-without-interception.md#11-decision-ledger) already records owner rulings [OQ-CL1](claude-login-without-interception.md#11-decision-ledger) (replace interception everywhere *after* view measurements and a rootless-host day, rather than retain two permanent paths) and [OQ-CL2](claude-login-without-interception.md#11-decision-ledger) (a jail login enrolls the machine; jail logout signs out that workspace; host logout signs out the machine). They are precedents, **not** approval to turn the switch on here. [The new per-jail daemon ruling](host-daemon-ownership.md#11-decision-ledger) [HD-D29](host-daemon-ownership.md#HD-D29) already starts a broker for a native launch and performs a locked *at-launch* catch-up when the login is near expiry; it adds no idle timer. A second broker process is safe for the shared refresh *only when* each reads the same canonical state and `refresh.lock`. It does not stop a Keychain-reading Claude from independently spending a token. The recent change cannot be represented as “no native auth service.”

[Native home tiers](../reference/macos-user-home-tiers.md) give each workspace an overlay-linked `.claude`; the account's machine tier and shared directory are reachable from concurrent sessions. This layout cannot itself provide per-workspace secrecy for a machine-tier credential. The [keychain design](keychain-from-a-jail.md#8-decision-ledger) [OQ-KC4](keychain-from-a-jail.md#8-decision-ledger) says the real host keychain remains the host's; its still-open [OQ-KC5](keychain-from-a-jail.md#OQ-KC5) chooses a native-client route. This lane must not provision an unlocked sandbox keychain, import a host login, or decide the workspace/profile lane's home mapping.

## Candidate route and limits

**Candidate, conditional — existing broker view.** The broker alone holds and refreshes the canonical machine grant under its host lock; each workspace gets only the current access token, actual expiry and supported Claude fields, with no refresh token ([view contract](claude-login-without-interception.md#51-one-writer-one-view-per-workspace)). Its existing confined view writer preserves unrelated MCP keys, uses Claude's storage write lock and refuses links. Existing enrollment, workspace logout and host logout semantics remain the [prior ruling](claude-login-without-interception.md#11-decision-ledger), not a newly invented API. Native launches register the view and hand Claude a workspace store *only if measurements show native Claude reads that store consistently*; the broker's host-only canonical state and lock must not be exposed via a workspace link, keychain copy or broad home sharing. Startup, refresh, view rewrite, teardown and at-launch catch-up reuse the existing per-launch broker lifetime: no daemon/timer survives a launch solely for this feature. Concurrent refreshes reread canonical state under the common lock before upstream redemption; a failed upstream call may not cause a blind retry of a possibly consumed token. A dying daemon must finish an in-flight redeem and persist its replacement before exit, as the existing `detach` declaration requires. Missing or lapsed login is surfaced with a human next step, not silently minted by a sandbox process.

**Gate:** on native Mac, Claude's Keychain-first behavior and secure-storage override are unmeasured ([existing caveat](claude-login-without-interception.md#11-decision-ledger), [CL-D11](claude-login-without-interception.md#CL-D11)). A file-only fixture can prove yolo's writer and lock, but not which store a signed-in real CLI reads. If Claude uses the keychain regardless of the documented store pointer, the view is **not** a working route. Seek a vendor-supported per-process credential-source selection or refresh hook, or explicitly revisit the product requirement with the owner; do not invent a schema prerequisite, patch Claude/Pi, set a global HTTPS proxy, relax TLS verification, inject a CA/host mapping, or put the whole refresh grant in a host-wide home. `CLAUDE_CODE_OAUTH_TOKEN`/`setup-token` narrows account features and puts a long-lived bearer in each sandbox; the [existing alternatives](claude-login-without-interception.md#4-the-options-that-remove-the-hosts-entry) already rejected it as equivalent to a subscription view. A bespoke inference bridge likewise owns model plaintext and is not this lane's route.

A native loopback doorway is possible **only for a client with a supported address override**: the [host doorway rule](host-notch-services.md#HS-D15) puts an authenticated launch-owned adapter outside Seatbelt on the same Mac loopback and publishes its endpoint with a per-file ACL. It works for Codex, not automatically for Claude. No supported Claude refresh endpoint override is established in the [vendor-seam survey](claude-login-without-interception.md#31-the-endpoint-cannot-be-moved). If such a vendor hook becomes available, its endpoint must be launch-owned, authenticated, TLS-pinned and disclosed; no unsigned proxy or opaque plain-HTTP endpoint gains authority by being on loopback. Do not add a doorway that nothing dials.

## Failure and observation contract (pending native ruling)

- No login: launch may continue and name the ordinary human `/login` path; never issue a refresh with absent credentials. A view that could not register says so and must not be advertised as coordinated.
- Two sessions: one canonical refresh under the existing lock, with the second reading the replacement; no two upstream redemptions of the same input. A workspace logout does not silently erase another workspace's registration. Host logout removes the canonical login and views without promising upstream revocation.
- Broker down, host asleep or view stale: do not hand Claude a refresh token as a fallback or claim a stale view is healthy. Report the service failure and the host `yolo claude-auth status`/owner login path. A returning broker catches up and updates registered views; measurement must verify what Claude does on a 401 and on restart.
- Trust and disclosure: the client sends normal account and inference traffic directly to Anthropic; yolo never terminates that TLS in this route. View access tokens remain readable within *their* jail, not within unrelated workspace overlays; machine-tier state is still writable/readable to the same sandbox account today and needs the workspace lane's confinement decision before claiming stronger isolation.

## Native measurements before any build decision

The Mac agent may inspect fixture layouts and `claude --version` without login, secrets or API calls in this design lane. **Later**, the owner must authorize and perform actual-credential trials on a disposable native workspace: record whether Claude's sandbox login goes to file, Keychain or both; whether `CLAUDE_SECURESTORAGE_CONFIG_DIR` selects the file for startup, ordinary account calls, refresh and logout; whether an externally rewritten no-refresh-token view is adopted at startup, during an idle session, after forced 401 and after process restart; whether the old and new access tokens overlap or the former is revoked; whether two concurrent sessions in *different* workspaces share a writer and preserve individual `/logout`; and whether a sleeping host's catch-up and an interrupted broker finish the one redemption. Capture version, elapsed times, status classes and redacted fingerprints only. Never record tokens, full credentials, request bodies or upstream responses. A localhost fake and synthetic credentials can test layout, locks and failure messages without sending a token; those tests cannot certify vendor behavior.

## Open questions

1. 💬 **OQ-MCR1: Which supported native Claude credential input actually bypasses Keychain refresh across two workspaces?**

   The view is already implemented but opt-in; native file-versus-Keychain behavior is not measured. If the file override works, keep the existing view route. If it does not, require a demonstrated supported client seam or ask the owner to relax the shared-login goal; do not interpose on a native hostname.

   <!-- vantage: question id=OQ-MCR1 leaning="Prefer the existing refresh-token-free view if native Claude demonstrably reads it for account calls and refresh, otherwise stop and seek a supported client hook." -->

   _Leaning:_ Prefer the existing refresh-token-free view if native Claude demonstrably reads it for account calls and refresh, otherwise stop and seek a supported client hook.

   **Answer:**
   > _(owner decision after Mac measurements)_

2. 💬 **OQ-MCR2: What credential-sharing boundary may native sessions claim while the one account's machine tier remains reachable?**

   A shared grant needs a host-only canonical store and per-workspace views. The existing sandbox account can reach its machine tier; coordination with [native home tiers](../reference/macos-user-home-tiers.md) and the [keychain route](keychain-from-a-jail.md#OQ-KC5) must decide whether stronger per-workspace isolation is a prerequisite for exposing a native view as secure. This does not authorize re-pointing the shared home.

   <!-- vantage: question id=OQ-MCR2 leaning="Retain host-only canonical state and per-workspace access-token views; make no stronger isolation claim until the workspace/profile lane measures its boundary." -->

   _Leaning:_ Retain host-only canonical state and per-workspace access-token views; make no stronger isolation claim until the workspace/profile lane measures its boundary.

   **Answer:**
   > _(owner decision after boundary review)_
