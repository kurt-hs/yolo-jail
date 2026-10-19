---
title: "Two launch diagnostics that sent a reader the wrong way"
date: 2026-10-19
status: open
tags: [lab-report, apple-container, podman, diagnostics, happy-path, provisioning]
summary: "A maintainer ran two launches believing they were on podman; both were Apple Container, because YOLO_BACKEND is not a yolo variable and the line naming the chosen runtime goes only to the launch log. A Ctrl-C during provisioning then reported that the provisioning log could not be written and named permissions and disk, while the log on disk was complete and 67 GiB were free."
---

# Two launch diagnostics that sent a reader the wrong way, 2026-10-19

A [lab report](README.md). Host: macOS 26.5, arm64. Workspace `/tmp/jail_test`, yolo
`0.14.0+555.gc3a38e634b7e`. Both findings are about what a launch *says*, not what it does, and
each cost the maintainer a wrong conclusion in this session.

## 1. The runtime a launch chose is never printed, and `YOLO_BACKEND` is not a variable

**Observed.** Two launches were made as
`YOLO_ALLOW_MISSING_PROGRAMS=1 YOLO_BACKEND=podman yolo`, and reported as podman runs. Both ran
on **Apple Container**: `/tmp/jail_test/.yolo/launch.log` records
`jail: yolo-jail-test-1d7fd0de  runtime=container  notch=jail` for the 10:39:22 and 10:40:43
launches.

**Two causes, and either alone would have been caught.**

- **`YOLO_BACKEND` is not a yolo dial.** The spelling appears nowhere in the tree — no Go source,
  no doc, no config example. The dial is `YOLO_RUNTIME`
  ([`config-ref`'s `ENVIRONMENT VARIABLES`](../reference/storage-and-config.md)). An unknown
  `YOLO_*` variable is accepted in silence, so a typo or a misremembered name is
  indistinguishable from a working override.
- **Nothing on screen names the backend.** The line that carries it is written with
  `o.logRecord` ([`launchcontext.go:633`](../../internal/cli/run/launchcontext.go)), and
  `logRecord` writes to `LaunchLogOnly(o.Stderr)`
  ([`launchcontext.go:81`](../../internal/cli/run/launchcontext.go)) — the launch log alone.
  ⚠ The `yolo: container.provision took …` timing line is **not** a disclosure of which backend
  ran: `container.provision` is recorded for any container backend
  ([`jailperf.go:151`](../../internal/cli/run/jailperf.go)), so it reads the same for podman and
  for Apple Container.

**Why it matters more than a typo.** The two backends fail differently, and this session has one
defect that reaches only one of them: the Apple Container build gate rejects every real
`container ls` ([the store-build design's Apple arm](../design/pi-extension-store-builds.md),
roadmap item 7), and its gate is `runtime == "container"`, so podman never meets it. A reader who
believes they switched runtimes and did not will attribute a backend-specific failure to both,
which is exactly what happened. **Podman remains untested on this host.**

**What a fix has to decide, rather than what it is.** Whether the launch names its runtime on
screen (a one-line disclosure, and
[AGENTS.md](../../AGENTS.md)'s *"a launch has no quiet mode"* argues it should), whether an
unrecognized `YOLO_*` variable is warned about, or both. ⚠ The second is not free: yolo
deliberately gives `YOLO_*` dials no central registry
([AGENTS.md](../../AGENTS.md), *"`YOLO_*` environment dials have no authority row, because they
have no authority"*), so "unknown" has no cheap definition today, and a warning list is a second
copy of a set that is deliberately distributed. The screen line needs no registry.

## 2. An interrupted provisioning stage blames permissions and disk

**Observed.** A `Ctrl-C` during the mise stage printed:

```text
Provisioning log could not be written: /workspace/.yolo/startup.log — repair workspace .yolo
permissions or free disk space, then retry yolo.
```

**Both named causes were false, and the run itself disproves them.**

| Claim | Measured |
| :--- | :--- |
| the log could not be written | `/tmp/jail_test/.yolo/startup.log` is 6637 bytes, mtime 10:45, and its **last line is `PROVISIONING FAILED (exit 141)`** — written by a later append in the same run |
| repair `.yolo` permissions | `-rw-r--r--  1 matt  wheel`, owned by the invoking user |
| free disk space | 67 GiB free on the volume |

**The mechanism.** The provisioning stage runs `(setup) 2>&1 | yolo internal log-append --tee`
and reads `PIPESTATUS`: `_prc` is the setup's status, `_plog` the log writer's
([`internal/provision/provision.go`](../../internal/provision/provision.go)). `Ctrl-C` signals
the whole foreground process group, so the writer is killed too and `_plog` is non-zero;
`logFailure` then fires with its permissions-and-disk text. `_prc` is **141** — `128 + 13`,
SIGPIPE — because the setup's stdout closed when the writer died first.

> [!NOTE]
> **The run contradicts itself inside one stage, which is the cleanest evidence available.**
> `_plog` says the log is unwritable, and the very next `printf … | log-append` wrote
> `PROVISIONING FAILED (exit 141)` into that same file successfully. A guard that fires on a
> signalled writer cannot be reporting a write failure.

**What a fix has to decide.** A signalled writer is not a failed one: a `_plog` of 130 (SIGINT)
or 141 (SIGPIPE) is the user's own interrupt, and its next step is nothing. Whether that is
distinguished by status, or whether the message simply stops naming causes it has not checked,
is a ruling — [the happy-path principle](../reference/happy-path-principle.md) says a stop names
*the easiest next step that keeps the user's trust*, and naming two wrong ones spends it instead.

## Not a yolo defect

The mise stage's long stall — `github:pantsbuild/scie-pants@0.13.2` retrying
`mise-versions.jdx.dev` and then `api.github.com` at 20 s a time for over two minutes — is **the
maintainer's VPN blocking container egress, a known issue on this host**, not yolo's. Recorded
only so a later reader does not re-diagnose it: the host reached both URLs in 0.42 s and 0.11 s
while the jail could not, which is the signature of that VPN and not of a yolo network decision.
No bound or hint from yolo is claimed to be at fault here.

## Evidence

```console
$ grep -rn YOLO_BACKEND .                 # no match anywhere in the tree
$ grep 'runtime=' /tmp/jail_test/.yolo/launch.log | tail -1
  jail: yolo-jail-test-1d7fd0de  runtime=container  notch=jail
$ tail -1 /tmp/jail_test/.yolo/startup.log
PROVISIONING FAILED (exit 141)
$ ls -l /tmp/jail_test/.yolo/startup.log   # -rw-r--r-- 1 matt wheel 6637
$ df -h /tmp | tail -1                     # 67Gi avail
$ curl -s -o /dev/null -m 12 -w '%{http_code} %{time_total}\n' https://api.github.com/
200 0.113234
```
