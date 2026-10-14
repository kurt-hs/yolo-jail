package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/mschulkind-oss/yolo-jail/internal/capture"
)

type programCaptureDiagnosticFixture struct {
	Chunks      [][]byte
	FD          int
	Workspace   string
	Resolution  []byte
	Success     bool
	OtherChunks map[int][][]byte
}

func init() {
	if len(os.Args) != 3 || os.Args[1] != "program-capture-diagnostics-fixture" {
		return
	}
	raw, err := os.ReadFile(os.Args[2])
	if err != nil {
		os.Exit(91)
	}
	var fixture programCaptureDiagnosticFixture
	if json.Unmarshal(raw, &fixture) != nil {
		os.Exit(92)
	}
	out := os.NewFile(uintptr(fixture.FD), "owned-fixture-output")
	for _, chunk := range fixture.Chunks {
		if _, err := out.Write(chunk); err != nil {
			os.Exit(93)
		}
	}
	for fd, chunks := range fixture.OtherChunks {
		other := os.NewFile(uintptr(fd), "owned-fixture-other-output")
		for _, chunk := range chunks {
			if _, err := other.Write(chunk); err != nil {
				os.Exit(93)
			}
		}
	}
	if fixture.Resolution != nil {
		if os.WriteFile(filepath.Join(fixture.Workspace, "resolution.json"), fixture.Resolution, 0600) != nil || writeForkBuildRunReturned(fixture.Workspace) != nil {
			os.Exit(94)
		}
		os.Exit(0)
	}
	os.Exit(37)
}

