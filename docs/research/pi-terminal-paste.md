---
title: "Pi terminal paste diagnostic fixture"
date: 2026-10-26
status: in-review
stage: SKETCH
next: "Capture unfiltered synthetic stdin at the next TTY boundary; do not infer a live-incident cause from this fixture"
tags: [research, pi, terminal, paste, tui, diagnostics]
summary: "A bounded offline fixture exercises Pi's installed bracketed-paste parser, editor, and both focused TUI renderers, plus the isolated clipboard-text caller, with synthetic data. Repeated 47-line blocks render through active mock-terminal TUI paths; this is mechanism evidence, not reproduction of a live terminal incident."
---

# Pi terminal paste diagnostic fixture

**Status:** 2026-10-26, evidence-only. No production fix or incident cause is established.

The reusable fixture is [`pi-paste-probe.mjs`](./fixtures/pi-paste-probe.mjs). It imports the installed Pi TUI parser, editor, and terminal modules by `--home` (defaulting to the actual `HOME`), and runs each case in a child with an 8-second wall deadline. It emits one sanitized JSON record: counters, hashes, source identity, and elapsed times; it never emits the generated text.

The focused repeated-block run from the repository root was:

```bash
node docs/research/fixtures/pi-paste-probe.mjs \
  --home "$HOME" \
  --evidence-dir "$YOLO_DURABLE_DIR/handoff-20261026/pi-paste-evidence" \
  > "$YOLO_DURABLE_DIR/handoff-20261026/pi-paste-evidence/pi-paste-probe-alt-screen-repaired.json"
```

The preceding six-case result remains separately preserved as `pi-paste-probe.json`. The 11-case corrected repeated run is `pi-paste-probe-repeated-confirmed.json`; the final 12-case run adds active alternate-screen rendering in `pi-paste-probe-alt-screen-repaired.json`.

## Finding

The installed bracketed-paste caller and editor path accepted the synthetic large paste as one event, called `onChange` once, did not submit, and kept a compact editor marker while preserving exact expanded text. The compiled interactive clipboard-text branch also inserted the same-size synthetic text exactly once through the terminal clipboard MIME reader and programmatic editor insertion.

This separates the observed code paths in the installed runtime; it does **not** establish why an earlier paste appeared frozen. Repeat tests use the real installed editor, one `TuiMainScreen` and an active `TuiAltScreen`, plus mock terminal writes and a passive input listener. They do not initialize `InteractiveMode`, an agent session, or the actual extension stack; no real terminal or backpressure is involved.

## What was exercised

| Path | Production behavior retained | Boundaries not exercised |
| :--- | :--- | :--- |
| Bracketed terminal paste and repeat renders | Actual `ProcessTerminal.setupStdinBuffer()` callbacks, fed through its production `stdinDataHandler`, forwarding into `Editor.handleInput()`; repeated cases dispatch through focused installed `TuiMainScreen` or active `TuiAltScreen` to a mock terminal writer. `TuiAltScreen` starts and stops only against the virtual terminal. | `ProcessTerminal.start()`, `process.stdin`, a real TTY/kernel writer, and InteractiveMode's full render/event loop |
| Interactive clipboard text | Exact installed `InteractiveMode.handleClipboardPaste()` method bytes in a minimal wrapper; the actual installed `readTerminalClipboard()` and its MIME decoder; installed editor insertion | InteractiveMode construction, session/resources/settings, real clipboard, native fallback clipboard reads, image-file creation, agent/API calls |
| Programmatic comparison | Installed editor's `insertTextAtCursor()` and synchronous `render()` at widths 80 and 200 | A claim that this comparison represents every unbracketed terminal input or clipboard behavior |

The clipboard method export keeps the installed method byte slice unchanged and adds only an import/class wrapper with throwing stubs for native fallback and image-file operations. Its provenance record distinguishes the complete source-file bytes, method bytes, wrapper/export bytes, and source hashes. The `runtimeBuildIdentity` is imported from the same matched `--home`; its compiled-input digest does not attest the whole dependency graph.

The exact method export and JSON provenance are saved outside the cleanup-managed worktree under the lane-owned durable directory. The compact JSON run results are beside them. The TUI constructors also receive this external directory for diagnostic logging; no intentional crash is induced. No installed package file was modified.

```text
$YOLO_DURABLE_DIR/handoff-20261026/pi-paste-evidence/
  pi-paste-probe.json
  pi-paste-probe-repeated.json
  pi-paste-probe-repeated-confirmed.json
  pi-paste-probe-alt-screen-confirmed.json
  pi-paste-probe-alt-screen-repaired.json
  pi-paste-interactive-method-<method-hash>.export.mjs
  pi-paste-interactive-method-<method-hash>.provenance.json
```

## Measured synthetic cases

