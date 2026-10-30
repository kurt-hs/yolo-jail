---
title: "Mac native witnesses: the comparison runbook, run in a disposable account"
date: 2026-10-29
tags: [lab-report, macos, macos-user, security, tls, apple-container, claude, copilot]
summary: "Every runnable open question in the native TLS design, run on the maintainer's Mac against 6f651f8c4. Two new defects: a stale inherited-scope integration test and a false unknown-setting warning."
---

# Mac native witnesses: the comparison runbook

**Run 2026-10-29 on the maintainer's Mac** (macOS 26.5 build 25F71, arm64, Command Line Tools
SDK 26.5, Apple clang 21.0.0, Swift 6.3.2, Apple Container 1.1.0). It follows
[the comparison runbook](../plans/macos-native-comparison-runbook.md#native-witnesses-one-per-open-question)
and answers the open questions of [the native TLS design](../design/macos-native-tls.md#7-open-questions).
At the maintainer's request the launcher was deployed from `main` at `6f651f8c4`
(`yolo-jail 0.15.1+773.g6f651f8c4da3`, SHA-256 `af2b703a…e0e7e4`).

**Terms.** A *witness* is native output that answers one open question. *P0* is the profile
`yolo internal macos-user-profile --mode self` renders, which the runbook calls the
*yolo P0 harness profile*: production's deny rules without production's launch inputs.
*Self mode* is macos-user's run-as-you mode
([MMP-D12](../design/macos-user-model-plan.md#MMP-D12)). *Account mode* runs the agent as the
hidden `_yolojail` user.

## 1. Results

| Question | Where it ran | Result |
| :--- | :--- | :--- |
| [OQ-MT3](../design/macos-native-tls.md#OQ-MT3) Safehouse effective policy | disposable account | As expected ([§3.2](#32-safehouse)) |
| [OQ-MT4](../design/macos-native-tls.md#OQ-MT4) Restrictive keychain ACL | disposable account | No ACL bypass ([§3.3](#33-restrictive-acl)) |
| [OQ-MT5](../design/macos-native-tls.md#OQ-MT5) Current P0 and TLS controls | disposable account | Boundary holds; every TLS control as expected ([§3.4](#34-current-p0-and-tls-controls)) |
| [OQ-MT6](../design/macos-native-tls.md#OQ-MT6) MDM-installed CA | — | **Untested**: this network does not intercept TLS |
| [OQ-MT7](../design/macos-native-tls.md#OQ-MT7) Inherited account relaunch | maintainer's account | Relaunch repair holds; **the integration test is stale** ([§4.1](#41-inherited-account-relaunch)) |
| [OQ-MT8](../design/macos-native-tls.md#OQ-MT8) Capture compatibility context | maintainer's account | Context repair holds ([§4.2](#42-capture-context-and-copilot)) |
| [OQ-MT9](../design/macos-native-tls.md#OQ-MT9) Claude view On then Off | maintainer's account | Retirement holds; **a false warning** ([§4.3](#43-claude-view-on-then-off)) |
| [OQ-MT10](../design/macos-native-tls.md#OQ-MT10) Apple Container JSON and prune | maintainer's account | Listing and deletion work ([§4.4](#44-apple-container-prune)) |
| [OQ-MT11](../design/macos-native-tls.md#OQ-MT11) Copilot TLS under the deny | maintainer's account | Not reachable: yolo refuses Copilot 1.0.93+ first ([§4.2](#42-capture-context-and-copilot)) |

## 2. The disposable account

The maintainer created `yolo-witness` (uid 504) with a script run once under sudo. It is a
standard, hidden account with a random password nobody saw. Its only way in is SSH from this Mac
with a throwaway key generated for this session. Each SSH login is a new security session, which
`sudo -u` from the owner's session is not. `_yolojail` was considered and not used: every
account-mode jail runs as it, it can read the Claude views in workspaces, and `sudo -u` keeps the
owner's session.

At first login the account had only `/Library/Keychains/System.keychain` in its search list and
no default keychain (`SecKeychainCopyDefault: A default keychain could not be found.`).

**Which questions ran where.** The keychain and Safehouse rows ([`OQ-MT3`](../design/macos-native-tls.md#OQ-MT3) to [`OQ-MT5`](../design/macos-native-tls.md#OQ-MT5)) ran in
`yolo-witness`. The rest ran in the maintainer's account:

- Account mode serves one macOS user per Mac ([the macOS guide](../../userguide/guides/macos.md)),
  and that user is the maintainer.
- The Claude view row uses the broker's existing login, which no other account holds.
- These are the same kind of launches the earlier lab reports ran there.

## 3. Disposable-account rows

### 3.1 Probes and fixture

The two probes were written to the runbook's contract, compiled in the account with the runbook's
commands, and deleted afterwards. Hashes:

| File | SHA-256 |
| :--- | :--- |
| `keychain-probe` | `1df5ef42fbd5f84cbb4c1513a355aca31c292e9ef8b4aaab6b483e88d05e2f23` |
| `keychain-probe.c` | `7a8964fac139210a0c5ad535ddfccf209dc0a77e5410cba409d32eb9a443f90f` |
| `tls-probe` | `3e6c7cb76d3550d5c3a69c0a2f3efe7012e16071c0495a897ce922e3ab258b5c` |
| `tls-probe.swift` | `8ef7b26a36d2c0a84706e5166954a4480114699b954fc4b7fd5be5763d8bc424` |

`setup` created `fixture.keychain-db` at an explicit path under the workspace. It created the two
items with `SecKeychainItemCreateFromContent`, and each ACL came from `SecAccessCreate`.

- **The permissive item's ACL reads back** as three entries. The decrypt entry trusts one
  application, the probe.
- **The restricted item's ACL could not be read back.** Finding its item reference failed with
  -25293. Its no-prompt read also fails with -25293 (`errSecAuthFailed`), which is the expected
  ACL denial.
- **The permissive item reads** with `match=true`.

**The keychain search list and default keychain were the same before and after
`SecKeychainCreate`.** This is the runbook's unknown side effect: in this account, on this build,
it changed neither.

Every preflight before a policy row found the fixture unlocked and the permissive item matching.
Every row's parent and child matched: uid 504, `HOME=/Users/yolo-witness` and the same probe hash.

### 3.2 Safehouse

Safehouse was fetched at the pinned `f1c6a6a355b1d82cc3f4b267c53f1f9ce456f9af`. It reports
`Agent Safehouse 0.12.0`, and `bin/safehouse.sh` hashes to `7711b17b…aec133`.

**The runbook's command is wrong for this revision.** `safehouse policy --stdout --workdir W copilot`
would treat `policy` as the command to run. The pinned source has no `policy` subcommand: policy
mode is `--stdout`, or no command at all ([`main.sh`](https://github.com/eugene1g/agent-safehouse/blob/f1c6a6a355b1d82cc3f4b267c53f1f9ce456f9af/bin/lib/commands/main.sh)).
Both policies were rendered with `safehouse.sh --stdout --workdir W [--append-profile W/no-keychain.sb] copilot`,
which selects the Copilot profile by command name and runs nothing.

- `safehouse-copilot.sb` (`0ec1c965…84a5`) includes `55-integrations-optional/keychain.sb` through the
  Copilot profile's `$$require$$`. It grants `home-subpath "/Library/Keychains"`, the System
  keychain, `com.apple.SecurityServer`, `com.apple.securityd.xpc` and `com.apple.secd`.
- `safehouse-no-keychain.sb` (`002089eb…b4aa`) places the runbook's overlay after every selected
  profile. Only `deny file-write*` protections follow it, so nothing reopens what it denies.

| Row | Result |
| :--- | :--- |
| `safehouse-copilot`, read permissive | status 0, `match=true`: Safehouse's documented broadened access |
| `safehouse-no-keychain`, read permissive | -25300 (`errSecItemNotFound`), `match=false`: the overlay takes effect |
| `safehouse-copilot`, add then read back | add 0, readback 0, `match=true`; cleanup deleted the item |
| `safehouse-no-keychain`, add then read back | add -25308 (`errSecInteractionNotAllowed`), readback -25300 |

### 3.3 Restrictive ACL

| Row | Result |
| :--- | :--- |
| Unsandboxed read restricted | -25293, `match=false`: the ACL denies |
| `safehouse-copilot`, read restricted | -25293, `match=false`: **no ACL bypass inside Safehouse** |
| `yolo-p0-harness`, read restricted | -25300, `match=false`: the service deny stops the call before the ACL is consulted |

### 3.4 Current P0 and TLS controls

`yolo-p0-harness.sb` (`ddf484fb…9047`, 446 lines) holds all four mandatory denies:
`com.apple.SecurityServer`, `com.apple.securityd`, `com.apple.securityd.xpc` and
`com.apple.secd`.

| Row | Result |
| :--- | :--- |
| P0, read permissive | -25300, `match=false` |
| P0, read restricted | -25300, `match=false` |
| P0, add then read back | add -25308, readback -25300; nothing to clean up |
| P0, `shasum` of the fixture file | readable: the refusal is the denied service, not the file |

These match the [d159a57f0 audit](2026-10-29-mac-securityserver-audit.md#2a-a-keychain-whose-file-the-sandbox-can-read-in-the-workspace)
on the current tree. Mediated keychain calls report a denied service as "item not found" or
"interaction not allowed", never as a sandbox error. The kernel's sandbox log showed no entry for
the probe, so the category rests on three facts: the file is readable, the unsandboxed control
succeeds, and the profile denies the service.

The chains were captured with `openssl s_client`:

| Host | Certificates | Leaf SHA-256, first 8 bytes |
| :--- | :--- | :--- |
| example.com | 4 | `85:CA:6A:B0:68:E9:BC:CE` |
| self-signed.badssl.com | 1 | `BB:A2:1B:74:3C:B5:A6:30` |
| wrong.host.badssl.com | 3 | `10:11:54:81:3E:8F:DA:B6` |

Results were identical unsandboxed and under P0:

| Host | `SecTrustEvaluateWithError` | URLSession |
| :--- | :--- | :--- |
| example.com | trusted | HTTP 200 |
| self-signed.badssl.com | not trusted, `NSOSStatusErrorDomain -67843` | `NSURLErrorDomain -1202` |
| wrong.host.badssl.com | not trusted, `NSOSStatusErrorDomain -67602` (host name mismatch) | `NSURLErrorDomain -1202` |

## 4. Maintainer-account rows

### 4.1 Inherited account relaunch

**The runbook's two launches pass.** The workspace was a throwaway
`/Users/Shared/yolo/mt7-relaunch` in account mode, under the maintainer's own user config; the
runbook's step that writes a new `config.jsonc` was not used in this account. Each launch did the
following:

- ran as `_yolojail`;
- found `~/.config/yolo-jail/config.jsonc` delivered, mode `-r--r--r--`, with its `packs`;
- had an append to it refused;
- exited 0.

The second launch printed no `refusing` or `could overwrite existing user file` line. This is the
[second-launch refusal](2026-10-29-mac-ci-cleanup-and-native-confirmation.md) that `1c8be03c9`
repairs, now holding natively. Both launches said
`inherited preflight scope uses generated JSONC because legacy user-scope values are not representable by the current TOML reader: … unknown TOML config key "loopholes"`,
because this user config still carries a top-level `loopholes` key.

**The integration witnesses ran natively for the first time.** The nightly fails before them, in
its nix build and sudo steps (run 40885490887). Command:
`YOLO_TEST_MACOS_USER=1 go test -count=1 -timeout 0 -v -run '^(TestMacosUserInheritedUserScopeArrivesReadOnly|TestMacosUserOverlayInstallKeepsTheAgentStateBesideItsSkills)$' ./integration`.

| Test | Result |
| :--- | :--- |
| `TestMacosUserOverlayInstallKeepsTheAgentStateBesideItsSkills` | PASS |
| `TestMacosUserInheritedUserScopeArrivesReadOnly` | **FAIL**: `file\|MISSING, want PACKS`, `append\|ALLOWED, want DENIED`, `rename\|ALLOWED, want DENIED` |

**The failing test is stale.** Since `7e2d1ba93` (2026-10-28), a user scope that TOML can represent
is delivered as `~/.config/yolo-jail/config.toml`
([`inheritPreflightRel`](../../internal/cli/run/inheritscope.go#L60)). `config.jsonc` is now only the
legacy fallback the maintainer's config takes. The test's isolated home writes
`{"packs": ["claude"]}`, which TOML represents. The test then probes `config.jsonc`
([the probe](../../integration/macosusercontent_test.go#L178)), which is absent, and its own append
and rename create the file.

A throwaway copy of the test, with only the path changed to `config.toml`, passed natively: file
`PACKS`, `yolo pack ls` showing claude, append and rename `DENIED`, side layer `ALLOWED`. The copy
was deleted. The kernel does protect the inherited scope. No fix was made here.

### 4.2 Capture context and Copilot

The workspace was a throwaway, with `"macos_user_mode": "self"` in `overrides.jsonc`. The runbook's
`yolo-jail.jsonc` line cannot select self mode: only the user scope can.

- **`yolo capture copilot` gets a valid context.** No `invalid macos-user launch context` line
  appeared. The vendor installer downloaded the current release, validated its checksum and
  installed it. The launch then stopped with the intended refusal,
  `Copilot CLI versions 1.0.93 and newer cannot make native HTTPS requests under macos-user self's required SecurityServer deny. See https://github.com/github/copilot-cli/issues/5085. …`,
  and nothing was captured (exit 1).
- **`yolo -- copilot --version` gives the same intended refusal** with the 1.0.103 capture, not
  the parse error of the [earlier report](2026-10-29-mac-native-witnesses-apple-prune-and-copilot-probe.md#3-self-mode-copilot-version-probe).
  `f44d66d64` ("read a Copilot version that ends a sentence") now accepts a trailing period
  ([the regex](../../internal/entrypoint/programcompatibility.go#L156)).
- **[`OQ-MT11`](../design/macos-native-tls.md#OQ-MT11) cannot be reached on a shipped launch.** The compatibility guard refuses before Copilot
  makes any request, so the `bad certificate format` line the runbook expects cannot appear. The
  rendered profile still contains `com.apple.SecurityServer`.

### 4.3 Claude view On then Off

The workspace was a throwaway, in account mode. Fingerprints only:

| Step | Result |
| :--- | :--- |
| `YOLO_CLAUDE_CREDENTIAL_VIEW=1 yolo -- true`, then status | 2 views: this workspace's (`runtime macos-user, live`, no refresh token, access `793f2aca`) and the host store's |
| `YOLO_CLAUDE_CREDENTIAL_VIEW=0 yolo -- true` | `Claude: disabled this workspace's credential view; the broker will no longer maintain its view.` |
| Status after Off | 1 view: the host store's, unchanged |

After Off, the workspace's `.claude/.credentials.json` is a symlink to
`../.claude-shared-credentials/.credentials.json`, the shared-credentials path.

**New defect: a false warning.** Both launches printed
`yolo: warning: YOLO_CLAUDE_CREDENTIAL_VIEW is not a recognized launch setting; unset YOLO_CLAUDE_CREDENTIAL_VIEW. To choose a container runtime, use YOLO_RUNTIME=podman yolo.`
Yet both launches obeyed the variable. The warning comes from
[`warnUnknownYOLOEnv`](../../internal/cli/unknownlaunchenv.go#L11), whose known list
([caller](../../internal/cli/commands.go#L1262)) does not include
[`claudeview.SwitchEnv`](../../internal/claudeview/claudeview.go#L53). The warning tells the
user to drop the switch the runbook and the view design rely on, and its next step (podman) is
unrelated.

### 4.4 Apple Container prune

Apple Container 1.1.0 was the runtime. Every image row had a name and a `sha256:` digest, and the
one container row (`yolo-ac-builder`) had a reference and a digest.

- **Dry run:** `YOLO_RUNTIME=container yolo prune --no-containers` printed no `FAILED` line. It
  listed `Old yolo-jail images (keeping each workspace's current image: 3530 pointer(s))  would remove: 3`.
- **Apply:** `--apply` exited 0 and printed no `FAILED`. It removed `yolo-jail:10c5c729c035f116`,
  `yolo-jail:3366b7f555d58d5d` and `yolo-jail:37cfb9e1834ecd91`.
- **What was kept:** `yolo-jail:latest`, the builder image the running container names, and every
  image that is not yolo's.

The installed launcher before this deploy still printed `could not list this runtime's images`
([earlier report](2026-10-29-mac-native-witnesses-apple-prune-and-copilot-probe.md#2d-the-installed-launcher-still-has-the-old-defect)).

The three removed tags are exactly the three paths in `build/last-load-container`, the load
sentinel. [Image retention](../reference/image-retention.md#the-load-sentinel-and-what-it-may-be-cited-for)
rules that the sentinel is never liveness, so this follows the rules. The next Apple Container CI
job will reload its image. `image-delivery/10c5c729c035f116.delivered.json` still names a removed
tag; whether a launch trusts that record before loading was not tested.

## 5. Corrections the runbook needs

- Safehouse's render command has no `policy` word ([§3.2](#32-safehouse)).
- `"macos_user_mode": "self"` goes in the user scope (`overrides.jsonc` or `config.jsonc`), not in
  `yolo-jail.jsonc` ([§4.2](#42-capture-context-and-copilot)).
- [`OQ-MT7`](../design/macos-native-tls.md#OQ-MT7)'s integration witness probes `config.jsonc`; a TOML-representable scope arrives as
  `config.toml` ([§4.1](#41-inherited-account-relaunch)).
- [`OQ-MT11`](../design/macos-native-tls.md#OQ-MT11) needs a way past the compatibility guard to reach Copilot's request at all
  ([§4.2](#42-capture-context-and-copilot)).
- The runbook expects a P0 keychain refusal in a "sandbox-deny category". Natively it surfaces as
  `errSecItemNotFound` or `errSecInteractionNotAllowed` ([§3.4](#34-current-p0-and-tls-controls)).

## 6. State changed on this Mac

- `just deploy` replaced the `yolo` launcher (`d159a57f0` → `6f651f8c4`) and primed the broker.
- **Created and kept:** account `yolo-witness`. The maintainer removes it with
  `sudo bash /Users/Shared/yolo/witness-account.sh remove`.
- **Created and kept:** the script `/Users/Shared/yolo/witness-account.sh`.
- **Created and kept:** a throwaway SSH key in the session scratchpad.
- **Created, then deleted:** the fixture keychain, the probes and their sources, the Safehouse
  checkout and the staged `yolo` copy, all inside `yolo-witness`. Its search list and default
  keychain ended as they started.
- **Removed:** three `yolo-jail` Apple Container images, by the prune ([§4.4](#44-apple-container-prune)).
- **Created, then deleted:** workspaces `mt7-relaunch`, `mt8-capture` and `mt9-view` under
  `/Users/Shared/yolo`, `~/.config/yolo-jail/overrides.jsonc`, and two throwaway test files in the
  repository. `git status` was clean afterwards.
- **Claude view registration** ended at the one host-store view it started with. No login, logout
  or token change.
- **No owner keychain call:** nothing in the maintainer's own keychain was read or changed. No
  keychain call ran in the maintainer's session beyond what an ordinary yolo launch makes.