func programCaptureDiagnosticCases() []struct {
	name      string
	chunks    []string
	forbidden []string
	useful    string
} {
	return []struct {
		name      string
		chunks    []string
		forbidden []string
		useful    string
	}{
		{"marker-outside-tail", []string{"token=" + strings.Repeat("fake-secret-", 100) + "\nTLS verification failed\n"}, []string{"fake-secret-"}, "TLS verification failed"},
		{"split-credentials", []string{"to", "ken", "=fake-", "token-value\nAuth", "orization: Bea", "rer fake-bearer-value\nhttps://fake-user:fake-password@registry.invalid/path?to", "ken=fake-query-value&ok=yes\nTLS verification failed\n"}, []string{"fake-token-value", "fake-bearer-value", "fake-user", "fake-password", "fake-query-value"}, "TLS verification failed"},
		{"terminal-sequences", []string{"\x1b]52;c;fake-osc\a\x1b[31mTLS\x1b[0m verification failed\n\x1bPfake-dcs\x1b\\\x1b_fake-apc\x1b\\\x1b^fake-pm\x1b\\\x1bXfake-sos\x1b\\\x07\b\x00\x01\x7f\n"}, []string{"fake-osc", "fake-dcs", "fake-apc", "fake-pm", "fake-sos"}, "TLS verification failed"},
		{"split-terminal-and-utf8", []string{"\x1b", "]52;c;fake-osc", "\x1b", "\\TLS verification failed: caf\xc3", "\xa9\n"}, []string{"fake-osc"}, "café"},
		{"incomplete-escape", []string{"TLS verification failed\n\x1b]52;c;fake-incomplete"}, []string{"fake-incomplete"}, "TLS verification failed"},
		{"incomplete-csi", []string{"TLS verification failed\n\x1b[123;"}, []string{"123;"}, "TLS verification failed"},
		{"giant-no-newline", []string{"password=" + strings.Repeat("fake-giant-secret", 1<<16), "\nTLS verification failed\n"}, []string{"fake-giant-secret"}, "TLS verification failed"},
		{"c1-terminal-sequences", []string{"\u009d52;c;fake-c1-osc\u009c\u0090fake-c1-dcs\u009c\u009ffake-c1-apc\u009c\u009b31mTLS verification failed\u009b0m\n"}, []string{"fake-c1-osc", "fake-c1-dcs", "fake-c1-apc"}, "TLS verification failed"},
		{"raw-c1-terminal-sequences", []string{"\x9d52;c;fake-c1-osc\x9c\x9b31mTLS verification failed\x9b0m\n"}, []string{"fake-c1-osc"}, "TLS verification failed"},
		{"incomplete-dcs", []string{"TLS verification failed\n\x1bPfake-incomplete"}, []string{"fake-incomplete"}, "TLS verification failed"},
		{"incomplete-apc", []string{"TLS verification failed\n\x1b_fake-incomplete"}, []string{"fake-incomplete"}, "TLS verification failed"},
		{"incomplete-esc", []string{"TLS verification failed\nunsafe unfinished record\x1b"}, []string{"unsafe unfinished record"}, "TLS verification failed"},
		{"incomplete-utf8", []string{"TLS verification failed\nunsafe unfinished record\xc3"}, []string{"unsafe unfinished record"}, "TLS verification failed"},
		{"no-newline-secret", []string{"TLS verification failed\ntok", "en=fake-final-secret"}, []string{"fake-final-secret"}, "TLS verification failed"},
		{"oversized-url", []string{"https://fake-user:" + strings.Repeat("fake-url-secret", 4096) + "@registry.invalid\nTLS verification failed\n"}, []string{"fake-user", "fake-url-secret"}, "TLS verification failed"},
		{"giant-unfinished", []string{"TLS verification failed\n" + strings.Repeat("fake-unfinished", 1<<16)}, []string{"fake-unfinished"}, "TLS verification failed"},
		{"incomplete-url-userinfo", []string{"TLS verification failed\nhttps://fake-user:fake-incomplete-password"}, []string{"fake-user", "fake-incomplete-password"}, "TLS verification failed"},
		{"bidi-format", []string{"TLS\u202e verification failed\u2066\u2069\n"}, nil, "TLS verification failed"},
		{"quoted-json-token", []string{"{\"token\":\"fake-json-secret\"}\nTLS verification failed\n"}, []string{"fake-json-secret"}, "TLS verification failed"},
		{"quoted-password", []string{"password=\"fake secret\"\nTLS verification failed\n"}, []string{"fake", "secret\""}, "TLS verification failed"},
		{"quoted-keys-values", []string{"{\"password\":\"fake quoted value\", 'api_key':'fake single value'}\nTLS verification failed\n"}, []string{"fake", "quoted value", "single value"}, "TLS verification failed"},
		{"escaped-quotes", []string{"token=\"fake \\\"quoted\\\" value\"\nTLS verification failed\n"}, []string{"fake", "quoted", "value"}, "TLS verification failed"},
		{"incomplete-quoted-secret", []string{"TLS verification failed\n{\"token\":\"fake unfinished secret"}, []string{"fake", "unfinished secret"}, "TLS verification failed"},
		{"ambiguous-quoted-secret", []string{"TLS verification failed\npassword=\"fake secret\"fake-suffix"}, []string{"fake", "secret", "suffix"}, "TLS verification failed"},
		{"ambiguous-unquoted-secret", []string{"TLS verification failed\npassword=fake\" secret\""}, []string{"fake", "secret"}, "TLS verification failed"},
		{"normal-quoted-progress", []string{"{\"message\":\"Preparing \\\"useful\\\" dependencies\"}\nTLS verification failed\n"}, nil, "{\"message\":\"Preparing \\\"useful\\\" dependencies\"}"},
		{"normal-no-newline", []string{"Preparing dependencies\nTLS verification failed: certificate expired"}, nil, "certificate expired"},
	}
}

// This writer checks every first disclosure, not merely a final buffered message.
type programCaptureCheckedPublicWriter struct {
	mu        sync.Mutex
	unsafe    bool
	forbidden []string
	bytes.Buffer
}

func (w *programCaptureCheckedPublicWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !programCaptureDiagnosticIsSafe(string(p), w.forbidden) {
		w.unsafe = true
		// Keep RED evidence textual and safe, including on an unsanitized baseline.
		return len(p), nil
	}
	return w.Buffer.Write(p)
}
func (w *programCaptureCheckedPublicWriter) WriteString(s string) (int, error) {
	return w.Write([]byte(s))
}

func programCaptureDiagnosticIsSafe(text string, forbidden []string) bool {
	if !utf8.ValidString(text) {
		return false
	}
	for _, r := range text {
		if (unicode.IsControl(r) || unicode.Is(unicode.Bidi_Control, r)) && r != '\n' && r != '\r' && r != '\t' {
			return false
		}
	}
	for _, secret := range forbidden {
		if strings.Contains(text, secret) {
			return false
		}
	}
	return true
}

func assertProgramCaptureDiagnosticSafe(t *testing.T, text string, forbidden []string) {
	t.Helper()
	if !programCaptureDiagnosticIsSafe(text, forbidden) {
		t.Fatal("public diagnostic disclosed fake credential, terminal payload, or invalid UTF-8")
	}
}