Node v24.19.0 loaded `@earendil-works/pi-tui` 1.4.0. The generated multiline input is 200 ASCII lines, 57,199 characters/UTF-8 bytes, with 199 LF characters and no other control characters. The long one-line case is 57,000 ASCII characters. They are synthetic openat-like path text, not a copy or transformation of the observed trace.

The first six-case JSON remains unchanged at `pi-paste-probe.json`; the expanded evidence adds independently generated 47-line, 7,180-character blocks, eight of which expand to 57,440 characters. None of these inputs includes the observed trace payload.

The installed settings API declares fullscreen as the `getTuiMode` default, and `createInteractiveTui()` maps `fullscreen` to `TuiAltScreen`. The actual mode selected for the reported incident remains unknown.

- One-chunk, split-marker/split-content, and long-one-line bracketed cases each produced one parser `paste` event, one forwarded editor call, one `onChange`, no `onSubmit`, exact expanded editor text, and the expected compact marker. The 200-line marker was `[paste #1 +200 lines]`; the long one-line marker counted 57,000 characters. The final run's width-80/200 renders returned within 0.28/0.01 ms for the one-chunk case, 0.24/0.01 ms for split chunks, and 0.26/0.01 ms for the long line.
- The actual exported clipboard-text method selected one advertised `text/plain` MIME type, called the mock terminal read and `requestRender` once, inserted all 57,199 characters, and reported no error. Its post-insert editor renders measured 9.83/6.19 ms at widths 80/200; direct `insertTextAtCursor()` measured 12.00/6.59 ms. These synchronous measurements are not a benchmark or a prediction of interactive latency. A separate installed-source read found the default editor `onChange` handler only tracks `!` bash-mode changes and does not submit or request a model; the full InteractiveMode and extension stack were not constructed.
- The simple missing-terminator probe buffered the 200-line synthetic paste plus one ESC byte without parser `data` or `paste` events before the matching end marker. After that marker, one paste event arrived and the editor filtered the ESC. A separate read-only scout experiment reported Ctrl+C, Escape, and typed input suppressed after 80 ms while the bracketed paste remained unterminated and `flush()` did not recover it. These characterize parser behavior only; none establishes that a live incident had a missing terminator.
- A single 47-line, 7,180-character bracketed block produced one marker, one change, no submit, one TUI input-listener dispatch, and one focused MainScreen render.
- Eight independently bracketed 47-line blocks (57,440 characters expanded) produced eight paste events, eight changes, eight passive TUI listener calls, eight focused renders, and zero submits. The mock MainScreen made 16 terminal-write calls totaling 2,215 bytes, with eight synchronized-output begin/end pairs. Splitting both bracket markers and content for every block produced the same counts, frames, and output-byte total.
- In the batched comparison, all eight complete blocks arrived before the next event-loop turn. The parser/editor/listener counts stayed at eight, while the TUI coalesced output to one render, two writes, and one synchronized-output pair (812 bytes). These are mock-terminal writes that return immediately; they cannot represent kernel backpressure.
- In the active alternate-screen case, the virtual terminal captured `TuiAltScreen.start()`'s input callback; the separate production `ProcessTerminal.setupStdinBuffer()` feeder dispatched into it. After starting `TuiAltScreen` and rendering its initial active frame, eight independently bracketed blocks yielded eight more render frames and writes (1,824 bytes), with eight synchronized-output begin/end pairs. The TUI mock's `start` and `stop` each ran once; `ProcessTerminal.start()` and any real terminal did not. This exercises alternate-screen activation and rendering, not the runtime mode selected for the incident.
- The first expanded run, preserved as `pi-paste-probe-repeated.json`, failed only its contrived negative-control assertion: the fixture expected the unterminated paste to omit Enter, but the parser later delivered that buffered carriage return inside the paste and the editor normalized it to a trailing LF. The assertion was corrected to the observed editor result; this is a fixture expectation correction, not a production red/green result.
- The first active alternate-screen run, preserved as `pi-paste-probe-alt-screen-confirmed.json`, reached teardown after the render case but failed because the fixture finalized its output hash before `TuiAltScreen.stop()` emitted cleanup writes. The fixture now snapshots with `Hash.copy().digest()`; the separate safe debug JSON remains in the durable evidence directory. This was a harness teardown error, not a runtime freeze; the final 12-case output records all cases passed.
- The 7-complete-plus-8th-unterminated case is a **contrived lost-end-marker control, not an incident replay**. Before its late end marker, seven events and renders had completed; Ctrl+C and Enter generated no parser data event, input dispatch, or change. After the end marker, the eighth event arrived. The buffered Enter was treated as paste content and normalized to one trailing LF; there was still no submit. This characterizes parser behavior and does not establish that a real paste lost its delimiter.
- A separately reported independent synthetic comparison used 57,777 characters/201 lines and measured the full `ProcessTerminal` callback at 2.6 ms versus 15.7 ms for programmatic rendering. It is distinct from the current 57,199-character fixture run; retain it as supporting characterization, not as an incident reproduction.

