#!/usr/bin/env node
import { createHash } from "node:crypto";
import { spawnSync } from "node:child_process";
import { existsSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import os from "node:os";
import path from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import { performance } from "node:perf_hooks";

const START = "\x1b[200~";
const END = "\x1b[201~";
const CHILD_TIMEOUT_MS = 8_000;
const FAILURE_NEXT_ACTION = "Verify --home points to installed Pi artifacts, retry with a fresh durable --evidence-dir, and consult docs/research/pi-terminal-paste.md.";
let fixturePhase = "initialization";
const MULTILINE_LINE_COUNT = 200;
const LINE_LENGTH = 285;
const SCRIPT_PATH = fileURLToPath(import.meta.url);

function parseArgs(args) {
  let home = process.env.HOME || os.homedir();
  let evidenceDir = process.env.YOLO_DURABLE_DIR;
  let callerExportPath;
  let childCase;
  for (let i = 0; i < args.length; i += 1) {
    if (args[i] === "--home" && args[i + 1]) {
      home = args[++i];
    } else if (args[i] === "--evidence-dir" && args[i + 1]) {
      evidenceDir = args[++i];
    } else if (args[i] === "--caller-export" && args[i + 1]) {
      callerExportPath = args[++i];
    } else if (args[i] === "--case" && args[i + 1]) {
      childCase = args[++i];
    } else if (args[i] === "--child") {
      // The child role is selected by the other private fixture argument.
    } else {
      throw new Error("invalid arguments");
    }
  }
  if (!evidenceDir) throw new Error("evidence directory required");
  const durableRoot = process.env.YOLO_DURABLE_DIR && path.resolve(process.env.YOLO_DURABLE_DIR);
  evidenceDir = path.resolve(evidenceDir);
  if (durableRoot) {
    const relative = path.relative(durableRoot, evidenceDir);
    if (relative === ".." || relative.startsWith(`..${path.sep}`) || path.isAbsolute(relative)) {
      throw new Error("evidence directory must stay under YOLO_DURABLE_DIR");
    }
  }
  return { home: path.resolve(home), evidenceDir, callerExportPath: callerExportPath && path.resolve(callerExportPath), childCase };
}

function makeSyntheticLine(index, length = LINE_LENGTH) {
  const prefix = `/synthetic/openat/dir-${String(index).padStart(3, "0")}/entry-`;
  const filler = "0123456789abcdef".repeat(Math.ceil(length / 16));
  return (prefix + filler).slice(0, length);
}

function makeSyntheticInput() {
  const lines = Array.from({ length: MULTILINE_LINE_COUNT }, (_, index) => makeSyntheticLine(index));
  const multiline = lines.join("\n");
  return { multiline, longLine: lines.join("") };
}

function sha256(text) {
  return createHash("sha256").update(text, "utf8").digest("hex");
}

function sha256Bytes(bytes) {
  return createHash("sha256").update(bytes).digest("hex");
}

function writeOrVerify(filePath, bytes) {
  if (existsSync(filePath)) {
    assert(readFileSync(filePath).equals(bytes));
    return;
  }
  writeFileSync(filePath, bytes, { flag: "wx", mode: 0o600 });
}

async function exportClipboardMethod(home, evidenceDir) {
  mkdirSync(evidenceDir, { recursive: true, mode: 0o700 });
  const agentRoot = path.join(home, ".local", "share", "pi-fork", "node_modules", "@earendil-works", "pi-coding-agent");
  const sourcePath = path.join(agentRoot, "dist", "modes", "interactive", "interactive-mode.js");
  const sourceBytes = readFileSync(sourcePath);
  const startText = Buffer.from("    async handleClipboardPaste() {");
  const endText = Buffer.from("    handleStartupSubmit(");
  const methodStart = sourceBytes.indexOf(startText);
  const methodEnd = sourceBytes.indexOf(endText, methodStart);
  assert(methodStart >= 0 && methodEnd > methodStart);
  const methodBytes = sourceBytes.subarray(methodStart, methodEnd);
  const methodHash = sha256Bytes(methodBytes);
  const exportName = `pi-paste-interactive-method-${methodHash.slice(0, 12)}`;
  const terminalClipboardPath = path.join(agentRoot, "dist", "utils", "terminal-clipboard.js");
  const runtimeBuildIdentityPath = path.join(agentRoot, "dist", "core", "runtime-build.js");
  const { runtimeBuildIdentity } = await import(pathToFileURL(runtimeBuildIdentityPath));
  const prelude = Buffer.from([
    `import { readTerminalClipboard } from ${JSON.stringify(pathToFileURL(terminalClipboardPath).href)};`,
    "const crypto = { randomUUID() { throw new Error('image path blocked'); } };",
    "const fs = { writeFileSync() { throw new Error('image file write blocked'); } };",
    "const os = { tmpdir() { throw new Error('image temp path blocked'); } };",
    "const path = { join() { throw new Error('image path join blocked'); } };",
    "const extensionForImageMimeType = () => { throw new Error('image MIME helper blocked'); };",
    "const readClipboardFilePaths = async () => { throw new Error('native file-path clipboard blocked'); };",
    "const readClipboardImage = async () => { throw new Error('native image clipboard blocked'); };",
    "const readClipboardText = async () => { throw new Error('native text clipboard blocked'); };",
    "const quoteIfNeeded = () => { throw new Error('native file-path quoting blocked'); };",
    "export class IsolatedInteractiveModeClipboardMethod {",
    "  constructor({ editor, ui }) {",
    "    this.editor = editor;",
    "    this.ui = ui;",
    "    this.clipboardPasteInFlight = false;",
    "    this.isShuttingDown = false;",
    "    this.shutdownRequested = false;",
    "    this.errors = [];",
    "  }",
  ].join("\n") + "\n");
  const suffix = Buffer.from([
    "  showStatus(message) { this.status = message; }",
    "  showError(error) { this.errors.push(error instanceof Error ? error.name : typeof error); }",
    "}\n",
  ].join("\n"));
  const exportBytes = Buffer.concat([prelude, methodBytes, suffix]);
  const exportPath = path.join(evidenceDir, `${exportName}.export.mjs`);
  writeOrVerify(exportPath, exportBytes);
  const metadata = {
    sourcePath,
    sourceBytes: sourceBytes.length,
    sourceSha256: sha256Bytes(sourceBytes),
    methodBoundaryStart: "    async handleClipboardPaste() {",
    methodBoundaryEndExclusive: "    handleStartupSubmit(",
    methodStartLine: sourceBytes.subarray(0, methodStart).toString("utf8").split("\n").length,
    nextMethodLine: sourceBytes.subarray(0, methodEnd).toString("utf8").split("\n").length,
    methodBytes: methodBytes.length,
    methodSha256: methodHash,
    exportPath,
    exportBytes: exportBytes.length,
    exportSha256: sha256Bytes(exportBytes),
    importedClipboardReader: terminalClipboardPath,
    clipboardReaderDependency: "utils/mime.js only",
    runtimeBuildIdentityPath,
    runtimeBuildIdentity,
    installedVersion: JSON.parse(readFileSync(path.join(agentRoot, "package.json"), "utf8")).version,
    tuiTerminalPath: path.join(home, ".local", "share", "pi-fork", "node_modules", "@earendil-works", "pi-tui", "dist", "terminal.js"),
    tuiEditorPath: path.join(home, ".local", "share", "pi-fork", "node_modules", "@earendil-works", "pi-tui", "dist", "components", "editor.js"),
    tuiPackageVersion: JSON.parse(readFileSync(path.join(home, ".local", "share", "pi-fork", "node_modules", "@earendil-works", "pi-tui", "package.json"), "utf8")).version,
    wrapperNote: "Method byte slice is unchanged; only class/import/mock-dependency wrapper bytes are added.",
  };
  const metadataBytes = Buffer.from(`${JSON.stringify(metadata, null, 2)}\n`);
  const metadataPath = path.join(evidenceDir, `${exportName}.provenance.json`);
  writeOrVerify(metadataPath, metadataBytes);
  return {
    sourcePath,
    sourceBytes: sourceBytes.length,
    sourceSha256: sha256Bytes(sourceBytes),
    methodStartLine: metadata.methodStartLine,
    nextMethodLine: metadata.nextMethodLine,
    methodBytes: methodBytes.length,
    methodSha256: methodHash,
    exportPath,
    metadataPath,
    exportBytes: exportBytes.length,
    exportSha256: sha256Bytes(exportBytes),
    runtimeBuildIdentityPath,
    runtimeBuildIdentity,
  };
}

function roundedMs(value) {
  return Number(value.toFixed(2));
}

function assert(condition, failureCheck = "assertion") {
  if (!condition) throw new Error(`fixture-assertion:${failureCheck}`);
}

function makeEditor(Editor, tui) {
  const identity = (text) => text;
  const theme = {
    borderColor: identity,
    selectList: {
      selectedPrefix: identity,
      selectedText: identity,
      description: identity,
      scrollInfo: identity,
      noMatch: identity,
    },
  };
  const editorTui = tui || {
    terminal: { rows: 24 },
    requestRender() {},
  };
  return new Editor(editorTui, theme);
}

function makeMockTui(Tui, evidenceDir) {
  let outputHash = createHash("sha256");
  let terminalInputHandler;
  const stats = {
    writeCalls: 0,
    outputBytes: 0,
    inputListenerCalls: 0,
    syncBeginCount: 0,
    syncEndCount: 0,
    terminalStartCalls: 0,
    terminalStopCalls: 0,
  };
  const terminal = {
    columns: 80,
    rows: 24,
    start(onInput) {
      stats.terminalStartCalls += 1;
      terminalInputHandler = onInput;
    },
    stop() {
      stats.terminalStopCalls += 1;
    },
    write(data) {
      stats.writeCalls += 1;
      stats.outputBytes += Buffer.byteLength(data);
      stats.syncBeginCount += data.split(`${String.fromCharCode(27)}[?2026h`).length - 1;
      stats.syncEndCount += data.split(`${String.fromCharCode(27)}[?2026l`).length - 1;
      outputHash.update(data, "utf8");
    },
    hideCursor() {},
    showCursor() {},
  };
  const tui = new Tui(terminal, false, evidenceDir);
  const removeInputListener = tui.addInputListener(() => {
    stats.inputListenerCalls += 1;
  });
  return {
    tui,
    terminal,
    stats,
    get outputHash() {
      return outputHash;
    },
    resetOutputHash() {
      outputHash = createHash("sha256");
    },
    dispatchInput(sequence) {
      if (terminalInputHandler) terminalInputHandler(sequence);
      else tui.dispatchTerminalInput(sequence);
    },
    removeInputListener,
  };
}

function watchEditorRender(editor) {
  const measurements = [];
  const render = editor.render.bind(editor);
  editor.render = (width) => {
    const started = performance.now();
    const lines = render(width);
    measurements.push({ width, elapsedMs: roundedMs(performance.now() - started), renderedLines: lines.length });
    return lines;
  };
  return measurements;
}

function makeSyntheticBlock(blockIndex) {
  return Array.from({ length: 47 }, (_, index) => {
    const lineLength = index < 37 ? 152 : 151;
    return makeSyntheticLine(200 + blockIndex * 47 + index, lineLength);
  }).join("\n");
}

async function nextTurn() {
  await new Promise((resolve) => setImmediate(resolve));
}

function makeProductionInputPath(ProcessTerminal, editor, dispatchInput = (sequence) => editor.handleInput(sequence)) {
  const counts = {
    dataEvents: 0,
    pasteEvents: 0,
    forwardedInputCalls: 0,
    onChange: 0,
    onSubmit: 0,
  };
  editor.onChange = () => {
    counts.onChange += 1;
  };
  editor.onSubmit = () => {
    counts.onSubmit += 1;
  };

  const terminal = new ProcessTerminal();
  terminal.inputHandler = (sequence) => {
    counts.forwardedInputCalls += 1;
    dispatchInput(sequence);
  };
  // This is the production parser-to-editor wiring. Do not call start(): it
  // attaches process.stdin and configures a real terminal.
  terminal.setupStdinBuffer();
  terminal.stdinBuffer.on("data", () => {
    counts.dataEvents += 1;
  });
  terminal.stdinBuffer.on("paste", () => {
    counts.pasteEvents += 1;
  });
  assert(typeof terminal.stdinDataHandler === "function");
  return { terminal, counts };
}

function renderWidths(editor) {
  return [80, 200].map((width) => {
    const started = performance.now();
    const rendered = editor.render(width);
    const elapsedMs = roundedMs(performance.now() - started);
    assert(Array.isArray(rendered) && rendered.length > 0);
    return { width, elapsedMs, renderedLines: rendered.length };
  });
}

function summarizeText(editor, expected, expectedCompactMarker) {
  const compact = editor.getText();
  const expanded = editor.getExpandedText();
  return {
    compactChars: compact.length,
    compactSha256: sha256(compact),
    compactMarkerMatches: compact === expectedCompactMarker,
    expandedChars: expanded.length,
    expandedSha256: sha256(expanded),
    expandedMatchesSyntheticInput: expanded === expected,
  };
}

async function runChild(home, evidenceDir, callerExportPath, caseName) {
  fixturePhase = "child-imports";
  const packageRoot = path.join(home, ".local", "share", "pi-fork", "node_modules", "@earendil-works", "pi-tui");
  const packageMetadata = JSON.parse(readFileSync(path.join(packageRoot, "package.json"), "utf8"));
  const dist = path.join(packageRoot, "dist");
  const callerExport = caseName === "actual-clipboard-text"
    ? await import(pathToFileURL(callerExportPath).href)
    : undefined;
  const isAltScreenCase = caseName === "repeat-paste-eight-47-line-blocks-alt-screen";
  const [{ ProcessTerminal }, { Editor }, { TuiAltScreen }, { TuiMainScreen }] = await Promise.all([
    import(pathToFileURL(path.join(dist, "terminal.js"))),
    import(pathToFileURL(path.join(dist, "components", "editor.js"))),
    import(pathToFileURL(path.join(dist, "tui-alt-screen.js"))),
    import(pathToFileURL(path.join(dist, "tui-main-screen.js"))),
  ]);
  const { multiline, longLine } = makeSyntheticInput();
  const isRepeatedCase = caseName.startsWith("repeat-paste-");
  const mockTui = isRepeatedCase ? makeMockTui(isAltScreenCase ? TuiAltScreen : TuiMainScreen, evidenceDir) : undefined;
  const editor = makeEditor(Editor, mockTui?.tui);
  const renderMeasurements = mockTui ? watchEditorRender(editor) : undefined;
  if (mockTui) {
    mockTui.tui.addChild(editor);
    mockTui.tui.setFocus(editor);
  }
  const tuiOutputBaseline = { writeCalls: 0, outputBytes: 0, syncBeginCount: 0, syncEndCount: 0 };
  if (isAltScreenCase) {
    fixturePhase = "alt-screen-start";
    mockTui.tui.start();
    fixturePhase = "alt-screen-initial-render";
    mockTui.tui.renderNow();
    assert(renderMeasurements.length > 0, "alt-screen-initial-render");
    renderMeasurements.length = 0;
    tuiOutputBaseline.writeCalls = mockTui.stats.writeCalls;
    tuiOutputBaseline.outputBytes = mockTui.stats.outputBytes;
    tuiOutputBaseline.syncBeginCount = mockTui.stats.syncBeginCount;
    tuiOutputBaseline.syncEndCount = mockTui.stats.syncEndCount;
    mockTui.resetOutputHash();
  }
  const dispatchInput = mockTui ? (sequence) => mockTui.dispatchInput(sequence) : undefined;
  const { terminal: inputTerminal, counts } = makeProductionInputPath(ProcessTerminal, editor, dispatchInput);
  const feed = (chunk) => inputTerminal.stdinDataHandler(chunk);
  const repeatedCount = caseName === "repeat-paste-one-47-line-block" ? 1 : 8;
  const repeatedBlocks = isRepeatedCase
    ? Array.from({ length: repeatedCount }, (_, index) => makeSyntheticBlock(index))
    : [];
  const repeatedText = repeatedBlocks.join("");
  const expected = caseName === "bracketed-long-one-line" ? longLine : (isRepeatedCase ? repeatedText : multiline);
  const started = performance.now();
  let text;
  let renders = [];
  let extra = {};

  switch (caseName) {
    case "bracketed-one-chunk":
      feed(`${START}${multiline}${END}`);
      break;
    case "bracketed-split-markers-and-chunks":
      feed(START.slice(0, 3));
      feed(START.slice(3));
      feed(multiline.slice(0, Math.floor(multiline.length / 2)));
      feed(multiline.slice(Math.floor(multiline.length / 2)));
      feed(END.slice(0, 3));
      feed(END.slice(3));
      break;
    case "bracketed-long-one-line":
      feed(`${START}${longLine}${END}`);
      break;
    case "bracketed-missing-terminator": {
      feed(`${START}${multiline}`);
      feed("\x1b");
      const beforeTerminator = { ...counts };
      feed(END);
      extra.beforeTerminator = beforeTerminator;
      break;
    }
    case "repeat-paste-one-47-line-block":
    case "repeat-paste-eight-47-line-blocks":
    case "repeat-paste-eight-47-line-blocks-split":
    case "repeat-paste-eight-47-line-blocks-batched":
    case "repeat-paste-eight-47-line-blocks-alt-screen": {
      const frames = [];
      const feedBlock = (block, split) => {
        const chunks = split
          ? [START.slice(0, 3), START.slice(3), block.slice(0, Math.floor(block.length / 2)), block.slice(Math.floor(block.length / 2)), END.slice(0, 3), END.slice(3)]
          : [`${START}${block}${END}`];
        for (const chunk of chunks) feed(chunk);
      };
      if (caseName.endsWith("-batched")) {
        fixturePhase = "repeated-batched-feed";
        for (const block of repeatedBlocks) feedBlock(block, false);
        await nextTurn();
        frames.push({
          renderCalls: renderMeasurements.length,
          renderMs: renderMeasurements.reduce((total, render) => total + render.elapsedMs, 0),
          terminalWriteCalls: mockTui.stats.writeCalls,
        });
      } else {
        for (const block of repeatedBlocks) {
          fixturePhase = isAltScreenCase ? "alt-screen-block-feed" : "repeated-block-feed";
          const renderStart = renderMeasurements.length;
          const writesStart = mockTui.stats.writeCalls;
          const listenerStart = mockTui.stats.inputListenerCalls;
          feedBlock(block, caseName.endsWith("-split"));
          await nextTurn();
          const blockRenders = renderMeasurements.slice(renderStart);
          const renderMs = blockRenders.reduce((total, render) => total + render.elapsedMs, 0);
          assert(blockRenders.length > 0, isAltScreenCase ? "alt-screen-paste-render" : "paste-render");
          assert(mockTui.stats.writeCalls > writesStart, isAltScreenCase ? "alt-screen-paste-write" : "paste-write");
          assert(mockTui.stats.inputListenerCalls === listenerStart + 1, isAltScreenCase ? "alt-screen-input-listener" : "paste-input-listener");
          frames.push({
            renderCalls: blockRenders.length,
            renderMs,
            terminalWriteCalls: mockTui.stats.writeCalls - writesStart,
          });
        }
      }
      fixturePhase = isAltScreenCase ? "alt-screen-result-assertions" : "repeated-result-assertions";
      const markers = editor.getText().match(/\[paste #\d+ \+47 lines\]/g) || [];
      assert(markers.length === repeatedCount);
      assert(markers.every((marker, index) => marker === `[paste #${index + 1} +47 lines]`));
      assert(counts.pasteEvents === repeatedCount);
      assert(counts.forwardedInputCalls === repeatedCount);
      assert(counts.onChange === repeatedCount);
      assert(counts.onSubmit === 0);
      assert(mockTui.stats.inputListenerCalls === repeatedCount);
      const repeatedWriteCalls = mockTui.stats.writeCalls - tuiOutputBaseline.writeCalls;
      const repeatedOutputBytes = mockTui.stats.outputBytes - tuiOutputBaseline.outputBytes;
      const repeatedSyncBegins = mockTui.stats.syncBeginCount - tuiOutputBaseline.syncBeginCount;
      const repeatedSyncEnds = mockTui.stats.syncEndCount - tuiOutputBaseline.syncEndCount;
      assert(repeatedWriteCalls > 0, isAltScreenCase ? "alt-screen-repeated-output" : "repeated-output");
      assert(repeatedSyncBegins === repeatedSyncEnds, isAltScreenCase ? "alt-screen-sync-pair" : "repeated-sync-pair");
      if (isAltScreenCase) {
        assert(repeatedCount === 8, "alt-screen-eight-blocks");
        assert(frames.length === 8 && frames.every((frame) => frame.renderCalls > 0), "alt-screen-eight-render-frames");
        assert(mockTui.stats.terminalStartCalls === 1, "alt-screen-terminal-start");
        assert(mockTui.stats.terminalStopCalls === 0, "alt-screen-not-stopped-before-evidence");
        extra.altScreen = {
          mode: "TuiAltScreen",
          terminalStartCalls: mockTui.stats.terminalStartCalls,
          terminalStopCallsBeforeCleanup: mockTui.stats.terminalStopCalls,
          blocksWithTuiRenderFrames: frames.filter((frame) => frame.renderCalls > 0).length,
          realTerminalAttached: false,
          processTerminalStartCalled: false,
        };
      }
      assert(editor.getExpandedText() === repeatedText);
      text = {
        compactChars: editor.getText().length,
        compactSha256: sha256(editor.getText()),
        compactMarkerCount: markers.length,
        expandedChars: editor.getExpandedText().length,
        expandedSha256: sha256(editor.getExpandedText()),
        expandedMatchesSyntheticInput: true,
      };
      renders = renderMeasurements;
      extra.repeated = {
        blocks: repeatedCount,
        linesPerBlock: 47,
        charactersPerBlock: repeatedBlocks[0].length,
        expandedCharacters: repeatedText.length,
        renderScheduling: caseName.endsWith("-batched") ? "batched-before-next-turn" : "await-next-turn-after-each-block",
        frames,
        passiveTuiInputListenerCalls: mockTui.stats.inputListenerCalls,
        totalTerminalWriteCalls: repeatedWriteCalls,
        terminalOutputBytes: repeatedOutputBytes,
        synchronizedOutputBegins: repeatedSyncBegins,
        synchronizedOutputEnds: repeatedSyncEnds,
        realTerminalAttached: false,
        realExtensionListenersLoaded: false,
        mockTerminalBackpressure: false,
      };
      break;
    }
    case "repeat-paste-seven-plus-final-unterminated": {
      for (const block of repeatedBlocks.slice(0, 7)) {
        feed(`${START}${block}${END}`);
        await nextTurn();
      }
      const eighthBlock = repeatedBlocks[7];
      feed(`${START}${eighthBlock}`);
      feed(String.fromCharCode(3, 13));
      await nextTurn();
      const beforeLateEnd = { ...counts };
      const rendersBeforeLateEnd = renderMeasurements.length;
      feed(END);
      await nextTurn();
      extra.unterminatedNegativeControl = {
        beforeLateEnd,
        editorRenderCallsBeforeLateEnd: rendersBeforeLateEnd,
        label: "contrived-lost-end-marker-control; not incident replay",
      };
      break;
    }
    case "actual-clipboard-text": {
      const terminalReads = [];
      let renderRequests = 0;
      const ui = {
        terminal: {
          async readClipboard(selectTypes) {
            terminalReads.push(selectTypes(["text/plain"]));
            return new Map([["text/plain", Buffer.from(multiline, "utf8")]]);
          },
        },
        requestRender() {
          renderRequests += 1;
        },
      };
      const mode = new callerExport.IsolatedInteractiveModeClipboardMethod({ editor, ui });
      await mode.handleClipboardPaste();
      assert(editor.getText() === multiline);
      assert(editor.getExpandedText() === multiline);
      assert(counts.onChange === 1);
      assert(counts.onSubmit === 0);
      assert(terminalReads.length === 1);
      assert(terminalReads[0].length === 1 && terminalReads[0][0] === "text/plain");
      assert(renderRequests === 1);
      assert(mode.errors.length === 0);
      assert(mode.clipboardPasteInFlight === false);
      extra.clipboard = {
        terminalReadCalls: terminalReads.length,
        textMimeSelected: true,
        requestedMimeCount: terminalReads[0].length,
        requestRenderCalls: renderRequests,
        reportedErrors: mode.errors.length,
        clipboardPasteInFlight: mode.clipboardPasteInFlight,
        realClipboardAttached: false,
        externalApiCalls: 0,
      };
      break;
    }
    case "direct-insert-multiline":
      editor.insertTextAtCursor(multiline);
      break;
    default:
      throw new Error("unknown fixture case");
  }

  const elapsedMs = roundedMs(performance.now() - started);
  const expectedMarker = caseName === "bracketed-long-one-line"
    ? `[paste #1 ${longLine.length} chars]`
    : `[paste #1 +${MULTILINE_LINE_COUNT} lines]`;

  if (isRepeatedCase) {
    if (caseName === "repeat-paste-seven-plus-final-unterminated") {
      const beforeLateEnd = extra.unterminatedNegativeControl.beforeLateEnd;
      const markers = editor.getText().match(/\[paste #\d+ \+\d+ lines\]/g) || [];
      const expectedAfterBufferedEnter = `${repeatedText}\n`;
      assert(beforeLateEnd.pasteEvents === 7);
      assert(beforeLateEnd.onChange === 7);
      assert(beforeLateEnd.dataEvents === 0);
      assert(beforeLateEnd.forwardedInputCalls === 7);
      assert(counts.pasteEvents === 8);
      assert(counts.forwardedInputCalls === 8);
      assert(counts.onChange === 8);
      assert(counts.onSubmit === 0);
      assert(mockTui.stats.inputListenerCalls === 8);
      assert(mockTui.stats.syncBeginCount === mockTui.stats.syncEndCount);
      assert(markers.length === 8);
      assert(markers.slice(0, 7).every((marker, index) => marker === `[paste #${index + 1} +47 lines]`));
      assert(markers[7] === "[paste #8 +48 lines]");
      assert(editor.getExpandedText() === expectedAfterBufferedEnter);
      text = {
        compactChars: editor.getText().length,
        compactSha256: sha256(editor.getText()),
        compactMarkerCount: markers.length,
        expandedChars: editor.getExpandedText().length,
        expandedSha256: sha256(editor.getExpandedText()),
        matchesExpectedAfterBufferedEnter: true,
      };
      extra.unterminatedNegativeControl.afterLateEnd = { ...counts };
      extra.unterminatedNegativeControl.label = "contrived-lost-end-marker-control; not incident replay";
      extra.unterminatedNegativeControl.realTerminalAttached = false;
    }
    renders = renderMeasurements;
    extra.repeatedTui = {
      terminalWriteCalls: mockTui.stats.writeCalls - tuiOutputBaseline.writeCalls,
      terminalOutputBytes: mockTui.stats.outputBytes - tuiOutputBaseline.outputBytes,
      synchronizedOutputBegins: mockTui.stats.syncBeginCount - tuiOutputBaseline.syncBeginCount,
      synchronizedOutputEnds: mockTui.stats.syncEndCount - tuiOutputBaseline.syncEndCount,
      terminalOutputSha256: mockTui.outputHash.copy().digest("hex"),
      passiveTuiInputListenerCalls: mockTui.stats.inputListenerCalls,
      realTerminalAttached: false,
      realExtensionListenersLoaded: false,
      mockTerminalBackpressure: false,
    };
  } else if (caseName === "bracketed-missing-terminator") {
    assert(extra.beforeTerminator.pasteEvents === 0);
    assert(extra.beforeTerminator.dataEvents === 0);
    assert(extra.beforeTerminator.forwardedInputCalls === 0);
    assert(extra.beforeTerminator.onChange === 0);
    assert(counts.pasteEvents === 1);
    assert(counts.forwardedInputCalls === 1);
    assert(counts.onChange === 1);
    assert(counts.onSubmit === 0);
    text = summarizeText(editor, expected, expectedMarker);
    assert(text.compactMarkerMatches && text.expandedMatchesSyntheticInput);
    renders = renderWidths(editor);
  } else if (caseName === "direct-insert-multiline" || caseName === "actual-clipboard-text") {
    assert(editor.getText() === multiline);
    assert(editor.getExpandedText() === multiline);
    assert(counts.pasteEvents === 0);
    assert(counts.forwardedInputCalls === 0);
    assert(counts.onChange === 1);
    assert(counts.onSubmit === 0);
    text = {
      compactChars: editor.getText().length,
      compactSha256: sha256(editor.getText()),
      expandedChars: editor.getExpandedText().length,
      expandedSha256: sha256(editor.getExpandedText()),
      expandedMatchesSyntheticInput: true,
    };
    renders = renderWidths(editor);
  } else {
    assert(counts.pasteEvents === 1);
    assert(counts.forwardedInputCalls === 1);
    assert(counts.onChange === 1);
    assert(counts.onSubmit === 0);
    text = summarizeText(editor, expected, expectedMarker);
    assert(text.compactMarkerMatches && text.expandedMatchesSyntheticInput);
    renders = renderWidths(editor);
  }

  fixturePhase = "input-buffer-destroy";
  inputTerminal.stdinBuffer.destroy();
  if (mockTui) {
    if (isAltScreenCase) {
      fixturePhase = "alt-screen-stop";
      mockTui.tui.stop({ preserveScreen: true });
      fixturePhase = "alt-screen-stop-assert";
      assert(mockTui.stats.terminalStopCalls === 1, "alt-screen-terminal-stop");
      extra.altScreen.terminalStopCallsAfterCleanup = mockTui.stats.terminalStopCalls;
    } else {
      fixturePhase = "main-screen-stop";
      mockTui.tui.stopped = true;
      mockTui.tui.cancelRenderTimer();
    }
    fixturePhase = "input-listener-remove";
    mockTui.removeInputListener();
  }
  fixturePhase = "complete";
  return {
    case: caseName,
    status: "passed",
    elapsedMs,
    counts,
    text,
    renders,
    ...extra,
  };
}

async function runParent(home, evidenceDir) {
  const packageRoot = path.join(home, ".local", "share", "pi-fork", "node_modules", "@earendil-works", "pi-tui");
  const packageMetadata = JSON.parse(readFileSync(path.join(packageRoot, "package.json"), "utf8"));
  const callerExport = await exportClipboardMethod(home, evidenceDir);
  const input = makeSyntheticInput();
  const cases = [
    "bracketed-one-chunk",
    "bracketed-split-markers-and-chunks",
    "bracketed-long-one-line",
    "bracketed-missing-terminator",
    "actual-clipboard-text",
    "direct-insert-multiline",
    "repeat-paste-one-47-line-block",
    "repeat-paste-eight-47-line-blocks",
    "repeat-paste-eight-47-line-blocks-split",
    "repeat-paste-eight-47-line-blocks-batched",
    "repeat-paste-eight-47-line-blocks-alt-screen",
    "repeat-paste-seven-plus-final-unterminated",
  ];
  const results = cases.map((caseName) => {
    const started = performance.now();
    const childArgs = [SCRIPT_PATH, "--child", "--case", caseName, "--home", home, "--evidence-dir", evidenceDir];
    if (caseName === "actual-clipboard-text") childArgs.push("--caller-export", callerExport.exportPath);
    const child = spawnSync(process.execPath, childArgs, {
      encoding: "utf8",
      timeout: CHILD_TIMEOUT_MS,
      maxBuffer: 32 * 1024,
    });
    const elapsedMs = roundedMs(performance.now() - started);
    if (child.error?.code === "ETIMEDOUT") {
      return { case: caseName, status: "timeout", timeoutMs: CHILD_TIMEOUT_MS, elapsedMs, nextAction: FAILURE_NEXT_ACTION };
    }
    if (child.error || child.status !== 0) {
      return {
        case: caseName,
        status: "failed",
        exitCode: child.status,
        signal: child.signal,
        elapsedMs,
        nextAction: FAILURE_NEXT_ACTION,
      };
    }
    try {
      const result = JSON.parse(child.stdout);
      return { ...result, childWallMs: elapsedMs };
    } catch {
      return { case: caseName, status: "invalid-child-output", elapsedMs, nextAction: FAILURE_NEXT_ACTION };
    }
  });
  const requiredFailures = results.filter((result) =>
    result.status !== "passed" && result.case !== "direct-insert-multiline"
  );
  const lines = input.multiline.split("\n");
  const printable = `${input.multiline}${input.longLine}`;
  const nonNewlineControls = [...printable].filter((character) => {
    const code = character.charCodeAt(0);
    return (code < 32 && character !== "\n") || code === 127;
  }).length;
  const output = {
    fixture: "pi-terminal-paste-probe",
    mode: "offline-synthetic-mock-editor; no live terminal or extension/model execution",
    nodeVersion: process.version,
    piTuiVersion: packageMetadata.version,
    synthetic: {
      multilineLines: lines.length,
      multilineCharacters: input.multiline.length,
      multilineUtf8Bytes: Buffer.byteLength(input.multiline),
      multilineLineFeeds: (input.multiline.match(/\n/g) || []).length,
      longLineCharacters: input.longLine.length,
      nonNewlineControlCharacters: nonNewlineControls,
      multilineSha256: sha256(input.multiline),
      longLineSha256: sha256(input.longLine),
    },
    callerMethodExport: callerExport,
    perChildDeadlineMs: CHILD_TIMEOUT_MS,
    cases: results,
    requiredCasesPassed: requiredFailures.length === 0,
  };
  // Keep stdout to one JSON record and never emit generated text or child stderr.
  process.stdout.write(`${JSON.stringify(output)}\n`);
  if (requiredFailures.length > 0) process.exitCode = 1;
}

try {
  const { home, evidenceDir, callerExportPath, childCase } = parseArgs(process.argv.slice(2));
  if (process.argv.includes("--child")) {
    if (!childCase) throw new Error("missing case");
    const result = await runChild(home, evidenceDir, callerExportPath, childCase);
    process.stdout.write(JSON.stringify(result));
  } else {
    await runParent(home, evidenceDir);
  }
} catch (error) {
  const failureCheck = error instanceof Error && error.message.startsWith("fixture-assertion:")
    ? error.message.slice("fixture-assertion:".length)
    : undefined;
  process.stdout.write(`${JSON.stringify({ fixture: "pi-terminal-paste-probe", status: "failed", error: "fixture-execution-failed", ...(failureCheck ? { failureCheck } : {}), failurePhase: fixturePhase, nextAction: FAILURE_NEXT_ACTION })}\n`);
  process.exitCode = 1;
}
