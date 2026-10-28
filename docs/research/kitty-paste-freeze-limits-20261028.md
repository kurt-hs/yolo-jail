---
title: "Kitty paste freeze: limits of the existing analysis"
date: 2026-10-28
status: in-review
tags: [research, kitty, terminal, pi]
summary: "Records what the existing bounded Kitty paste analysis does and does not establish, plus the owner-observed temporary shell recovery."
---

# Kitty paste freeze: limits of the existing analysis

## Reported observation

The owner reports that twice, pasting the same long Kitty selection with Ctrl+Shift+V through the `clip2path` wrapper left Pi's input, output and visible Working animation stopped while Kitty remained responsive. Ctrl+Z promptly returned a usable shell. This is a useful observation about that reported terminal session, not a reproduction or a diagnosis.

Here, the **outer proxy** means the host-side program forwarding terminal input and output; **inner Pi** means the Pi process behind that proxy. The exact incident host launcher and backend are not identified.

## What the existing evidence narrows

The Linux proxy source at immutable commit `e20d78ead92eedd9c8669daac441a541520fcd31` dispatches a proxied session through `RunWithProxyObserved` ([proxy entry](../../internal/cli/run/proxy_linux.go)). In that source, one loop writes child output to the host output descriptor, then later polls and handles host input; the output write's count and error are discarded ([proxy loop](../../internal/ttyproxy/ttyproxy.go)). When that loop recognizes Ctrl+Z, it restores the terminal and stops only the proxy process; it resumes later when continued ([suspend-key recognition](../../internal/ttyproxy/suspendkey.go)). These are source facts about e20, not proof that the incident used that exact launcher, backend or route.

**Conditional inference:** if the incident used that Linux route, a prompt Ctrl+Z that returned control to the shell weakens the explanation that the outer proxy was persistently blocked in its output write at that moment: the same loop must reach its input handling to recognize the key. It does not show that Pi recovered, that Pi's JavaScript thread ran, or that the condition was never transient.

A separate outer transport defect is established in that source: the proxy makes a single output write and does not retry a short write or handle its error. This is real source-level evidence about the outer writer, but it is separate from the incident. It does not establish that output pressure happened during either paste or that this defect caused the stopped Pi display.

The earlier bounded read-only Pi/TUI inspection was scoped to installed modules whose package metadata reported version 1.4.0, inspected on 2026-10-26; that version is not verified as the incident version. It found synchronous input/render work and a synchronous output sink as possible ways Pi could stop progressing, but did not establish a blocked Pi write, a loop, or another cause. The Working spinner uses an independent timer, but shares Pi's JavaScript thread and output path. Therefore, a missing bracketed-paste end marker by itself can leave paste input buffered; it does not account for an independently stopped spinner. Neither hypothesis was observed in the incident. Stationary pixels alone do not prove that Pi's thread died.

The bounded Kitty-producer follow-up considered public current-master excerpts supplied on 2026-10-26, not the installed incident Kitty. It refined how the reported wrapper route could deliver multiple completed paste blocks; it did not establish the incident route, delivery of every end marker, or the freeze's cause. Multiple visible folds and successful wrapper return are not delivery or Pi-consumption acknowledgments.

## What remains unknown

This evidence cannot identify the incident host launcher/backend, Pi or Kitty revisions, active extensions, or UI state. It also cannot tell whether the animation later recovered on its own, whether timer callbacks stopped or frames merely failed to reach the screen, or whether Pi encountered a blocked write or a missing frame. These are limits of this incident analysis, not prerequisites imposed on unrelated work.

## Safe temporary recovery

The only recovery reported here is the owner's observation: Ctrl+Z promptly returned a usable shell in that incident. Treat it as a temporary escape the owner observed, not a Pi repair, a general guarantee, or evidence the suspended job is healthy.

`fg` means asking the shell to resume its already-suspended foreground job. It is optional only if the owner chooses to resume it; it was not run here and its outcome is unknown. It is not the passive first step. Do not automatically resume or kill the job, signal a process group, inject paste markers, or create another stress reproduction.

The smallest next observation is passive: if the incident naturally recurs, the owner can note whether the animation resumes without further input and roughly how long it remained still. No copied text, private logs, recording, or new instrumentation is needed. Nothing here asks for private clipboard contents.

## Source scope

- [Linux proxy entry](../../internal/cli/run/proxy_linux.go) and [proxy loop](../../internal/ttyproxy/ttyproxy.go): read at the immutable e20 commit named above; not evidence of the incident executable or backend.
- [Suspend-key handling](../../internal/ttyproxy/suspendkey.go): the e20 recognition and targeted suspend behavior; not proof that the incident used this route.
- [Current terminal-boundary verification](./current-terminal-boundary-verification-20261027.md): separate Linux TTY resume-boundary work and its limits; it is not Kitty certification or evidence of this incident's cause.