All 12 final expanded cases completed under their 8-second child deadline. The child deadline bounds a hung mock; no live or OS-terminal input, loaded extension handler, model request, kernel write backpressure, or process-event-loop responsiveness sample was measured. A timeout would be reported as such, not attributed to a particular code path.

## Original trace limits

The original scratch strace capture was kept read-only and its payload was not replayed or included in this document. Its recorded SHA-256 is `07b2bbf9fe57e4d14cefb922395fdc18fd26d4dae8c7536d025de11bffde2649`. The file is a 443-line `strace -f` capture from a Claude process piped through an `open` grep, not a Pi terminal trace; it contains no Pi stdin/stdout read/write sequence, wait interval, or timestamp evidence. Thus it cannot establish a Pi paste-parser or render stall.

The bounded, payload-free recount supplied for this investigation reports 57,664 UTF-8 characters and 57,670 bytes; 443 LF-terminated lines; a longest line of 283 characters/bytes; two non-ASCII characters in the banner and no other control characters. It reports 130 lines up to 100 characters and 313 lines from 101 to 300 characters. Those shape counts do not identify clipboard bytes or prove that a terminal delivered a particular paste.

## Provenance and limits

The installed runtime reports version 1.4.0, origin `build`, build digest `b691448e8338d963f2c1daa13d35c92a83c0bb6a96e14a5262d5c51711212872`, `forkCommit: null`, and `dirty: null`. The launcher metadata supplied with this run identifies `earendil/main` and key `31ac79d2c1d33172`; this is distinct from the installed module identity below. The runtime identity is imported from the matched `--home` at execution time and recorded alongside byte hashes/paths in the external provenance JSON. The build digest describes compiled workspace inputs, not an attestation that a particular upstream commit or every transitive dependency produced the installed files. The fixture hashes the exact installed source/method/export bytes it uses so a later run can distinguish a changed artifact.

The scratch Pi pack metadata names `earendil-works/pi` on `main` and lists the source patches `0056` (editor paste fold) and `0085` (clipboard). The historical lockfile commit `d78e4f75…` is not this runtime's provenance. No host-owned patch source was modified or assumed writable.

The trace-derived size is only a synthetic shape guide. Synthetic inputs here are ASCII; the original trace has two banner characters outside ASCII. The fixture does not replay either trace payload or real credentials, start an interactive agent, make a paid model call, or attach to a real terminal.

## Interim workaround and next evidence boundary

Until the reported freeze is reproduced with its real input route, a cautious temporary workaround is to put the body in a file and supply the file path/reference through the normal workflow. This is not a product fix or a conclusion about which paste route was used.

The next useful evidence is an **unfiltered synthetic stdin capture at the Pi TTY boundary**, preserving chunk boundaries and timing while keeping the content synthetic. It should connect the production stdin feed to the parser and editor/render-request boundary without a model or extension. Only a reproduction at that boundary can support an incident-level cause; no caps, truncation, feature disablement, or parser changes follow from these passing characterization cases.

## Sources

The source evidence came from read-only inspection of the installed 1.4.0 artifacts and runtime metadata; no archive or source checkout was created. Source paths and byte identities are in the durable provenance JSON named above. The relevant production seams are:

```text
@earendil-works/pi-tui/dist/stdin-buffer.js: StdinBuffer.process(), paste event
@earendil-works/pi-tui/dist/terminal.js: ProcessTerminal.setupStdinBuffer(), stdinDataHandler
@earendil-works/pi-tui/dist/components/editor.js: Editor.handleInput(), handlePaste(), getExpandedText(), render()
@earendil-works/pi-tui/dist/tui.js: TuiBase focused input dispatch and render scheduling
@earendil-works/pi-tui/dist/tui-main-screen.js: TuiMainScreen render state and terminal writes
@earendil-works/pi-tui/dist/tui-alt-screen.js: TuiAltScreen activation, focused input dispatch, synchronized render, and stop cleanup
@earendil-works/pi-coding-agent/dist/modes/interactive/tui-renderer.js: createInteractiveTui() mode-to-renderer selection
@earendil-works/pi-coding-agent/dist/core/settings-defaults.d.ts: SettingsDefaults.tuiMode default
@earendil-works/pi-coding-agent/dist/modes/interactive/interactive-mode.js: handleClipboardPaste()
@earendil-works/pi-coding-agent/dist/utils/terminal-clipboard.js: readTerminalClipboard()
@earendil-works/pi-coding-agent/dist/utils/mime.js: detectSupportedImageMimeType()
```