func (w *programCaptureCheckedPublicWriter) assertSafe(t *testing.T) {
	t.Helper()
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.unsafe {
		t.Fatal("first public writer received unsafe diagnostic bytes")
	}
	assertProgramCaptureDiagnosticSafe(t, w.String(), w.forbidden)
}

// This is the preparation caller, its actual owned child, all four public stream
// routes, and its returned error; no fetched package, runtime, or agent is used.
func TestProgramCaptureDiagnosticsPreparationFirstDisclosure(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	old := forkBuildChildCommand
	t.Cleanup(func() { forkBuildChildCommand = old })
	for _, tc := range programCaptureDiagnosticCases() {
		for _, fd := range []int{1, 2, 3, 4} {
			t.Run(tc.name+"/fd"+string(rune('0'+fd)), func(t *testing.T) {
				chunks := make([][]byte, len(tc.chunks))
				for i, chunk := range tc.chunks {
					chunks[i] = []byte(chunk)
				}
				raw, err := json.Marshal(programCaptureDiagnosticFixture{Chunks: chunks, FD: fd})
				if err != nil {
					t.Fatal(err)
				}
				file := filepath.Join(t.TempDir(), "fixture.json")
				if err := os.WriteFile(file, raw, 0600); err != nil {
					t.Fatal(err)
				}
				forkBuildChildCommand = func(argv []string) (*exec.Cmd, error) {
					if len(argv) < 2 || argv[1] != programCaptureJailVerb {
						t.Fatal("preparation bypassed sealed producer")
					}
					return exec.Command(self, "program-capture-diagnostics-fixture", file), nil
				}
				store := &capture.Store{Dir: t.TempDir()}
				in := npmCaptureInput{Owner: "diagnostic-fixture", Bin: "probe", Package: "diagnostic-fixture"}
				boundary, err := registerNPMCaptureRequest(context.Background(), store, in)
				if err != nil {
					t.Fatal(err)
				}
				public := programCaptureCheckedPublicWriter{forbidden: tc.forbidden}
				_, err = advanceNPMCapture(context.Background(), store, in, "diagnostic-fixture", false, boundary, &public, func(commit func() error) error { return commit() })
				if err == nil {
					t.Fatal("failed producer reported preparation success")
				}
				public.assertSafe(t)
				assertProgramCaptureDiagnosticSafe(t, err.Error(), tc.forbidden)
				if !strings.Contains(public.String(), tc.useful) || !strings.Contains(err.Error(), tc.useful) {
					t.Fatal("useful preparation diagnostic lost")
				}
				if len(err.Error()) > maxProgramCaptureDiagnostic+256 {
					t.Fatal("preparation error exceeded diagnostic bound")
				}
				record, _ := readNPMCaptureRecord(store, in)
				if record.Entry != "" {
					t.Fatal("failed preparation published a target")
				}
			})
		}
	}
}

// Byte-at-a-time writes force every prefix, value, UTF-8 rune and escape split;
// owned-child pipes may coalesce fixture writes, so they cannot pin this alone.
func TestProgramCaptureDiagnosticsEveryWriteBoundary(t *testing.T) {
	for _, tc := range programCaptureDiagnosticCases() {
		t.Run(tc.name, func(t *testing.T) {
			public := programCaptureCheckedPublicWriter{forbidden: tc.forbidden}
			diag := &programCaptureDiagnostic{}
			stream := &programCaptureDiagnosticTee{out: &public, diagnostic: diag}
			for _, chunk := range tc.chunks {
				for i := range len(chunk) {
					if n, err := stream.Write([]byte{chunk[i]}); err != nil || n != 1 {
						t.Fatal("stream write failed")
					}
					if len(stream.record) > maxProgramCaptureDiagnosticRecord || cap(stream.record) > maxProgramCaptureDiagnosticRecord || len(diag.tail) > maxProgramCaptureDiagnostic || cap(diag.tail) > maxProgramCaptureDiagnostic {
						t.Fatal("retained diagnostic exceeded fixed bounds")
					}
				}
			}
			stream.finish()
			public.assertSafe(t)
			assertProgramCaptureDiagnosticSafe(t, diag.String(), tc.forbidden)
			if !strings.Contains(public.String(), tc.useful) || !strings.Contains(diag.String(), tc.useful) {
				t.Fatal("useful diagnostic lost across write boundaries")
			}
		})
	}
}

