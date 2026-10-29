---
title: "Synthetic native macOS comparison runbook"
status: accepted
stage: DECIDED
next: "Materialize the described temporary probes, compile and confirm selected-SDK APIs and fixture behavior, then run controls only in a wholly disposable synthetic-only UID/session"
tags: [runbook, macos, security, tls]
summary: "Source-pinned instructions for the unrun Safehouse, restrictive-ACL and TLS controls; the yolo baseline is prior evidence, not a result of this runbook."
---

# Synthetic native macOS comparison

**Preparation only, written 2026-10-29 against `e66398b09c0571a98927b6b9eb5256a480ae4dac`; no Mac execution.** The [D159 SecurityServer audit](../lab-reports/2026-10-29-mac-securityserver-audit.md) is existing evidence against yolo `d159a57f0`, not a rerun and not proof for another build. The [native TLS design](../design/macos-native-tls.md#mac-side-follow-up--audit-the-securityserver-boundary) and [Copilot networking research](../research/copilot-native-networking.md) own the behavior and source provenance.

## Scope and stop conditions

The [design's settled scope](../design/macos-native-tls.md#mac-side-follow-up--audit-the-securityserver-boundary) owns behavior. This document prepares only the remaining Safehouse comparison, restrictive synthetic ACL, and native SSL-policy/hostname controls. Do not reopen the yolo baseline or claim current Copilot works.

- Use a wholly disposable, synthetic-only macOS UID/session for **every** Security.framework call in this runbook, including fixture creation, setup, preflight, comparison rows, and cleanup. That session must not use or contain the owner's login keychains or credentials. A disposable workspace or separate `HOME` under the owner's UID/session is not sufficient.
- The retained [`SecKeychainCreate` header](https://github.com/apple-oss-distributions/Security/blob/main/OSX/libsecurity_keychain/lib/SecKeychain.h) documents creation at a supplied file path but says nothing about whether creation changes the current user's default keychain or search list; no implementation source is retained here to settle that side effect. Treat it as **unknown**. Do not call `SecKeychainCreate` in an owner UID/session, even transiently. If a wholly disposable synthetic-only UID/session is unavailable, stop before setup; never snapshot/restore owner state as a workaround.
- Keep the fixture database at an explicit path under the disposable workspace and open that path directly. Never pass a null keychain selector that falls back to a user's default/search list. Explicit path selection does not remove the isolation requirement above.
- The tested file path should be readable in the comparison workspace. This measures mediated item operations with a readable file; it does not test Safehouse's host-home keychain-file grants.
- Do not read/print credentials, change default/search lists, alter owner keychains, inspect owner default/search-list metadata, use `lock-keychain -a`, run agents or login/API tests, or deploy a profile. Set Security.framework user interaction off and report prompt/interaction errors separately from ACL and sandbox errors.
- Preserve the production `com.apple.SecurityServer`, `com.apple.securityd.xpc`, `com.apple.secd` and other yolo denies. Do not add a service allow to yolo.
- Stop and mark the relevant row untested if the fixture cannot be created, ACL behavior cannot be distinguished from interaction, or the effective rendered policy does not match the described case. Do not repair a failing production deny in this comparison.
- No pathless operation is authorized here. It remains untested unless a disposable UID with its own sole synthetic default keychain is already available without changing owner state.
- Copilot actual native HTTP remains untested until public/vendor evidence identifies a credential-free supported request path. Do not invent an endpoint, selector, constructor or request; no Copilot login, prompt or API call is part of this runbook.

The host that prepared this document was Linux without `sandbox-exec`. That is an executor gap, not a request for another policy ruling. The existing yolo measurements remain the baseline; no RED callsite or production-code change is applicable to this docs-only preparation.

## Pinned inputs and what they prove

| Input | Use | Boundary |
| :--- | :--- | :--- |
| [Safehouse at `f1c6a6a355b1d82cc3f4b267c53f1f9ce456f9af`](https://github.com/eugene1g/agent-safehouse/tree/f1c6a6a355b1d82cc3f4b267c53f1f9ce456f9af) | Parse `policy --stdout`, `--workdir`, `--append-profile`; render and inspect selected Copilot policy | Pin and hash the executable/source. The Copilot profile requires the keychain module; do not infer inactivity from an optional-module label. |
| [Safehouse Copilot profile](https://github.com/eugene1g/agent-safehouse/blob/f1c6a6a355b1d82cc3f4b267c53f1f9ce456f9af/profiles/60-agents/copilot-cli.sb) and [keychain profile](https://github.com/eugene1g/agent-safehouse/blob/f1c6a6a355b1d82cc3f4b267c53f1f9ce456f9af/profiles/55-integrations-optional/keychain.sb) | Verify the selected agent's requirement and exact file/service grants | Profile selection is policy evidence only, not item access or actual Copilot behavior. |
| [Safehouse policy architecture](https://github.com/eugene1g/agent-safehouse/blob/f1c6a6a355b1d82cc3f4b267c53f1f9ce456f9af/docs/docs/policy-architecture.md), [launch implementation](https://github.com/eugene1g/agent-safehouse/blob/f1c6a6a355b1d82cc3f4b267c53f1f9ce456f9af/bin/lib/runtime/launch.sh), and [debugging guide](https://github.com/eugene1g/agent-safehouse/blob/f1c6a6a355b1d82cc3f4b267c53f1f9ce456f9af/docs/docs/debugging.md) | Confirm appended rules render after selected profiles and child processes inherit the outer sandbox | A policy probe is not the Copilot CLI's optional inner shell sandbox. |
| [`yolo internal macos-user-profile`](../../internal/cli/macosuserprofile.go) | Render a yolo P0 harness profile for an explicit workspace and temp directory | This helper omits production launch inputs; label it harness P0, never full launch equivalence. |
| [`DenyDefaultProfile`](../../internal/macosuser/seatbeltdeny.go) and the [D159 audit](../lab-reports/2026-10-29-mac-securityserver-audit.md) | Check the unchanged deny and identify prior measured baseline | Audit ran against `d159a57f0`; do not silently attribute its results to the current tree. |
| [Apple Security framework headers](https://github.com/apple-oss-distributions/Security) | Confirm legacy fixture APIs and access-list arguments before compiling | Read-only header snapshot was unversioned `main`; record the actual macOS SDK/Xcode version and recheck its installed headers before execution. |

## Prepare an isolated fixture

Use a fresh temporary workspace inside the wholly disposable synthetic-only UID/session, with restrictive directory permissions and no symlinked fixture paths. Record that session's UID, physical workspace path and real process `HOME`; use the same caller binary, child process, UID, `HOME`, working directory and exact fixture across each newly run comparison row. **Do not use the owner's UID/session, login keychains, or credentials for any Security.framework operation.** A separate `HOME` directory does not create a credential boundary or isolate Security.framework state.

The retained [`SecKeychainCreate` header](https://github.com/apple-oss-distributions/Security/blob/main/OSX/libsecurity_keychain/lib/SecKeychain.h) describes creating a keychain at the supplied file path but does not specify whether the call also changes the current user's default keychain or search list. No Apple implementation source is present in the retained source snapshot, so that effect remains unknown. The isolation requirement is therefore mandatory even though the fixture uses an explicit path. If a synthetic-only UID/session cannot be used, stop before `setup`; do not call the API as a test, inspect owner search-list state, or attempt restoration afterward.

1. Build the disposable keychain probe as described under [Build the disposable probes](#build-the-disposable-probes), then run its `setup` mode once outside all sandboxes **within the synthetic-only UID/session**. Create one new, non-login file keychain at `$W/fixture.keychain-db` using the installed Security.framework API with a synthetic password and no user interaction. Do not add it to any search list or make it default. If creation/unlock would touch or prompt for owner state, stop.
2. In that same one-time setup, create two preexisting generic-password items in the explicit keychain. Fix the class to generic password and use constant test-only service/account labels, for example `yolo-native-comparison` / `synthetic-permissive` and `yolo-native-comparison` / `synthetic-restricted`. Give each item a distinct fixed, test-only byte string embedded in the private probe source; the program compares bytes internally and prints only `match=true|false`.
3. Build access objects explicitly with `SecAccessCreate(descriptor, trustedApplications, &access)`. The permissive item's list contains a `SecTrustedApplicationRef` made from the exact probe executable path; the restrictive item's list is an empty `CFArray`. Apple's header says `NULL` instead means default trust for the creating application. Never use `NULL` accidentally for the restrictive fixture. Confirm the resulting ACLs using the API and report creation/readback errors.
4. Call `SecKeychainSetUserInteractionAllowed(false)` before fixture operations. Create the file keychain at the absolute workspace path with `SecKeychainCreate(path, syntheticPasswordLength, syntheticPassword, false, NULL, &keychain)`, then explicitly reopen it with `SecKeychainOpen(path, &keychain)` in each probe process. If it is locked, use `SecKeychainUnlock(keychain, syntheticPasswordLength, syntheticPassword, true)` with that non-null fixture reference. Do not pass a null keychain reference to unlock, find, add or read. Use `SecKeychainFindGenericPassword(keychain, ..., fixedService, ..., fixedAccount, ...)` for the legacy path-based item checks. The keychain argument must never be a search-list array. Pass returned password bytes only to an in-process equality check, then free them; emit no value, length, hash or buffer.
5. Create each preexisting item with `SecKeychainItemCreateFromContent(kSecGenericPasswordItemClass, attributesForFixedServiceAndAccount, syntheticValueLength, syntheticValue, keychain, access, &item)`: the attributes list contains only the fixed test labels, `keychain` is the explicit fixture reference, and `access` is the ACL from step 3. The checked headers confirm this call accepts both selectors. If using modern APIs instead, use `SecItemAdd` with `kSecUseKeychain` naming the fixture and `SecItemCopyMatching` with `kSecMatchSearchList` containing only that fixture; first verify both selectors against the installed SDK. Do not treat a modern `SecItem*`/`secd` result as interchangeable with the CDSA `SecKeychain*` path measured in D159.
6. Still outside all sandboxes, use this same executable to establish the permissive item's no-prompt read positive control and the restrictive item's expected ACL denial. Before interpreting every policy row, run an unsandboxed preflight that explicitly opens the fixture, confirms it is unlocked, and reads/matches the preexisting permissive item. If preflight fails, stop and classify the policy result as unknown; do not attribute auto-lock or fixture drift to a sandbox. Run each comparison row against the same preexisting items; never rerun `setup` inside a policy. If an explicit unlock is needed, unlock only the fixture reference with its synthetic password, then repeat preflight.
7. For each row's auth-like synthetic add/readback check, use a unique fixed account label reserved for that row (for example `synthetic-add-safehouse-copilot`), add only that item to the fixture, read it back in-process, and then run `cleanup-item` outside the sandbox to find/delete only that exact label. If add partly succeeds but readback fails, perform the same narrow cleanup. Never reuse an account label across rows or repeat a prior row without cleanup.

The headers checked while preparing this document are public Apple headers downloaded from unversioned `main`; recheck all signatures against the executor's SDK. In particular, no outcome is inferred from an ACL constructor alone.

## Render and compare Safehouse policies

Use the pinned Safehouse executable built/checked from the revision above. Run its policy-render path only; it writes the policy to stdout and does not execute the caller. The pinned parser accepts `--stdout`, `--workdir` and `--append-profile`. The Copilot profile declares the keychain integration as a requirement, so do not rely on an invented module-disable switch or add an explicit `--enable` to make the selected policy appear active.

```bash
safehouse policy --stdout --workdir "$W" copilot >"$W/safehouse-copilot.sb"
```

Read the complete generated profile. Confirm the Copilot profile and keychain module appear, identify grants for the home Keychains path and named Mach services, and confirm the actual workdir and appended-profile ordering. If the source-pinned executable rejects this documented invocation or does not select the Copilot profile, stop rather than trying guessed flags.

Prepare a separate overlay file at `$W/no-keychain.sb` containing these final denies for the paths and services granted by the pinned keychain module:

```scheme
(deny file-read* file-write* (home-subpath "/Library/Keychains"))
(deny file-read* file-write* (home-literal "/Library/Preferences/com.apple.security.plist"))
(deny file-read* (literal "/Library/Preferences/com.apple.security.plist"))
(deny file-read* (literal "/Library/Keychains/System.keychain"))
(deny file-read* (subpath "/private/var/db/mds"))
(deny mach-lookup
  (global-name "com.apple.SecurityServer")
  (global-name "com.apple.security.agent")
  (global-name "com.apple.securityd.xpc")
  (global-name "com.apple.security.authhost")
  (global-name "com.apple.secd")
  (global-name "com.apple.trustd"))
(deny ipc-posix-shm-read-data ipc-posix-shm-write-create ipc-posix-shm-write-data
  (ipc-posix-name "com.apple.AppleDatabaseChanged"))
```

Keep the normal workspace read/write grant so the synthetic file remains readable. Then render the comparison variant through the supported append mechanism:

```bash
safehouse policy --stdout --workdir "$W" \
  --append-profile "$W/no-keychain.sb" copilot >"$W/safehouse-no-keychain.sb"
```

Confirm the appended overlay occurs after the selected profiles and all earlier appended policies. The pinned renderer then emits append-file protections and terminal denies afterward; verify those later rules do not include an allow that reopens any capability the overlay is intended to deny, and confirm each intended deny remains effective. The deny overlay deliberately narrows the effective policy; it is not a claim that Safehouse supports disabling the required module. Because denying `trustd` may affect certificate validation, record keychain and TLS results separately; do not attribute a TLS failure in this variant to the SecurityServer path alone.

Run the probe in `spawn-child` mode under each saved policy, with the same UID, real `HOME`, working directory and absolute fixture path. This process is the parent: it must `fork`, then have only the child `execve` the identical hashed probe binary by its absolute path with the child mode; the parent must `waitpid` for that child. Both processes record PID, PPID, UID, real `HOME`, executable path and SHA-256. Require the parent and child to match each other and the row's expected UID, `HOME`, executable path and binary hash. A failed `fork`, child `execve` or parent `waitpid` is an untested/failed control, never a pass. Do not invoke a shell. The child runs the fixture-only operation; child sandbox inheritance is part of the observation. The harness makes no Copilot process and no agent request. The fixed item labels are selected from a compiled whitelist; never accept a caller-supplied service/account string:

```bash
# Outside every sandbox, immediately before each policy row:
"$W/keychain-probe" --mode preflight --keychain "$W/fixture.keychain-db" \
  --item synthetic-permissive

/usr/bin/sandbox-exec -f "$W/safehouse-copilot.sb" -- \
  "$W/keychain-probe" --mode spawn-child --child-mode read \
  --keychain "$W/fixture.keychain-db" --item synthetic-permissive
/usr/bin/sandbox-exec -f "$W/safehouse-no-keychain.sb" -- \
  "$W/keychain-probe" --mode spawn-child --child-mode read \
  --keychain "$W/fixture.keychain-db" --item synthetic-permissive
/usr/bin/sandbox-exec -f "$W/safehouse-copilot.sb" -- \
  "$W/keychain-probe" --mode spawn-child --child-mode add-readback \
  --keychain "$W/fixture.keychain-db" --item synthetic-add-safehouse-copilot
# After every add-readback attempt, outside the sandbox:
"$W/keychain-probe" --mode cleanup-item --keychain "$W/fixture.keychain-db" \
  --item synthetic-add-safehouse-copilot
```

Repeat the same preflight/parent/child sequence for the restrictive preexisting item and each yolo P0 policy, substituting only fixed policy-specific add labels. The parent and child must report the same probe-binary hash; reject a row if it differs. Record the Safehouse child-probe hash and environment. These rows measure the outer selected Copilot policy applied to a harmless test process, not the policy around Copilot's optional shell-tool subprocess and not actual CLI authentication or TLS.

## Render yolo harness P0; preserve the measured baseline separately

Build/use the yolo binary matching the source revision under test; do not deploy it. Render the helper profile with the fixture workspace, a private temp directory, the actual host home as `--host-home`, and a separate agent-home path beneath the workspace. Keep the process environment `HOME` equal to the real host `HOME`; never pass the real home as `--home`, which would grant the profile's agent-home rules against owner state.

```bash
yolo internal macos-user-profile --mode self \
  --workspace "$W" --home "$W/.yolo/home" --host-home "$HOME" \
  --tmp "$W/tmp" >"$W/yolo-p0-harness.sb"
```

Inspect the result and verify the four mandatory service denies are present: `com.apple.SecurityServer`, `com.apple.securityd`, `com.apple.securityd.xpc` and `com.apple.secd`. Run the same probe, with the same real `HOME` and fixture, under this profile. Label the row **yolo P0 harness profile**, not production launch: `macosUserProfileMain` explicitly leaves machine dirs, context mounts, cache relocations, readonly-workspace state, staged home content and devices empty. It cannot establish complete launch equivalence.

The prior measured yolo results remain exactly those in the [D159 report](../lab-reports/2026-10-29-mac-securityserver-audit.md): shipped launch and generated p0 blocked the tested operations on the then-readable fixture; p2 reallowed only SecurityServer and exposed the trusted permissive specimen. Do not repeat p2, do not construct a SecurityServer allow, and do not update the old report with this unexecuted runbook.

## Build the disposable probes

No native probe source or helper binary is checked in. Materializing the temporary C/Swift sources, confirming their APIs compile against the selected Mac SDK, establishing fixture behavior, and running any native policy row are **unverified prerequisites**, not successful controls. The algorithm below is the source contract for that disposable harness; stop if the API does not compile or differs on the target SDK rather than substituting unreviewed code. On the Mac, write the described probes inside `$W`, then compile them against the active SDK; these commands build those local source files and do not imply they already exist:

```bash
xcrun --sdk macosx clang -std=c11 -Wall -Wextra -Werror -Wno-deprecated-declarations \
  -framework Security -framework CoreFoundation \
  "$W/keychain-probe.c" -o "$W/keychain-probe"
xcrun --sdk macosx swiftc \
  -framework Security -framework Foundation \
  "$W/tls-probe.swift" -o "$W/tls-probe"
```

Record `xcode-select -p`, `xcrun --show-sdk-version`, compiler version and SHA-256 of both source and binaries. Compile once and reuse the identical hashes in every sandbox row. Do not add the temporary source, fixture or binary to this repository.

The temporary keychain probe must implement these modes and selectors; materialization, SDK compilation and native execution remain unverified prerequisites:

1. Parse one required absolute `--keychain` path; reject every other path and never fall back to a default.
2. `setup` runs once outside all sandbox policies. Disable Security.framework interaction, create the keychain only at that path with `SecKeychainCreate`, create the two preexisting ACL-controlled items with `SecKeychainItemCreateFromContent`, close references, then verify them with the unsandboxed positive controls. Store only fixed synthetic test bytes in the temporary private probe source; do not log them.
3. `preflight` runs outside policy rows: open the fixture by path, confirm it is unlocked, and require a no-prompt matching read of the preexisting permissive item. `spawn-child` runs as a parent that records its PID/PPID and identity, forks, and has only the child `execve` the identical hashed probe binary by absolute path with the selected child mode; the parent `waitpid`s and records its result. Parent and child each record PID, PPID, UID, real `HOME`, executable path and SHA-256; require both to match each other and the row's expected UID, `HOME`, executable path and hash. A failed `fork`, child `execve` or parent `waitpid` is an untested/failed control, never a pass. Do not fork a shell or call `sandbox-exec` from inside the probe. The child opens the same fixture reference and executes only the selected fixture mode.
4. Build two `SecAccessRef` values with `SecAccessCreate`: one from a one-element `CFArray` of `SecTrustedApplicationRef` for the exact executable, and one from an empty `CFArray`. Create `kSecGenericPasswordItemClass` records with `SecKeychainItemCreateFromContent`, a `SecKeychainAttributeList` containing only the fixed `kSecServiceItemAttr` and `kSecAccountItemAttr` labels, the fixed synthetic bytes, that explicit `SecKeychainRef`, and the corresponding access object.
5. For `read` and `add-readback`, call `SecKeychainFindGenericPassword` with the explicit keychain reference and exact service/account byte strings and lengths. The `add-readback` mode uses a unique compiled-in per-row account, adds that item with the same explicit keychain reference and fixture ACL, then reads it back. Report `operation`, OSStatus name/code, `match=true|false`, and failure class. Compare returned bytes in memory to fixed synthetic values; print no secret-derived bytes, length or digest. Release item/keychain/access references and API buffers with their matching release/free functions.
6. `cleanup-item` runs outside the sandbox and may find/delete only the exact unique per-row synthetic item label. `setup` never runs within a comparison policy. No mode queries all items or enumerates a user's keychain.

The Swift TLS probe must use no authentication, cookies or API request. For a certificate control, capture the server-presented PEM certificates with `openssl s_client -showcerts -connect "$host:443" -servername "$host" </dev/null`, split each PEM certificate, and convert each to DER using `openssl x509 -in chain-N.pem -outform DER -out chain-N.der`. This fetch is only certificate material; do not treat OpenSSL's connection outcome as a validation result. In the probe, load each DER file with `SecCertificateCreateWithData`, place the server leaf first followed by its presented intermediates in a `CFArray`, create `SecPolicyCreateSSL(true, expectedHostname)` using the exact DNS host, then call `SecTrustCreateWithCertificates(chain, policy, &trust)` and `SecTrustEvaluateWithError`. Record the complete-chain result and `CFError` domain/code without weakening verification. Repeat with the self-signed endpoint's DER chain and its matching DNS hostname (chain must fail), then with the `wrong.host.badssl.com` DER chain and expected hostname `wrong.host.badssl.com` (hostname policy must fail). Keep the certificate source host and expected policy hostname separately recorded.

Separately use `URLSessionConfiguration.ephemeral` and, after SDK-checking the public properties, set `urlCredentialStorage = nil`, `httpCookieStorage = nil` and `httpShouldSetCookies = false`. The [URLSessionConfiguration API](https://developer.apple.com/documentation/foundation/urlsessionconfiguration) supplies the ephemeral mode and credential/cookie properties; confirm the named properties exist in the selected SDK before compiling. The `URLSessionTaskDelegate` redirect callback must return `nil` for every redirect. For its authentication-challenge callback, use `.performDefaultHandling` only when the protection-space method is server trust, with the original challenge unchanged; cancel every other challenge. This keeps URLSession's default server-trust checks and prevents redirect/authentication paths from supplying owner credentials. Issue credential-free GETs to `https://example.com/`, `https://self-signed.badssl.com/` and `https://wrong.host.badssl.com/`: the valid HTTPS request should succeed; the latter two must fail certificate verification. If an endpoint is unreachable or interception changes its chain, record unavailable/unknown, not a pass. The URLSession source and all outcomes remain unverified until a native Mac compiles and runs the temporary source.

Run the native TLS controls under unsandboxed and unchanged yolo P0 harness profiles. Safehouse rows may be useful comparison data but are not Copilot TLS evidence. Do not use SecureTransport success under an added SecurityServer allow as a control: that weakens the boundary and is out of scope.

## Record outcomes and report only what ran

Store a redacted private run log with this schema; never commit fixture values or raw secrets:

| Field | Record |
| :--- | :--- |
| Run identity | Date, macOS version/build, architecture, SDK/Xcode, current source revision and exact binary hashes |
| Process identity | UID, real `HOME`, physical workdir, caller/child executable hash and child environment; state whether a policy render or a real launch was used |
| Fixture | Disposable keychain path/hash, item class, fixed service/account labels, ACL definition/readback and whether the permissive/restrictive control behaved as expected |
| Policy | Safehouse revision, selected Copilot/keychain modules, rendered policy SHA-256, appended overlay SHA-256 and confirmed final deny list; yolo profile helper binary/revision and harness-only label |
| Operation result | Row, operation, exit status, OSStatus/domain/code, `match` boolean only, sandbox deny/prompt/ACL/missing-item/timeout/unknown category |
| TLS result | URL hostname, explicit SSL policy hostname, presented-chain fingerprint only, trust result/error code, URLSession default-verification result; no headers, cookies, auth or response body |
| Cleanup | Fixture and temp source/binary removed; no Security.framework calls used the owner UID/session, and no owner default/search-list or keychain state was read, changed or restored |

After all rows, close every Security.framework reference, then remove only the temporary source, binaries, private fixture manifest (if any), and the known disposable keychain file/sidecars under `$W`. Do not call a broad keychain delete or search-list operation. Keep categories distinct: a sandbox deny is not an ACL denial; a prompt-disabled interaction failure is not proof of an ACL decision; a missing item is not a denied secret read; a timeout or unreachable endpoint is unknown. Report a result only for the exact tested selector, ACL, policy hash and process identity. Do not generalize to every keychain item, every Safehouse configuration, Copilot's live inner sandbox, pathless session access, `secd` data-protection items or client-certificate/private-key operations.

No Mac execution or fixture operation occurred while preparing this runbook. The temporary probe sources do not yet exist: first materialize them, confirm their APIs compile against the selected Mac SDK and verify fixture behavior **inside the wholly disposable synthetic-only UID/session**, and only then run eligible controls. Do not perform any of those steps from the owner's UID/session; if the required isolation is unavailable, leave the rows untested.

## Still unexecuted

- Safehouse permissive and restrictive ACL rows under the effective Copilot policy and final-deny overlay.
- Current-source yolo P0 harness row; the existing real-launch baseline is from `d159a57f0` and is not replaced by it.
- Native SSL-policy hostname-match/mismatch controls and URLSession default-verification negative controls under unchanged yolo P0.
- Any pathless session or data-protection keychain operation; no owner/default/search-list operation is authorized.
- Copilot native HTTP path, its selected TLS verifier and any current CLI success result; public release assets linking multiple TLS stacks do not identify the active caller. Require public/vendor callsite mapping and an independently authorized, credential-free native request before claiming that result.

**Gates for this document:** repository-pinned Vantage style-guide read; `scripts/vantage-check.sh docs/plans/macos-native-comparison-runbook.md`; `git diff --check`; manual API/source and safety review. No Go test, nested jail, Mac test or full integration gate applies to this docs-only edit.
