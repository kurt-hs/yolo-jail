---
title: "GitHub Copilot CLI TLS on macOS: no documented backend selector found"
date: 2026-10-28
status: accepted
stage: CURRENT
next: "Verify the current CLI's actual TLS path from its packaged runtime or GitHub, then test that path natively under the existing SecurityServer deny before making a stronger claim"
tags: [research, copilot, macos, tls, keychain, networking]
summary: "A bounded review of GitHub's Copilot CLI documentation and a public native-HTTPS regression report found no documented TLS-backend selector. The report is evidence about one reported runtime failure, not proof that all HTTPS or certificate verification requires blanket login-keychain access."
---

# GitHub Copilot CLI TLS on macOS: no documented backend selector found

**Status:** Checked 2026-10-28 against GitHub's published Copilot CLI command reference and network-settings documentation, plus public issue [#5085](https://github.com/github/copilot-cli/issues/5085). This is source research, not a native macOS reproduction or a finding about every Copilot CLI release. No TLS-backend selector was located in the searched documentation; that bounded negative result does not prove no undocumented or future option exists.

## Short answer

No documented Copilot CLI setting was found for selecting its TLS implementation or certificate verifier. GitHub documents proxy routing, additional CA input, and an HTTP/2 opt-in; those control routing, certificate inputs, or HTTP protocol negotiation, not a TLS-backend choice. The CLI's documented MCP `--transport` modes select how it connects to an MCP server, not how the CLI verifies HTTPS certificates.

This does **not** establish that Copilot must access the entire login keychain to verify a certificate. A public report describes one native HTTPS failure when macOS denies access to `com.apple.SecurityServer`, but it does not demonstrate that all certificate verification requires that service or disclose a supported backend selector. macOS keychain item access is separately governed by item access controls; permission to make a Mach service lookup is not the same as blanket authorization to read every keychain secret.

The repo has a measured, narrower example: [mise's native-TLS failure under the macos-user profile](../design/macos-native-tls.md) was traced to its SecureTransport path and the profile's deliberate `SecurityServer` deny, while the rustls-based release binary worked under that deny. That is evidence about mise, not proof of Copilot's implementation or a universal macOS TLS requirement.

## Terms

- **TLS backend** — the library or platform API a program uses to implement the TLS handshake and certificate verification. This is plain descriptive usage here, not a claim that GitHub documents a Copilot setting by that name.
- **Certificate verifier** — the component that decides whether a peer certificate chains to an accepted trust anchor and satisfies the applicable checks. The [rustls-platform-verifier project](https://github.com/rustls/rustls-platform-verifier) describes the platform-verifier implementation discussed in issue #5085.
- **Mach service lookup** — a macOS request to find a named service. Here `com.apple.SecurityServer` names the service denied by the referenced Seatbelt profile; allowing the lookup is not itself a statement that every keychain item's access controls are bypassed.
- **Keychain item access control** — macOS controls associated with keychain items that govern which clients may perform protected operations. Apple documents the [Security framework access-control-list APIs](https://developer.apple.com/documentation/security/access-control-lists).

## What the sources establish

### GitHub's published CLI documentation

The [Copilot CLI command reference](https://docs.github.com/en/copilot/reference/cli-command-reference) was searched in full for `native-tls`, `rustls`, TLS, certificate-store, and plaintext-token terms. The reference documents `COPILOT_ENABLE_HTTP2` values `1` or `true` as opting into HTTP/2, with HTTP/1.1 as the default. No documented TLS backend or verifier selector was found in that reference.

The [network settings documentation](https://docs.github.com/en/copilot/concepts/security-governance-and-network-settings/network-settings) documents conventional HTTPS proxy configuration and `NODE_EXTRA_CA_CERTS`. These describe proxy routing and additional CA input; the page does not document choosing a TLS backend. The CLI reference's MCP `--transport` options are protocol/connection modes and are not a TLS implementation setting.

This documentation check is scoped to those published pages as fetched on 2026-10-28. It cannot rule out an undocumented option, a setting added later, or a selector present only in a particular build.

## Fast-moving — verify before relying

### Public report of a native HTTPS regression

In [Copilot CLI issue #5085](https://github.com/github/copilot-cli/issues/5085), opened 2026-10-08 and still open with no comments when checked on 2026-10-28, the reporter says Copilot CLI 1.0.92 worked in a macOS environment denying `com.apple.SecurityServer`, while 1.0.93 and later failed native HTTPS calls there with `errSSLBadCert` (`-9808`). The report identifies a bundled Node runtime containing both `native-tls` and `rustls-platform-verifier`. Its author reports that a small, separate `reqwest` client using `rustls-platform-verifier` succeeded under the same deny, and that setting `SSL_CERT_FILE` or `NODE_EXTRA_CA_CERTS` did not fix Copilot's failing path.

These are the issue author's observations and analysis, not an independently reproduced Copilot test. The `reqwest` control is not Copilot itself. The report proposes routing the native HTTP clients through an existing rustls verifier; it does not identify a supported Copilot CLI setting for doing so. The observed version transition does not establish why the implementation changed or what motivated it.

## What is not established

- **No documented selector found is not proof no selector exists.** The published docs search is bounded; this report does not inspect the current CLI binary, all runtime strings, or private vendor documentation.
- **The issue does not prove Copilot needs blanket keychain access.** It records a failure in one denied-service environment and a separate client's success with another verifier. It does not show that every macOS HTTPS path needs `SecurityServer`, nor that granting the service lookup gives unrestricted access to all login-keychain secrets. Apple documents per-item access-control APIs; the precise authorization for a particular client and item depends on that item's controls and the operation.
- **No native Copilot experiment was run for this note.** The documented HTTP/2 switch is a separate protocol-layer variable and remains only a possible hypothesis to test, not an endorsed workaround or an experiment result. No Copilot process was run, no credentials or real API calls were used, and no certificate-verification bypass is recommended.
- **Editor extension settings are not CLI settings.** Settings named for VS Code's Copilot extension or its fetcher do not establish a supported Copilot CLI option and are not treated as CLI guidance here.

## Next verification

Before claiming that Copilot's native HTTPS path is fixed, selectable, or necessarily keychain-dependent, inspect the exact current CLI build's documented help and packaged runtime or obtain vendor confirmation, then reproduce the relevant behavior on macOS under the existing `SecurityServer` deny. Keep the deny in place; test any documented backend/protocol setting as a hypothesis, and verify certificate checking remains enabled. Do not infer a selector from editor settings, or advise disabling certificate verification.

## Sources and provenance

- [Copilot CLI command reference](https://docs.github.com/en/copilot/reference/cli-command-reference) — official CLI options and environment variables; full stored page searched for TLS-backend terms on 2026-10-28.
- [Copilot network settings](https://docs.github.com/en/copilot/concepts/security-governance-and-network-settings/network-settings) — official proxy and CA-input documentation, not evidence of backend selection; checked 2026-10-28.
- [Copilot CLI issue #5085](https://github.com/github/copilot-cli/issues/5085) — public, open report of one macOS denied-service regression and the author's separate `reqwest` control; checked 2026-10-28, not independently reproduced.
- [Apple Security access-control-list APIs](https://developer.apple.com/documentation/security/access-control-lists) — primary reference for per-item keychain access controls; consulted for the distinction between service lookup and item authorization.
- [mise's measured native-TLS case](../design/macos-native-tls.md) — project evidence for one native-tls/SecureTransport path and a rustls alternative under yolo's deny; not evidence about Copilot.
