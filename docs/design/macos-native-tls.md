---
title: "mise cannot reach HTTPS inside macos-user, and the keychain deny is why"
date: 2026-10-23
status: in-review
stage: DESIGN
next: "Rule OQ-MT1 (witness C passed on the maintainer's Mac 2026-10-23, so A is available) and OQ-MT2"
tags: [design, macos, macos-user, seatbelt, tls, mise, ca-trust]
summary: "Inside the macos-user Seatbelt profile, yolo's nixpkgs mise fails every HTTPS call with 'bad certificate format'. It is built with native-tls, whose SecureTransport handshake reads the server key through Apple's old CDSA code, which checks in with securityd (com.apple.SecurityServer). The profile denies that service on purpose because it fronts the login keychain. The proposal keeps the deny and ships a mise build that verifies through rustls-platform-verifier, which talks to trustd only. Unmeasured: the witnesses in §5 settle it on a Mac."
---

# mise cannot reach HTTPS inside macos-user, and the keychain deny is why

**Status:** 2026-10-23. Nothing built. The cause is traced from source and **measured on the
maintainer's Mac** ([the Mac run](#measured-on-the-maintainers-mac-2026-10-23)): every witness came out as
expected, and witness C passes, so option A works.

> **In short.** Keep securityd denied and change mise, the same way the Go fix changed Go: ship
> a mise build whose TLS verifies through the trust daemon the profile already allows, so
> nothing in the sandbox needs the keychain's front door.

