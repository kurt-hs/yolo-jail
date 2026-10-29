package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/brokeraudit"
	"github.com/mschulkind-oss/yolo-jail/internal/brokerscope"
	"github.com/mschulkind-oss/yolo-jail/internal/brokerstore"
	"github.com/mschulkind-oss/yolo-jail/internal/ghbroker"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
	"github.com/mschulkind-oss/yolo-jail/internal/shquote"
	"github.com/mschulkind-oss/yolo-jail/internal/svcendpoint"
)

// TestGitHubApprovalBrokerHelper runs the production github-broker Main in a child process.
func TestGitHubApprovalBrokerHelper(t *testing.T) {
	if os.Getenv("YOLO_GH_APPROVAL_BROKER_HELPER") != "1" {
		t.Skip("run as the GitHub broker helper only")
	}
	for i, arg := range os.Args {
		if arg == "--" {
			os.Exit(ghbroker.Main(os.Args[i+1:]))
		}
	}
	os.Exit(2)
}

type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}

func (b *lockedBuffer) Contains(s string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return strings.Contains(b.b.String(), s)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.String()
}

type githubApprovalBroker struct {
	cmd      *exec.Cmd
	done     chan struct{}
	endpoint string
	stop     chan struct{}
	logs     lockedBuffer
}

