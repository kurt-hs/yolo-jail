---
title: "Pants cannot resolve github.com in a podman jail where curl can"
date: 2026-10-19
status: open
tags: [lab-report, podman, macos, dns, vpn, musl, pants]
summary: "On a coworker's Mac, with the VPN connected, pants' first run in a podman jail fails with \"Could not resolve host: github.com\" while curl and ping in the same shell reach github.com. scie-pants is a static musl binary, and musl asks every nameserver at once; the jail lists two. Unconfirmed: the decisive one-nameserver test has not been run."
---

# Pants cannot resolve github.com in a podman jail where curl can, 2026-10-19

A [lab report](README.md). Two Macs on arm64, both on the company VPN, both running a podman
jail. On the maintainer's, pants bootstraps. On a coworker's, it fails to resolve `github.com`.
Nothing here has been fixed, and the cause is a hypothesis with one test left to run.

## Observed

**The maintainer's jail works.** `YOLO_RUNTIME=podman yolo` in `/Users/Shared/yolo/jail_test`,
with the VPN up (an internal host answered `ping`). In the jail, `pants` resolves to
`/mise/installs/github-pantsbuild-scie-pants/0.13.2/pants`, and its first run starts downloading
`cpython-3.14.4+20260414-aarch64-unknown-linux-gnu-install_only.tar.gz` from
`github.com/astral-sh/python-build-standalone`.

**The coworker's jail does not.** The same `pants` path, and its first run (`pants green ::`)
fails:

```text
Failed to source file cpython-3.14.4+20260414-aarch64-unknown-linux-gnu-install_only.tar.gz: Failed to fetch https://github.com/astral-sh/python-build-standalone/releases/download/20260414/cpython-3.14.4%2B20260414-aarch64-unknown-linux-gnu-install_only.tar.gz: [6] Could not resolve hostname (Could not resolve host: github.com)
Error: Failed to establish atomic directory /home/agent/.cache/nce/73fdf405…/locks/configure-e987562a…. Population of work directory failed: … The tar.gz destination /home/agent/.cache/nce/5c8db1c2…/cpython-3.14.4+20260414-aarch64-unknown-linux-gnu-install_only.tar.gz of size 0 had unexpected hash: e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855
```

Rerunning in the same shell fails the same way. In that shell, `ping github.com` gets replies,
and `curl -I https://github.com` returns `HTTP/2 200`. `getent` is not installed and
`/etc/nsswitch.conf` does not exist. Their `/etc/resolv.conf`:

```text
search int.hs.ai ec2.internal
nameserver 169.254.1.1
nameserver 192.168.127.1
```

Their prompt read `jail:/ctx/workspace/…` rather than `jail:/workspace`. The backend was
reported as podman, but no launch output from that machine was seen.

## What was measured here

- **The maintainer's podman container has the same two nameservers.** `podman run` of
  `localhost/yolo-jail:latest` printed `nameserver 169.254.1.1` and `nameserver 192.168.127.1`,
  and `search lan`. `169.254.1.1` is the DNS forwarding address pasta provides, and
  `192.168.127.1` is the podman VM's gvproxy gateway. Only the search line differs: the
  coworker's carries the VPN's domains.
- **scie-pants is a static musl binary.** The v0.13.2 `scie-pants-linux-aarch64` release asset,
  inspected with `file` and `strings` without running it: `ELF 64-bit LSB executable, ARM
  aarch64 … statically linked`, built under `/target/aarch64-unknown-linux-musl/`, with libcurl's
  `getaddrinfo() thread failed to start` string. So its downloads resolve names through musl's
  `getaddrinfo`, and the `[6]` in the error is libcurl's "could not resolve host" code.

## Open: why only musl fails on the coworker's machine

**The hypothesis.** musl's resolver sends each query to every listed nameserver at once and takes
the first reply, a negative one included. glibc asks the nameservers in order. If either of the
coworker's two servers answers `github.com` negatively, or wrongly, and fast, musl programs
fail while glibc programs keep working. The jail's `curl` and `ping` are presumably glibc
builds (unchecked), which would make them the programs that work. The VPN is the natural
suspect for the bad answer: its search domains reach this machine's `resolv.conf` and not the
maintainer's, so its DNS setup evidently reaches this VM differently.

**Not checked:** which of the two servers misbehaves, whether it is the A or the AAAA query, and
whether `/ctx/workspace` means this was not the backend it was reported as.

**The decisive test**, for the coworker, inside the jail. Keep one nameserver at a time and
rerun pants:

```console
$ cp /etc/resolv.conf /tmp/resolv.orig
$ grep -v '192.168.127.1' /tmp/resolv.orig > /etc/resolv.conf && pants --version
$ grep -v '169.254.1.1'   /tmp/resolv.orig > /etc/resolv.conf && pants --version
$ cp /tmp/resolv.orig /etc/resolv.conf
```

If one spelling works and the other fails, that server is the bad one and the hypothesis holds.
If both fail, the cause is not musl's parallel queries and this report is wrong about it.
`dig` or `nslookup` against each server, for `A` and `AAAA`, would then say what it answers, if
either tool is in the image.

**The empty cache file.** The failed fetch left a zero-byte archive in pants' cache (`e3b0c442…`
is the SHA-256 of empty input). Each rerun refetches, so it is not what blocks the coworker, but
`rm -rf ~/.cache/nce/5c8db1c2*` clears it if a rerun ever stops at the hash check alone.

**What a fix has to decide, once the test settles the cause.** yolo has no DNS setting today:
`yolo config-ref` names none, and the launcher passes podman no `--dns`. The choice is between
a config key for a launch's nameservers, a launcher that writes one nameserver when it can tell
which works, or leaving it to the user and documenting the edit above. It also has to decide
whether Apple Container's missing internet under the VPN, reported the same day, belongs to the
same cause.
