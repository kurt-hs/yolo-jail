---
title: "Pants cannot resolve github.com in a podman jail where curl can"
date: 2026-10-19
status: open
tags: [lab-report, podman, macos, dns, vpn, musl, pants]
summary: "On a coworker's Mac, with the VPN connected, pants' first run in a podman jail fails with \"Could not resolve host: github.com\" while curl and ping in the same shell reach it. scie-pants is a static musl binary. Measured: musl fails, and glibc succeeds, when the upstream answers the AAAA query with NXDOMAIN, SERVFAIL or REFUSED. Both jail nameservers relay the Mac's own resolver, and every hop on the maintainer's Mac answers correctly, so his DNS is the remaining suspect."
---

# Pants cannot resolve github.com in a podman jail where curl can, 2026-10-19

A [lab report](README.md). Two Macs on arm64, both on the company VPN, both running a podman
jail. On the maintainer's, pants bootstraps. On a coworker's, it fails to resolve `github.com`.
Nothing here has been fixed, and the mechanism is measured, with one fact about his DNS left to collect.

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

## Proven: which DNS answers break musl and not glibc

A stub DNS server in one podman container, misbehaving in one way at a time, with
`socket.getaddrinfo("github.com", 443)` (the call libcurl's threaded resolver makes) run from
`python:3.12-alpine` (musl) and `python:3.12-slim` (glibc) in its network namespace, three runs
each:

| The stub's answer | musl | glibc |
| :--- | :--- | :--- |
| forwarded unchanged | ok | ok |
| `AAAA` answered `NXDOMAIN`, `A` forwarded | **fails at once** | ok |
| `AAAA` answered `SERVFAIL` or `REFUSED` | **fails after 5 s** | ok |
| two servers listed; the second answers everything `NXDOMAIN` at once | **fails** | ok |
| two servers listed; the first answers everything `NXDOMAIN` | fails | fails |

**Every row where musl fails and glibc succeeds is an upstream misbehaving** about the `AAAA`
query, or a second listed server answering negatively and fast. glibc keeps the `A` answer and
asks servers in order; musl fails the whole lookup and takes the first reply from any server.

## Not reproduced on the maintainer's Mac, and why

**Every hop answers correctly there.** `dig` for `github.com`, `A` then `AAAA`: `NOERROR` with
one answer, then `NOERROR` with none, from every server. That means the VPN resolver `10.19.0.2`
and the home router `192.168.1.1` asked directly from the Mac, and gvproxy and pasta asked from
inside a container. A plain musl container resolves `github.com` every time, with or without the
coworker's search domains.

**Both jail nameservers end at the Mac's own resolver.** The podman VM's `/etc/resolv.conf` names
only `192.168.127.1`, so pasta's `169.254.1.1` forwards there too, and gvproxy (v0.8.9) answers
through macOS's `libresolv`. So the race between the two jail servers (the fourth row) cannot
happen: both relay one upstream. What decides the answer is **which DNS server the coworker's
Mac uses for outside names.**

**The search domains are a snapshot, not a cause.** The VM's `resolv.conf` was written by
NetworkManager when the VM booted, from gvproxy's DHCP, so it carries the search domains of
whatever network the Mac was on at that moment: `lan` for the maintainer, the VPN's for the
coworker.

**The maintainer's AWS Client VPN** (tunnel `utun4`) sends all DNS to `10.19.0.2`, the VPC's
resolver. Per the maintainer, the coworker's VPN setup is the same and his internet DNS is not.
The remaining suspect is therefore his internet (router or ISP) DNS, reached for outside names,
answering `AAAA` for `github.com` with `NXDOMAIN`, `SERVFAIL` or `REFUSED`.

## Open: what the coworker's DNS answers

**Sent to the coworker on 2026-10-19; his reply is expected the next day.** Run in a terminal on
his Mac, outside any jail, with the podman machine running:

```bash
echo "== Mac DNS config"; scutil --dns | grep -E 'nameserver|search domain' | sort -u
echo "== each Mac DNS server, github.com AAAA"; for s in $(scutil --dns | awk '/nameserver/{print $3}' | sort -u); do echo "$s: $(dig +time=3 +tries=1 @$s github.com AAAA | grep -o 'status: [A-Z]*')"; done
echo "== gvproxy, github.com AAAA"; podman run --rm docker.io/library/alpine:3.20 sh -c 'apk add -q bind-tools; dig @192.168.127.1 github.com AAAA | grep -o "status: [A-Z]*"'
echo "== musl lookup (what pants does)"; podman run --rm docker.io/library/python:3.12-alpine python3 -c 'import socket; print(socket.getaddrinfo("github.com", 443, 0, socket.SOCK_STREAM)[0][4])'
```

**The baseline, from the maintainer's Mac:** `nameserver 10.19.0.2` and `search domain lan`,
`10.19.0.2: status: NOERROR`, gvproxy `status: NOERROR`, and the musl lookup printed
`('140.82.114.3', 443)`.

**Reading his output:**

- **The last line errors** (`gaierror`): the pants failure reproduces outside yolo.
- **A `status:` other than `NOERROR`** names the server sending the bad answer, and the table
  above says why only pants fails.
- **Identical to the baseline:** the hypothesis is wrong, and the next comparison is his jail's
  networking against a plain podman container's.

**Workaround, measured on the maintainer's Mac:** point the jail straight at the VPN resolver,
which answers both internal and outside names correctly. A musl container with
`--dns 10.19.0.2` resolved `github.com` and `vision.int.hyperscience.net`. In a running jail that
is `echo 'nameserver 10.19.0.2' > /etc/resolv.conf`, lost when the jail restarts.

**The empty cache file.** The failed fetch left a zero-byte archive in pants' cache (`e3b0c442…`
is the SHA-256 of empty input). Each rerun refetches, so it is not what blocks the coworker, but
`rm -rf ~/.cache/nce/5c8db1c2*` clears it if a rerun ever stops at the hash check alone.

**What a fix has to decide, once the test settles the cause.** yolo has no DNS setting today:
`yolo config-ref` names none, and the launcher passes podman no `--dns`. The choice is between
a config key for a launch's nameservers, a launcher that writes one nameserver when it can tell
which works, or leaving it to the user and documenting the edit above. It also has to decide
whether Apple Container's missing internet under the VPN, reported the same day, belongs to the
same cause.