func startGitHubApprovalBroker(t *testing.T, home, workspace, fakeGH, ghConfig, sfPath, userConfig string) *githubApprovalBroker {
	t.Helper()
	if userConfig != "" {
		configDir := filepath.Join(home, ".config", "yolo-jail")
		if err := os.MkdirAll(configDir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(configDir, "config.jsonc"), []byte(userConfig), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	sockDir, err := os.MkdirTemp("/tmp", "gh-approval-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(sockDir) })
	socket := filepath.Join(sockDir, "broker.sock")
	tmpDir := filepath.Join(home, "tmp")
	if err := os.MkdirAll(tmpDir, 0o700); err != nil {
		t.Fatal(err)
	}
	p := &githubApprovalBroker{done: make(chan struct{}), stop: make(chan struct{})}
	p.cmd = exec.Command(os.Args[0], "-test.run=^TestGitHubApprovalBrokerHelper$", "-test.count=1", "--",
		"--socket", socket, "--scope-file", sfPath)
	p.cmd.Dir = workspace
	p.cmd.Env = append(os.Environ(), "YOLO_GH_APPROVAL_BROKER_HELPER=1", "YOLO_VERSION=", "YOLO_USER_LAYER=",
		"HOME="+home, "TMPDIR="+tmpDir, "GH_CONFIG_DIR="+ghConfig, "PATH="+filepath.Dir(fakeGH)+":/usr/bin:/bin",
		svcendpoint.AdvertiseHostEnv+"=127.0.0.1")
	p.cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	p.cmd.Stdout, p.cmd.Stderr = &p.logs, &p.logs
	if err := p.cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go func() { _ = p.cmd.Wait(); close(p.done) }()
	t.Cleanup(func() {
		_ = syscall.Kill(-p.cmd.Process.Pid, syscall.SIGTERM)
		select {
		case <-p.done:
		case <-time.After(2 * time.Second):
			_ = syscall.Kill(-p.cmd.Process.Pid, syscall.SIGKILL)
			<-p.done
		}
		close(p.stop)
		if t.Failed() {
			t.Logf("github-broker output:\n%s", p.logs.String())
		}
	})
	waitGitHubApproval(t, "the production daemon socket", 10*time.Second, func() bool {
		_, err := os.Stat(socket)
		return err == nil
	})
	endpointRoot, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	endpointDir := filepath.Join(endpointRoot, paths.JailShortHash("approval-fixture"))
	if err := os.MkdirAll(endpointDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(endpointDir, 0o700); err != nil {
		t.Fatal(err)
	}
	p.endpoint = filepath.Join(endpointDir, "github-broker.endpoint")
	go func() { _ = svcendpoint.ServeFront(p.endpoint, "127.0.0.1", socket, p.stop) }()
	waitGitHubApproval(t, "the production service front", 10*time.Second, func() bool {
		return svcendpoint.Probe(p.endpoint)
	})
	return p
}

func waitGitHubApproval(t *testing.T, what string, timeout time.Duration, ready func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !ready() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

type productionGitHubFixture struct {
	broker  *githubApprovalBroker
	store   *brokerstore.Store
	calls   string
	stdin   string
	hookLog string
}

func newProductionGitHubFixture(t *testing.T, userConfig string, withHook bool) *productionGitHubFixture {
	t.Helper()
	t.Setenv("YOLO_VERSION", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	workspace := filepath.Join(root, "workspace")
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	fakeDir := filepath.Join(root, "fake-gh")
	if err := os.MkdirAll(fakeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	calls := filepath.Join(fakeDir, "calls")
	stdinReceipt := filepath.Join(fakeDir, "stdin")
	fakeGH := filepath.Join(fakeDir, "gh")
	script := "#!/bin/sh\ncase \"$1\" in\n  --version) echo 'gh version 2.101.0 (fake)' ;;\n  auth) echo 'fake-token-1234567890' ;;\n  *) printf '%s\\000' \"$@\" >> " + shquote.Quote(calls) +
		"; printf '\\000' >> " + shquote.Quote(calls) + "; cat > " + shquote.Quote(stdinReceipt) +
		"; echo 'fake GitHub receipt' ;;\nesac\n"
	if err := os.WriteFile(fakeGH, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	ghConfig := filepath.Join(root, "gh-config")
	if err := os.MkdirAll(ghConfig, 0o700); err != nil {
		t.Fatal(err)
	}
	hookLog := filepath.Join(fakeDir, "requests.log")
	if withHook {
		hookPath := filepath.Join(fakeDir, "on-request")
		hookScript := fmt.Sprintf("#!/bin/sh\ncat > %s\necho approve\n", shquote.Quote(hookLog))
		if err := os.WriteFile(hookPath, []byte(hookScript), 0o755); err != nil {
			t.Fatal(err)
		}
		var config map[string]json.RawMessage
		if userConfig == "" {
			config = make(map[string]json.RawMessage)
		} else if err := json.Unmarshal([]byte(userConfig), &config); err != nil {
			t.Fatal(err)
		}
		hookCommand, err := json.Marshal(shquote.Quote(hookPath))
		if err != nil {
			t.Fatal(err)
		}
		config["on_request"] = hookCommand
		encodedConfig, err := json.Marshal(config)
		if err != nil {
			t.Fatal(err)
		}
		userConfig = string(encodedConfig)
	}
	scopeFile, err := brokerscope.Write(brokerscope.File{Source: ghbroker.Source, LaunchID: "cafe", PID: os.Getpid(),
		Container: "approval-fixture", Workspace: workspace, Repos: []string{"o/r"},
		Grants: []brokerscope.Grant{{Kind: brokerscope.KindRead, Repo: brokerscope.AnyRepo}}})
	if err != nil {
		t.Fatal(err)
	}
	return &productionGitHubFixture{
		broker:  startGitHubApprovalBroker(t, home, workspace, fakeGH, ghConfig, scopeFile, userConfig),
		store:   &brokerstore.Store{Dir: paths.BrokerSourceDir(ghbroker.Source), Source: ghbroker.Source},
		calls:   calls,
		stdin:   stdinReceipt,
		hookLog: hookLog,
	}
}

func (f *productionGitHubFixture) forward(argv ...string) (int, string, string) {
	return f.forwardInput("", argv...)
}

func (f *productionGitHubFixture) forwardInput(stdin string, argv ...string) (int, string, string) {
	var out, stderr bytes.Buffer
	code := ghbroker.Forward(argv, ghbroker.ForwardEnv{
		Getenv: func(key string) string {
			if key == ghbroker.EndpointEnv {
				return f.broker.endpoint
			}
			return os.Getenv(key)
		},
		Stdin: strings.NewReader(stdin), Stdout: &out, Stderr: &stderr,
		OriginRepo: func() string { return "o/r" },
	})
	return code, out.String(), stderr.String()
}

func TestProductionStandingWritePolicyRefusesWritesAndKeepsReads(t *testing.T) {
	userConfig := `{"brokered":{"github":{"sets":[{"name":"read-only","mode":"standing"},{"name":"read-write","mode":"standing"}]}}}`
	fixture := newProductionGitHubFixture(t, userConfig, false)
	if code, out, stderr := fixture.forward("pr", "view", "41", "--repo", "o/r"); code != 0 || !strings.Contains(out, "fake GitHub receipt") {
		t.Fatalf("standing read changed: exit %d, stdout %q, stderr %q", code, out, stderr)
	}
	code, _, stderr := fixture.forward("pr", "comment", "41", "--repo", "o/r", "--body", "must-not-run")
	if code != ghbroker.ExitNoPerm || !strings.Contains(stderr, "brokered.github.sets") || !strings.Contains(stderr, "remove") {
		t.Fatalf("standing write policy was not refused with a repair step: exit %d, stderr %q", code, stderr)
	}
	for _, argv := range [][]string{
		{"pr", "view", "41", "--repo", "outside/secret"},
		{"auth", "token"},
		{"api", "-H", "X-HTTP-Method-Override: DELETE", "repos/o/r/issues/1"},
	} {
		before, err := os.ReadFile(fixture.calls)
		if err != nil {
			t.Fatal(err)
		}
		if code, _, stderr := fixture.forward(argv...); code == 0 {
			t.Fatalf("out-of-scope or secret command was not refused: %v", argv)
		} else if strings.Contains(stderr, "fake GitHub receipt") {
			t.Fatalf("refused command reached fake host gh: %v: %q", argv, stderr)
		}
		after, err := os.ReadFile(fixture.calls)
		if err != nil || !bytes.Equal(before, after) {
			t.Fatalf("refused command reached fake host gh: argv=%v before=%q after=%q err=%v", argv, before, after, err)
		}
	}
	requests, _, err := fixture.store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(requests) != 0 {
		t.Fatalf("unsupported standing write filed a request: %+v", requests)
	}
	data, err := os.ReadFile(fixture.calls)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "pr comment") {
		t.Fatalf("standing write reached host gh: %q", data)
	}
}

func TestProductionGitHubWriteApprovalFlow(t *testing.T) {
	for _, tc := range []struct {
		name, command  string
		argv, hostArgv []string
		stdin          string
		withHook       bool
	}{
		{name: "PR comment manual approval without on_request", command: "comment",
			argv:     []string{"pr", "comment", "41", "--repo", "o/r", "--body-file", "-"},
			hostArgv: []string{"pr", "comment", "--repo=o/r", "--body-file=-", "41"}, stdin: "stdin-approval-body\n"},
		{name: "issue close despite on_request output", command: "close",
			argv:     []string{"issue", "close", "7", "--repo", "o/r"},
			hostArgv: []string{"issue", "close", "--repo=o/r", "7"}, withHook: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newProductionGitHubFixture(t, "", tc.withHook)
			type result struct {
				code        int
				out, stderr string
			}
			finished := make(chan result, 1)
			go func() {
				code, out, stderr := fixture.forwardInput(tc.stdin, tc.argv...)
				finished <- result{code: code, out: out, stderr: stderr}
			}()

			var request brokerstore.Request
			deadline := time.Now().Add(3 * time.Second)
			for request.ID == "" {
				select {
				case got := <-finished:
					t.Fatalf("production forwarder returned before filing an approval request: exit %d, stderr %q; want exit %d and a pending request", got.code, got.stderr, ghbroker.ExitTempFail)
				default:
				}
				requests, _, err := fixture.store.List()
				if err != nil {
					t.Fatal(err)
				}
				for _, candidate := range requests {
					if candidate.State == "pending" && len(candidate.Argv) > 1 && candidate.Argv[1] == tc.command {
						request = candidate
						break
					}
				}
				if request.ID != "" {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("the production broker did not file a request for host approval")
				}
				time.Sleep(10 * time.Millisecond)
			}

			stdinSum := sha256.Sum256([]byte(tc.stdin))
			wantStdinDigest := hex.EncodeToString(stdinSum[:])
			if !slices.Equal(request.Argv, tc.hostArgv) || request.Repo != "o/r" || request.StdinDigest != wantStdinDigest ||
				request.Digest != brokerstore.CommandDigest(request) {
				t.Fatalf("filed production action identity differs from the requested exact action: %+v", request)
			}
			view := fixture.store.RequestView(request)
			encodedView, err := json.Marshal(view)
			if err != nil {
				t.Fatal(err)
			}
			if view.CommandDigest != request.Digest || view.StdinDigest != request.StdinDigest ||
				(tc.stdin != "" && bytes.Contains(encodedView, []byte(tc.stdin))) {
				t.Fatalf("request JSON does not expose only the action digests: %s", encodedView)
			}
			var listed bytes.Buffer
			printApproveList(&listed, brokerstore.Snapshot{Requests: []brokerstore.Request{request}})
			if !strings.Contains(listed.String(), "command "+request.Digest) ||
				!strings.Contains(listed.String(), "stdin SHA-256 "+request.StdinDigest) ||
				(tc.stdin != "" && strings.Contains(listed.String(), tc.stdin)) {
				t.Fatalf("host text listing does not identify the exact body without exposing it: %q", listed.String())
			}

			waitGitHubApproval(t, "the host broker-log request", 3*time.Second, func() bool {
				return fixture.broker.logs.Contains(request.ID)
			})
			if tc.withHook {
				waitGitHubApproval(t, "the host on_request receipt", 3*time.Second, func() bool {
					data, err := os.ReadFile(fixture.hookLog)
					return err == nil && bytes.Contains(data, []byte(request.ID))
				})
				hookView, err := os.ReadFile(fixture.hookLog)
				if err != nil || !bytes.Contains(hookView, []byte(`"command_digest":"`+request.Digest+`"`)) ||
					!bytes.Contains(hookView, []byte(`"stdin_digest":"`+request.StdinDigest+`"`)) ||
					(tc.stdin != "" && bytes.Contains(hookView, []byte(tc.stdin))) {
					t.Fatalf("hook request summary lacks safe exact action identity: %s, %v", hookView, err)
				}
			}
			status, err := fixture.store.Status(request.Jail, request.Start, request.ID)
			if err != nil || status.State != "pending" {
				t.Fatalf("on_request output was interpreted as consent: %+v %v", status, err)
			}
			if _, err := os.Stat(fixture.calls); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("a pending request reached host gh before host approval: stat err %v", err)
			}
			if rc := Main([]string{"yolo", "approve", request.ID}); rc != 0 {
				t.Fatalf("host yolo approve exited %d for %s", rc, request.ID)
			}
			select {
			case got := <-finished:
				if got.code != 0 || !strings.Contains(got.out, "fake GitHub receipt") {
					t.Fatalf("approved production write: exit %d, stdout %q, stderr %q", got.code, got.out, got.stderr)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("the production forwarder did not return after host approval")
			}
			data, err := os.ReadFile(fixture.calls)
			if err != nil {
				t.Fatal(err)
			}
			wantReceipt := append([]byte(strings.Join(tc.hostArgv, "\x00")), 0, 0)
			if !bytes.Equal(data, wantReceipt) {
				t.Fatalf("fake host gh argv receipt differs at argument boundaries or was dispatched more than once: got %q want %q", data, wantReceipt)
			}
			gotStdin, err := os.ReadFile(fixture.stdin)
			if err != nil {
				t.Fatal(err)
			}
			gotStdinSum := sha256.Sum256(gotStdin)
			if hex.EncodeToString(gotStdinSum[:]) != request.StdinDigest || !bytes.Equal(gotStdin, []byte(tc.stdin)) {
				t.Fatalf("fake host gh received stdin other than the approved bytes: sha=%x bytes=%d", gotStdinSum, len(gotStdin))
			}
			events, _, err := brokeraudit.Read(paths.BrokerAuditLog())
			if err != nil {
				t.Fatal(err)
			}
			var spent, dispatched bool
			dispatchCount := 0
			for _, event := range events {
				if event.Request != request.ID || event.Grant == "" || event.CommandDigest != request.Digest || event.StdinDigest != request.StdinDigest {
					continue
				}
				spent = spent || event.Event == "grant-used" && event.Outcome == "spent-before-dispatch"
				if event.Event == "call" && event.Outcome == "ran" {
					dispatchCount++
					dispatched = true
				}
			}
			if !spent || !dispatched || dispatchCount != 1 {
				t.Fatalf("audit did not link exactly one exact request, grant and dispatched action: spent=%t dispatched=%t count=%d events=%+v", spent, dispatched, dispatchCount, events)
			}
			if tc.command == "comment" {
				wantReceipt := append([]byte(strings.Join(tc.hostArgv, "\x00")), 0, 0)
				seenIDs := map[string]bool{request.ID: true}
				mutations := []struct {
					argv, wantArgv []string
					body           string
					wantDigest     string
				}{
					{argv: tc.argv, wantArgv: tc.hostArgv, body: tc.stdin, wantDigest: request.Digest},
					{argv: tc.argv, wantArgv: tc.hostArgv, body: "changed body after approval\n"},
					{argv: []string{"pr", "comment", "42", "--repo", "o/r", "--body-file", "-"},
						wantArgv: []string{"pr", "comment", "--repo=o/r", "--body-file=-", "42"}, body: tc.stdin},
				}
				for _, mutation := range mutations {
					replayFinished := make(chan result, 1)
					go func(argv []string, body string) {
						code, out, stderr := fixture.forwardInput(body, argv...)
						replayFinished <- result{code: code, out: out, stderr: stderr}
					}(mutation.argv, mutation.body)
					var next brokerstore.Request
					deadline := time.Now().Add(3 * time.Second)
					for next.ID == "" {
						requests, _, err := fixture.store.List()
						if err != nil {
							t.Fatal(err)
						}
						for _, candidate := range requests {
							if candidate.State == "pending" && !seenIDs[candidate.ID] {
								next = candidate
								break
							}
						}
						if next.ID != "" {
							break
						}
						select {
						case got := <-replayFinished:
							t.Fatalf("production replay returned before a host decision: exit %d stderr %q", got.code, got.stderr)
						default:
						}
						if time.Now().After(deadline) {
							t.Fatal("production replay did not file a new exact request")
						}
						time.Sleep(10 * time.Millisecond)
					}
					bodySum := sha256.Sum256([]byte(mutation.body))
					if !slices.Equal(next.Argv, mutation.wantArgv) || next.StdinDigest != hex.EncodeToString(bodySum[:]) ||
						(mutation.wantDigest != "" && next.Digest != mutation.wantDigest) ||
						(mutation.wantDigest == "" && next.Digest == request.Digest) {
						t.Fatalf("production replay identity did not bind argv and body exactly: original=%+v next=%+v", request, next)
					}
					seenIDs[next.ID] = true
					if rc := Main([]string{"yolo", "approve", next.ID, "--deny"}); rc != 0 {
						t.Fatalf("deny replay/mutated request %s returned %d", next.ID, rc)
					}
					select {
					case got := <-replayFinished:
						if got.code != ghbroker.ExitNoPerm {
							t.Fatalf("denied production replay ran or returned %d", got.code)
						}
					case <-time.After(5 * time.Second):
						t.Fatal("denied production replay did not return")
					}
					gotReceipt, err := os.ReadFile(fixture.calls)
					if err != nil || !bytes.Equal(gotReceipt, wantReceipt) {
						t.Fatalf("replay/mutated body reached host gh: %q err %v", gotReceipt, err)
					}
				}
			}
		})
	}
}