**Why it matters.** On a run-as-you launch every tool a repo's `mise.toml` declares (node, go, uv,
just, …) fails to install, so the agent starts without them. It was reported from a managed Mac;
the cause in [§2](#2-the-cause) predicts every Mac.

**Cost.** A second mise version pin on darwin (or a slower source build), and nixpkgs' patches to
mise are lost on that path.

**Start at [§2](#2-the-cause).**

**Needs your ruling:** [OQ-MT1](#OQ-MT1), [OQ-MT2](#OQ-MT2).

**Reads with:** [`provisioner-sets.md`](provisioner-sets.md) (the CA trust rulings, PS-D10 and
PS-D14), [`macos-user-model.md`](macos-user-model.md) (the profile and its keychain promise).

---

## 1. What was seen

From a run-as-you launch on a managed Mac with an MDM-installed corporate CA:

- **mise fails every HTTPS call** inside the sandbox with `bad certificate format`.
- **curl works** inside the same sandbox, and mise works outside it and under a fully permissive
  profile, with either CA bundle.
- **One deny is responsible.** Removing the profile's denies one at a time, then the service names
  one at a time, isolates `(global-name "com.apple.SecurityServer")`
  (`internal/macosuser/seatbeltdeny.go:881`, commented "securityd: the login keychain"). Allowing
  that one name makes mise work under the otherwise unchanged profile.

## 2. The cause

Traced from source; the witnesses in [§5](#5-witnesses-for-the-mac) measure it.

1. **The mise in the sandbox is yolo's.** nixpkgs `mise` is in the core floor (`flake.nix:1495`),
   which feeds the macos-user tool profile, and that profile ranks ahead of Homebrew on the
   sandbox PATH (`internal/macosuser/macosuser.go:1429`).
2. **That build uses native-tls.** mise's `Cargo.toml` defaults to `native-tls`, and nixpkgs does
   not change the default features (Homebrew's formula is unchecked). On macOS native-tls hands the handshake to
   SecureTransport.
3. **SecureTransport reaches securityd before any trust check.** It reads the server's public key
   with the old CDSA `SecTrustCopyPublicKey`, which builds a CSSM key and so opens CSSM's module
   directory, whose first act is to contact securityd at `com.apple.SecurityServer`. Denied, the
   key comes back NULL and the handshake fails with `errSSLBadCert`, whose documented text is "bad
   certificate format".
4. **Nothing is detected at run time.** Which trust path a program takes is fixed when it is
   built: native-tls compiled for macOS always calls the Security framework, and that framework
   has no environment variable or file that points it elsewhere. So the sandbox cannot steer it;
   only a different build can.
5. **CA files play no part.** mise loads no certificate file and reads no `SSL_CERT_FILE` on
   macOS, so which CAs the launch's bundle holds ([`cabundle.go`](../../internal/macosuser/cabundle.go))
   changes nothing. Dropping the login keychain's CAs would not help.

> [!NOTE]
> curl and nix git read the bundle file, as every TLS client in a podman jail does, which is why
> they work. Native macOS programs ask the Security framework instead. Go is the one such program
> fixed so far: the launch sets a `GODEBUG` value so Go verifies against the bundle
> ([`cagodebug.go`](../../internal/macosuser/cagodebug.go)). mise has no equivalent switch.

## 3. The proposal

**Keep the deny, and deliver mise's upstream release build on darwin.** This is not a yolo build of
mise: it is the binary mise's own installer (`mise.run`) and its GitHub releases hand every user,
so on a Mac it is arguably the more common mise than nixpkgs'. mise's release tarballs are
built with `rustls-native-roots`, which on macOS verifies through rustls-platform-verifier. That
verifier creates and evaluates a `SecTrust` (the newer API), which sends evaluation to trustd, and
`com.apple.trustd.agent` is already allowed (`seatbeltdeny.go:653`). It never touches the CDSA path
in [§2](#2-the-cause) step 3. trustd reads the System keychain's trust settings itself, so an MDM CA
should be trusted with no bundle at all.

- **Delivery:** a hash-pinned fetch of the release tarball for each darwin architecture in
  `flake.nix`, in the macos-user tool profile in place of nixpkgs mise. Linux and the container
  backends keep nixpkgs mise.
- **Version:** pinned to the same version nixpkgs ships, and bumped with it.
- **Done looks like:** in a run-as-you launch with the profile unchanged, `mise ls-remote node`
  prints versions, and a repo's `mise.toml` tools install.

## 4. Alternatives

| Option | Cost | Security | Verdict |
| :--- | :--- | :--- | :--- |
| **A — release mise on darwin** ([§3](#3-the-proposal)) | A second version pin; nixpkgs patches lost | The keychain deny stays | Proposed, if witness C passes |
| **B — rebuild nixpkgs mise with `rustls-native-roots`** | No binary cache: mise compiles on each Mac after every nixpkgs bump, and its TLS crate may need cmake | Same as A | Fallback if a release pin is unwanted |
| **C — allow `com.apple.SecurityServer`** | One line | **Breaks run-as-you's promise**: with the login keychain unlocked, the agent could list item attributes, add items, and read any item whose access list admits it without a prompt. Seatbelt filters this service by name only, so no narrower allow exists | Rejected unless A and B both fail |
| **D — no mise in the sandbox** | Repos' tools missing on macos-user | None | Rejected; it is today's failure |
| **E — ask upstream to make rustls the macOS default** | Slow; not in our hands | — | Worth filing; not a fix |
| **F — ask nixpkgs to build mise with `rustls-native-roots`** | One nixpkgs pull request; yolo then keeps nixpkgs mise everywhere | Same as A | The cleanest end state; A bridges until it lands |

## 5. Witnesses for the Mac

For the maintainer's Mac agent. Run in zsh on Apple silicon (on Intel, replace `arm64` with `x64`).
Each run uses fresh mise data and cache directories, so no cached remote list can stand in for a
network call.

```zsh
W=$(mktemp -d) && cd "$W"
printf '(version 1)\n(allow default)\n(deny mach-lookup (global-name "com.apple.SecurityServer"))\n' > d.sb
N=$(nix build --no-link --print-out-paths github:nixos/nixpkgs/fe13242281a67641c0102cdb1087f0ebe586c7df#mise)/bin/mise
curl -fsSL https://github.com/jdx/mise/releases/download/v2026.10.3/mise-v2026.10.3-macos-arm64.tar.gz | tar xz; R=$W/mise/bin/mise
p(){ MISE_DATA_DIR=$(mktemp -d) MISE_CACHE_DIR=$(mktemp -d) "$@" ls-remote node 2>&1 | tail -1; }
echo "A $(p $N)"; echo "B $(p sandbox-exec -f d.sb $N)"; echo "C $(p sandbox-exec -f d.sb $R)"
echo "D $(p env -u SSL_CERT_FILE sandbox-exec -f d.sb $N)"
echo | openssl s_client -connect nodejs.org:443 -servername nodejs.org 2>/dev/null | openssl x509 -noout -issuer
```

| Line | Expected | If not |
| :--- | :--- | :--- |
| **A**: nixpkgs mise, no sandbox | A Node version, such as `25.x.y` | The control failed; nothing below means anything |
| **B**: nixpkgs mise, only securityd denied | Contains `bad certificate format` | A version means [§2](#2-the-cause) is wrong |
| **C**: release mise, only securityd denied | A Node version | Any TLS error means option A fails; go to B or rule on C |
| **D**: as B, with `SSL_CERT_FILE` unset | Same as B | A version means the bundle path is involved after all, and [§2](#2-the-cause) step 5 is wrong |
| **issuer** | The issuer the Mac sees | If it names the corporate CA and C passed, the platform verifier trusts the MDM CA with no bundle |

### Measured on the maintainer's Mac, 2026-10-23

macOS 26.5 (25F71), arm64; nixpkgs mise `/nix/store/nf7990y6…-mise-2026.10.3`, release mise
`v2026.10.3` (`macos-arm64`, sha256 `0999bd4943523ccd…` of `bin/mise`). Every line came out as
expected, so [§2](#2-the-cause) holds and option A works:

| Line | Result |
| :--- | :--- |
| **A** | `26.12.1` |
| **B** | `bad certificate format` (after `error sending request`, `client error (Connect)`) |
| **C** | `26.12.1` |
| **D** | Same as B. The session already had `SSL_CERT_FILE` unset, so D also ran with it **set** to `/etc/ssl/cert.pem`: still `bad certificate format`, which confirms step 5 more directly |
| **issuer** | `C=US, O=Google Trust Services, CN=WE1`: this network does not intercept TLS, so the corporate CA was not in the path and the platform verifier's MDM trust is **not** exercised here |

**Under yolo's own profile too**, not only the one-deny witness: the profile
`yolo internal macos-user-profile --mode self` renders at `3c35c9f87` (which keeps the
`com.apple.SecurityServer` deny), with `env -i` and fresh mise dirs in its workspace. nixpkgs mise fails
the same way. Release mise lists `26.12.1` and `mise install node@22` installs `22.23.4`. So option A
needs no profile change. The run-as-you launch that hit this (with `mise_tools` from the user config)
failed in provisioning with the same error for `pipx:swarf` and the neovim plugin.

Still unmeasured: a host behind a TLS-intercepting proxy whose CA only the MDM installed, which is what
the issuer line would show.

**Then, once a build delivers option A:** a real run-as-you launch with the shipped profile, running
`mise ls-remote node` and `mise install` in a repo with a `mise.toml`. Expected: versions listed and
tools installed, with no profile change.

## 6. What this does not cover

- **Other native macOS programs** that use SecureTransport. Each would fail the same way; this doc
  fixes mise because it is the one reported. Witness B's method finds the next.
- **Account mode's login-keychain CAs** for the release build: [OQ-MT2](#OQ-MT2).
- **Container backends.** Their mise is Linux and reads the bundle file.

## 7. Open questions

1. 💬 <a id="OQ-MT1"></a>**[OQ-MT1](#OQ-MT1): How does mise get TLS inside macos-user?**

   <!-- vantage: oq id=OQ-MT1 leaning="A: witness C passed on the maintainer's Mac, under yolo's own profile too, so release mise on darwin keeps the keychain deny; dropped once option F lands in nixpkgs." -->

   Witness C passed on 2026-10-23 ([the Mac run](#measured-on-the-maintainers-mac-2026-10-23)), under yolo's
   own profile as well, so option A is available. The options and their costs are in
   [§4](#4-alternatives).

   - **A — Release mise on darwin**, mise's own official binary, hash-pinned, keychain deny kept;
     dropped once a nixpkgs change (option F) lands.
   - **B — Rebuild nixpkgs mise with rustls**, keychain deny kept, compiled on each Mac.
   - **C — Allow securityd**, which ends run-as-you's keychain promise.

   _Leaning:_ A, now that witness C passed; B if a second pin is unwanted; C only if both fail.

   **Answer:**
   > _(empty — fill in when decided)_

2. 💬 <a id="OQ-MT2"></a>**[OQ-MT2](#OQ-MT2): May the release mise miss login-keychain CAs in account mode?**

   You ruled that the host's trust, the login keychain's included, is the sandbox's
   ([`provisioner-sets.md`](provisioner-sets.md)). In account mode the sandbox runs as a separate
   account, so trustd consults that account's login keychain, not yours; a corporate CA only in your
   login keychain is then untrusted by mise, as it already is by Go programs there. Run-as-you mode
   is unaffected.

   - **A — Accept the gap**, disclosed at launch as Go's is, and close both together later.
   - **B — Hold option A** of [OQ-MT1](#OQ-MT1) until a way exists to hand trustd those CAs.

   <!-- vantage: oq id=OQ-MT2 leaning="A: the gap already exists for Go, it touches only account mode with a CA kept outside the System keychain, and holding the fix leaves every Mac without mise." -->

   _Leaning:_ A: the gap already exists for Go, it touches only account mode with a CA kept
   outside the System keychain, and holding the fix leaves every Mac without mise.

   **Answer:**
   > _(empty — fill in when decided)_

## Decision Ledger

| ID | Ruling / Decision | Date | Settled in | Built |
| :--- | :--- | :--- | :--- | :--- |
| MT-D1 | The `com.apple.SecurityServer` deny stays while any option that keeps it can work: it is how run-as-you keeps the agent away from the user's login keychain | 2026-10-23 | [§4](#4-alternatives) | — |