func TestProgramCaptureDiagnosticsProgressAndSeparateStreams(t *testing.T) {
	public := programCaptureCheckedPublicWriter{forbidden: []string{"fake-interleaved-secret"}}
	diag := &programCaptureDiagnostic{}
	stdout := &programCaptureDiagnosticTee{out: &public, diagnostic: diag}
	stderr := &programCaptureDiagnosticTee{out: &public, diagnostic: diag}
	_, _ = stdout.Write([]byte("to"))
	_, _ = stderr.Write([]byte("Preparing dependencies\n"))
	if public.String() != "Preparing dependencies\n" {
		t.Fatal("complete normal progress was suppressed")
	}
	_, _ = stdout.Write([]byte("ken=fake-interleaved-secret\n"))
	stdout.finish()
	stderr.finish()
	public.assertSafe(t)
	if !strings.Contains(public.String(), "token=<redacted>") {
		t.Fatal("stream parser states were mixed")
	}
}

func TestProgramCaptureDiagnosticsPrivateResolutionProtocolUnchanged(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	in := npmCaptureInput{Owner: "diagnostic-fixture", Bin: "probe", Package: "diagnostic-fixture"}
	// A valid synthetic typed result larger than both diagnostic limits, with bytes
	// that would be removed/redacted if a public sanitizer touched the private file.
	padding := strings.Repeat("private-protocol-padding ", 512) + "token=fake-private-protocol-secret\x1b]52;c;fake-private-control\a"
	lock, err := json.Marshal(map[string]any{
		"lockfileVersion": 3, "padding": padding,
		"packages": map[string]any{"node_modules/diagnostic-fixture": map[string]any{
			"version": "1.0.0", "resolved": "https://registry.invalid/fixture.tgz", "integrity": "sha512-fake", "bin": map[string]string{"probe": "bin/probe.js"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := npmCaptureResolution{
		Input: in, Lock: lock, Release: "1.0.0", Source: "https://registry.invalid/fixture.tgz|sha512-fake", BinPath: "bin/probe.js",
		Dependencies: []string{"node_modules/diagnostic-fixture=1.0.0|https://registry.invalid/fixture.tgz|sha512-fake"},
		Runtime:      npmCaptureRuntime{Image: "synthetic-image", Node: "synthetic-node", NodeDigest: "synthetic-digest", NPM: "synthetic-npm", Platform: "linux/amd64", ABI: "synthetic-abi"},
	}
	resolution, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	if len(resolution) <= maxProgramCaptureDiagnosticRecord {
		t.Fatal("private protocol fixture is too small")
	}
	if _, err := want.inputs(); err != nil {
		t.Fatal("invalid private protocol fixture")
	}
	old := forkBuildChildCommand
	t.Cleanup(func() { forkBuildChildCommand = old })
	file := filepath.Join(t.TempDir(), "fixture.json")
	forkBuildChildCommand = func(argv []string) (*exec.Cmd, error) {
		workspace := ""
		for _, arg := range argv {
			if value, ok := strings.CutPrefix(arg, "--workspace="); ok {
				workspace = value
			}
		}
		raw, err := json.Marshal(programCaptureDiagnosticFixture{Chunks: [][]byte{[]byte("Resolution complete\n")}, FD: 1, Workspace: workspace, Resolution: resolution})
		if err != nil {
			return nil, err
		}
		if err := os.WriteFile(file, raw, 0600); err != nil {
			return nil, err
		}
		return exec.Command(self, "program-capture-diagnostics-fixture", file), nil
	}
	public := programCaptureCheckedPublicWriter{forbidden: []string{"fake-private-protocol-secret", "fake-private-control"}}
	got, err := resolveSelectedNPMCapture(context.Background(), &capture.Store{Dir: t.TempDir()}, in, &public)
	if err != nil {
		t.Fatal("successful private result failed preparation decoding")
	}
	public.assertSafe(t)
	if !bytes.Equal(got.Lock, want.Lock) {
		t.Fatal("private lock/result bytes were sanitized or truncated")
	}
	gotInputs, err := got.inputs()
	if err != nil {
		t.Fatal("parsed private result lost its input identity")
	}
	wantInputs, _ := want.inputs()
	if gotID, _ := gotInputs.Identity(); gotID == "" {
		t.Fatal("parsed private result has no identity")
	} else if wantID, _ := wantInputs.Identity(); gotID != wantID {
		t.Fatal("private protocol identity changed")
	}
	if !strings.Contains(public.String(), "Resolution complete") {
		t.Fatal("successful public progress was lost")
	}
}

type programCaptureFinalFailWriter struct {
	err    error
	short  bool
	cancel context.CancelFunc
	writes int
}

func (w *programCaptureFinalFailWriter) Write(p []byte) (int, error) {
	w.writes++
	if w.cancel != nil {
		w.cancel()
	}
	if w.short {
		return len(p) - 1, nil
	}
	return 0, w.err
}

func TestProgramCaptureDiagnosticsFinalFlushErrors(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	old := forkBuildChildCommand
	t.Cleanup(func() { forkBuildChildCommand = old })
	sentinel := errors.New("safe fixture public writer failure")
	in := npmCaptureInput{Owner: "diagnostic-fixture", Bin: "probe", Package: "diagnostic-fixture"}
	result := npmCaptureResolution{
		Input: in, Lock: json.RawMessage(`{"lockfileVersion":3,"packages":{"node_modules/diagnostic-fixture":{"version":"1.0.0","resolved":"https://registry.invalid/fixture.tgz","integrity":"sha512-fake","bin":{"probe":"bin/probe.js"}}}}`),
		Release: "1.0.0", Source: "https://registry.invalid/fixture.tgz|sha512-fake", BinPath: "bin/probe.js",
		Dependencies: []string{"node_modules/diagnostic-fixture=1.0.0|https://registry.invalid/fixture.tgz|sha512-fake"},
		Runtime:      npmCaptureRuntime{Image: "synthetic-image", Node: "synthetic-node", NodeDigest: "synthetic-digest", NPM: "synthetic-npm", Platform: "linux/amd64", ABI: "synthetic-abi"},
	}
	if _, err := result.inputs(); err != nil {
		t.Fatal("invalid successful resolver fixture")
	}
	resolution, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name                          string
		success, short, multi, cancel bool
	}{
		{"success-writer-error", true, false, false, false},
		{"success-short-write", true, true, false, false},
		{"success-earlier-write-error", true, false, false, false},
		{"success-multistream-error", true, false, true, false},
		{"child-failure-precedence", false, false, false, false},
		{"cancel-precedence", true, false, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := programCaptureDiagnosticFixture{Chunks: [][]byte{[]byte("Final useful diagnostic")}, FD: 1}
			if tc.success {
				fixture.Resolution = resolution
			}
			if tc.name == "success-earlier-write-error" {
				fixture.Chunks = [][]byte{[]byte("Useful completed diagnostic\nFinal diagnostic")}
			}
			if tc.multi {
				fixture.OtherChunks = map[int][][]byte{4: {[]byte("Other final diagnostic")}}
			}
			file := filepath.Join(t.TempDir(), "fixture.json")
			forkBuildChildCommand = func(argv []string) (*exec.Cmd, error) {
				for _, arg := range argv {
					if workspace, ok := strings.CutPrefix(arg, "--workspace="); ok {
						fixture.Workspace = workspace
					}
				}
				raw, err := json.Marshal(fixture)
				if err != nil {
					return nil, err
				}
				if err := os.WriteFile(file, raw, 0600); err != nil {
					return nil, err
				}
				return exec.Command(self, "program-capture-diagnostics-fixture", file), nil
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			writer := &programCaptureFinalFailWriter{err: sentinel, short: tc.short}
			if tc.cancel {
				writer.cancel = cancel
			}
			_, err = resolveSelectedNPMCapture(ctx, &capture.Store{Dir: t.TempDir()}, in, writer)
			expectedWrites := 1
			if tc.multi {
				expectedWrites = 2
			}
			if writer.writes != expectedWrites {
				t.Fatal("not every final public stream was flushed")
			}
			if tc.cancel {
				if !errors.Is(err, context.Canceled) {
					t.Fatal("flush failure displaced cancellation")
				}
			} else if !tc.success {
				if err == nil || !strings.Contains(err.Error(), "status 37") || errors.Is(err, sentinel) {
					t.Fatal("flush failure displaced original child failure")
				}
			} else {
				want := sentinel
				if tc.short {
					want = io.ErrShortWrite
				}
				if !errors.Is(err, want) {
					t.Fatal("successful child ignored final public writer failure")
				}
			}
		})
	}
}
