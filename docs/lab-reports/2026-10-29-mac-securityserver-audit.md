---
title: "SecurityServer boundary audit on the Mac: the deny is the only barrier for a readable keychain, the file deny for the login keychain, and no certificate-only path needs securityd except SecureTransport"
date: 2026-10-29
status: open
tags: [lab-report, macos, macos-user, seatbelt, keychain, tls, copilot, evidence]
summary: "The five-step audit macos-native-tls.md asks for, run on the maintainer's Mac against d159a57f0 with disposable keychains only. Under the shipped self-mode profile every keychain operation tried fails. Re-allowing com.apple.SecurityServer alone exposes everything (secret read, write, delete, lock) on a keychain whose file the sandbox can read, and nothing on one it cannot. curl, URLSession, SecTrustEvaluateWithError and Node all verify HTTPS under the unchanged profile; only SecureTransport fails. Copilot 1.0.105 links both native-tls and rustls-platform-verifier and documents no selector. The deny stays."
---

# SecurityServer boundary audit, 2026-10-29

A [lab report](README.md) for the
[Mac-side follow-up](../design/macos-native-tls.md#mac-side-follow-up--audit-the-securityserver-boundary)
in `macos-native-tls.md`. Each numbered section below is one of its five steps.

- **Host:** the maintainer's Mac, macOS 26.5 (25F71), arm64, Command Line Tools only (Swift 6.3.2).
- **yolo:** `just deploy` of `d159a57f0` (`0.15.1+705`).
- **The shipped profile was not changed.** Variants existed only as files in a throwaway harness.
- **No real credential was read or printed, and the real login keychain was not modified.** The
  search list and default keychain were recorded before and after, and were identical:
  `login.keychain-db` (user), `System.keychain` (common), `login.keychain-db` (default).
- **Fixture secrets were redacted** by the probe before any output was printed.

Terms used below:

- **Profile:** the Seatbelt profile (`.sb` text) `sandbox-exec` enforces. "The shipped profile" is
  the self-mode (run-as-you) one, `macosuser.DenyDefaultProfile`.
- **securityd family:** the Mach service names `com.apple.SecurityServer`, `com.apple.securityd`
  and `com.apple.securityd.xpc`. The profile denies all three, plus `com.apple.secd`.
- **File-based keychain:** a `.keychain-db` file read through the old CDSA keychain API (what
  `/usr/bin/security` uses). The login keychain is one. The data-protection keychain (`secd`) is
  not, and is not covered here.

## 1. The boundary as shipped, and what b4/b5 established

**Which profile carries the deny.** There are three profiles, and the securityd deny matters in
two of them:

| Profile | Base | securityd |
| :--- | :--- | :--- |
| Self-mode launch: `DenyDefaultProfile` ([`seatbeltdeny.go`](../../internal/macosuser/seatbeltdeny.go#L228)) | `(deny default)` | Never in the `mach-lookup` allowlist ([L677](../../internal/macosuser/seatbeltdeny.go#L677)), and denied again by name last ([L912](../../internal/macosuser/seatbeltdeny.go#L912)) |
| Self-mode capture | the same deny-default profile ([`seatbeltcapture.go`](../../internal/macosuser/seatbeltcapture.go#L247)) | the same |
| Account mode and its capture (`SeatbeltProfile`, `SeatbeltCaptureProfile`) | `(allow default)` | not denied. The process runs as `_yolojail`, which cannot read the owner's keychain files |

`com.apple.trustd.agent` is allowed. No other rule in the generated profiles allows `mach-lookup`.

**What b4 and b5 established.** [Check b4](../plans/runbooks/macos-user-model-prototype/run-checks.sh)
adds a throwaway item to the real login keychain with `-T /usr/bin/security` (no prompt for that
tool). It then checks that `security find-generic-password -w` does not print the secret inside
the sandbox, through both the search list and the explicit path. b5 checks that the login
keychain file cannot be read. Both passed in the
[self-mode rerun](../design/macos-user-model.md#self-mode-rerun-2026-10-23).

So b4 and b5 establish two things only: a no-prompt secret read of a login-keychain item through
`security`, and a byte read of the login keychain file, are both denied. b4 cannot say which
layer did it, because the file deny and the securityd deny were both in place. Neither check
covers attribute queries, enumeration, writes, deletes, or lock and unlock. That historical
fixture was not repeated here.

## 2. Disposable-keychain measurements

A disposable keychain was made outside the sandbox with `security create-keychain`. Apple's
source says creating a keychain does not change the search list unless its name is a login or
System keychain (`shouldAddToSearchList` in `StorageManager.cpp`, "Creation of a private keychain
should not modify the search list: rdar://13529331"). The snapshots confirmed it. The keychain was
set to never auto-lock and unlocked. It held one generic password with `-T /usr/bin/security`,
which is b4's shape, and a second "victim" item for the delete test.

The same probe ran four ways:

- **control:** no sandbox;
- **launch:** inside a real `yolo -- bash` self-mode launch;
- **p0:** `sandbox-exec` with `yolo internal macos-user-profile --mode self` output, byte for byte
  the shipped profile for that workspace;
- **p2:** p0 plus one appended `(allow mach-lookup (global-name "com.apple.SecurityServer"))`.
  A variant re-allowing all three securityd names (p1) gave the same results as p2.

### 2a. A keychain whose file the sandbox can read (in the workspace)

| Operation | control | launch | p0 | p2 |
| :--- | :--- | :--- | :--- | :--- |
| F0 read the file's bytes | ok | ok | ok | ok |
| F1 secret read (`find-generic-password -w`) | **secret printed** | 44 not found | 44 | **secret printed** |
| F2 attribute query | attributes | 44 | 44 | attributes |
| F3 enumeration (`dump-keychain`) | items listed | rc 0, nothing | rc 0, nothing | items listed |
| F4 `show-keychain-info` | ok | 206 | 206 | ok |
| F6 write (`add-generic-password`) | ok | 152 "Unable to obtain authorization" | 152 | **ok, item added** |
| F7 delete the victim item | ok | 44, item still there | 44 | **ok, item deleted** |
| F8 `lock-keychain` | ok | 206 | 206 | **ok, locked** |
| F9 `unlock-keychain -p` | ok | 206 | 206 | ok |

Afterwards the fixture was checked outside the sandbox: the launch's write never landed, the
victim was still there, and the keychain was still unlocked.

**Reading:** for a keychain whose file the sandbox can read, the securityd deny is the only
barrier. Re-allowing `com.apple.SecurityServer` alone hands the sandbox every operation in the
table, including the b4 shape: a no-prompt secret read.

### 2b. A keychain whose file the sandbox cannot read

A second disposable keychain was put outside every granted root, and the probe ran under p0 and
p2:

| Operation | p0 | p2 |
| :--- | :--- | :--- |
| F0 read the file | EPERM | EPERM |
| F1, F2, F7 | 44 not found | 44 not found |
| F3 | rc 0, nothing | rc 0, nothing |
| F4, F8, F9 | 206 "parameters … not valid" | 161 `UNIX[Operation not permitted]` |
| F6 | 152 | 152 |

**Reading:** with securityd reachable, every path-based operation still fails, because the
client opens the database file itself and the profile denies that. p2's errors change from
"parameters not valid" to the client's own EPERM, which shows where the call now stops. So for
a file-based keychain, the file deny on `~/Library/Keychains` (b5) is a second, independent
layer.

### What this does and does not say about the login keychain

- **It does say:** the two layers are independent. Each one alone defeated every path-based
  operation tried, on a keychain in the same unlocked state the login keychain is normally in.
- **It does not say** that the login keychain behaves the same. It is the default keychain,
  securityd knows it by session, and it has special cases in Apple's source. None of that was
  measured.
- **Not tested: path-less session operations.** `security lock-keychain -a`
  (`SecKeychainLockAll`) names no file, so the file deny cannot stop it. Under p2 it would
  presumably lock the real login keychain. That is a denial of service, not a disclosure. Testing
  it means locking the maintainer's login keychain, so it was not run.
  *Smallest controlled test, if wanted:* with the maintainer present and able to unlock, run
  `sandbox-exec -f p2.sb -- security lock-keychain -a`, then `security show-keychain-info` outside.
- **Not covered:** the data-protection keychain (`com.apple.secd`), which is a separate daemon
  and separately denied.

**Conclusion for steps 1 and 2:** the deny is load-bearing. Option C in
[macos-native-tls.md §4](../design/macos-native-tls.md#4-alternatives) would leave the login
keychain behind one layer (the file deny) instead of two. It would also expose every keychain
file the sandbox can read, and session-wide operations like lock-all, to the agent.

## 3. Certificate-only mechanisms, compared in the harness

Each client fetched `https://api.github.com` and `https://example.com`, with negative controls
against `badssl.com`. The probes were built locally and are listed below.

| Mechanism | API | no sandbox | p0 (shipped) | p2 (+SecurityServer) | Negative control under p0 |
| :--- | :--- | :--- | :--- | :--- | :--- |
| `/usr/bin/curl` | LibreSSL and a CA file | 200 | 200 | 200 | self-signed: `curl: (60)` |
| URLSession (Swift) | Network.framework, trust by trustd | 200 | 200 | 200 | n/a |
| **`SecTrustCreateWithCertificates` + `SecTrustEvaluateWithError`** (Swift; the API rustls-platform-verifier calls) | trustd | ok (chain 3) | **ok** | ok | self-signed: `-67843 "certificate is not trusted"` |
| **SecureTransport `SSLHandshake`** (C; the path Rust `native-tls` takes) | `SSLCopyPeerTrust`, the CDSA key path | 0 | **-9808 `errSSLBadCert`** | 0 | expired: -9808 |
| Node 24.19 `fetch` | OpenSSL, bundled roots + `NODE_EXTRA_CA_CERTS` | 200 | 200 | 200 | self-signed: `DEPTH_ZERO_SELF_SIGNED_CERT` |
| Node `--use-system-ca` | Security framework store read | 200 | 200 | 200 | same |

- **SecureTransport is the only mechanism that needs securityd.** A 40-line client reproduces
  mise's failure exactly: -9808 is the "bad certificate format" from
  [§2 of the design](../design/macos-native-tls.md#2-the-cause). It passes once SecurityServer is
  re-allowed. So the mise finding is a property of the API, not of mise.
- **`SecTrustEvaluateWithError` verifies under the unchanged profile, and still rejects an
  untrusted certificate.** This is the evidence [§3 of the design](../design/macos-native-tls.md#3-the-proposal) lacked for the rustls path. It
  shows the verifier API works; it does not show a rustls build of mise does.
- **Node's system store is closed by the file deny, not by securityd.**
  `tls.getCACertificates("system").length` is 2 outside and **0 under p0, p2 and p1 alike**.
  Node reads the System keychain, which the profile denies (`/Library/Keychains`). This does not
  break Node here: a self-mode launch sets `NODE_EXTRA_CA_CERTS` to a 1-certificate file
  ([`cabundle.go`](../../internal/macosuser/cabundle.go)), and Node verified through that.
- **Not tested:** a server signed by the MDM CA. This Mac had no TLS-intercepted host to try,
  so trustd trusting the corporate CA under p0 is still the design's claim, not a measurement.
- *Harness noise:* one Node run, started by relative path, aborted in Node's process
  initialisation under p0. The same run by absolute path worked. It was not investigated.

## 4. The current Copilot CLI, by static inspection

- **Version:** the latest release is `v1.0.105` (published 2026-10-28). This Mac's capture is
  `1.0.103` (`copilot --version`).
- **Download:** `copilot-darwin-arm64.tar.gz` and the npm tarball
  `github-copilot-1.0.105-darwin-arm64.tgz` were downloaded and checked against the release's
  `SHA256SUMS.txt`. Running the downloaded binary (`--version`, `--help`) was refused by this
  session's permission policy, so 1.0.105's help output was **not** collected. Everything below
  is static.
- **Both TLS stacks are linked.** The Rust addon `prebuilds/darwin-arm64/runtime.node` (98 MiB)
  contains `native-tls-0.2.18`, `native-tls-0.3.1`, `security-framework-3.7.0`,
  `hyper-rustls-0.27.10`, `rustls-0.23.46` and `rustls-platform-verifier-0.7.1`. It imports both
  `SSLHandshake`/`SSLCreateContext` and `SecTrustEvaluateWithError`. It also imports keychain
  item APIs (`SecKeychainFindGenericPassword`, `SecItemCopyMatching`), presumably for its own
  token storage. Which client uses which stack cannot be read from the strings.
- **No TLS-backend selector was found.**
  - Every `COPILOT_*` name in `runtime.node` and `cli-main.js` was listed. None names TLS,
    certificates, rustls or native-tls. The network-related ones are
    `COPILOT_NATIVE_HTTP_TIMEOUT_MS`, `COPILOT_PROXY_KERBEROS(_SPN)` and
    `COPILOT_PROVIDER_TRANSPORT` (the provider wire protocol).
  - `COPILOT_ENABLE_HTTP2`, which the
    [research note](../research/copilot-native-networking.md) cites from GitHub's docs, does not
    appear in either file of 1.0.105.
  - The JavaScript has no `rustls`, `native-tls`, `use-system-ca` or `rejectUnauthorized`.
  - The addon does read `NODE_EXTRA_CA_CERTS` and `CURL_CA_BUNDLE` next to its PEM parser. That
    is an extra-CA input, not a backend choice.
  - The package's `changelog.json` has no entry mentioning TLS, certificates, keychain or HTTP/2.
- **What this implies, without running Copilot:** whichever native requests go through
  `native-tls` will fail under the shipped profile exactly as the SecureTransport probe does
  (section 3). The ones that go through rustls-platform-verifier would not. A native Copilot run
  under the profile is a separate, manually authorized test, and was not done.

## 5. Outcome

**The deny must stay as it is.** Section 2 shows that `com.apple.SecurityServer` is the only
thing between the agent and any keychain whose file it can read, and the outer layer for the
login keychain. A name-level allow cannot be narrowed to certificate work.

**A supported approach exists for verifying certificates without securityd:**
`SecTrustEvaluateWithError`, through trustd, which the profile already allows. It verified a
public chain and rejected an untrusted one with every keychain negative control still passing
(section 2a, launch and p0 columns). URLSession, curl and Node with yolo's `NODE_EXTRA_CA_CERTS`
also work. The one failing path is SecureTransport, which Rust `native-tls` uses on macOS.

**What is still owed:**

- **mise:** the design's option B (rustls build) remains the fix. Section 3 now measures the
  verifier API it relies on.
- **Copilot:** the fix is upstream. It means routing its native HTTP clients through the
  rustls-platform-verifier it already links, which is
  [#5085](https://github.com/github/copilot-cli/issues/5085)'s proposal. That issue stays open,
  and no GitHub operation was done.
- **Open, and small:** the lock-all test above, and a measurement of trustd trusting the MDM CA
  under p0, if a TLS-intercepted host is available.

## 6. Also seen

- **`yolo -- bash` in self mode reports
  `cannot parse copilot's declared version probe for this macos-user self launch`**, and copilot's
  install exits 1. The launch continued because `YOLO_ALLOW_MISSING_PROGRAMS=1` was set. This
  looks like the same family as the host "invalid macos-user launch context" error in the
  [CI cleanup report](2026-10-29-mac-ci-cleanup-and-native-confirmation.md#33-host-launch-invalid-macos-user-launch-context),
  from the Copilot compatibility controls (`fea1f1ce7`).

## 7. State changed on this Mac

- Created and then deleted (`security delete-keychain`): two disposable keychains, one under
  `/Users/Shared/yolo/sec-audit` and one in this session's scratch directory. The search list and
  default keychain were the same before and after.
- `~/.config/yolo-jail/overrides.jsonc` (`macos_user_mode: "self"`) was created and removed;
  `yolo check --no-build` passed afterwards.
- `/Users/Shared/yolo/sec-audit` (workspace, harness profiles, probes) was removed.
- The Copilot 1.0.105 download stays in the session scratch directory, never installed.
- Separately, at the maintainer's request: the CI runner's post-job hook from the
  [CI cleanup report](2026-10-29-mac-ci-cleanup-and-native-confirmation.md#prevention-a-post-job-hook-staged-but-not-registered)
  is now registered (`ACTIONS_RUNNER_HOOK_JOB_COMPLETED` in `~/actions-runner/.env`), and the
  runner service was restarted.
