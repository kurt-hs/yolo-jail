package cli

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/brokerscope"
	"github.com/mschulkind-oss/yolo-jail/internal/brokerstore"
	"github.com/mschulkind-oss/yolo-jail/internal/ghbroker"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
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

type githubApprovalBroker struct {
	cmd      *exec.Cmd
	done     chan struct{}
	endpoint string
	stop     chan struct{}
	logs     bytes.Buffer
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
	p := &githubApprovalBroker{done: make(chan struct{}), stop: make(chan struct{})}
	p.cmd = exec.Command(os.Args[0], "-test.run=^TestGitHubApprovalBrokerHelper$", "-test.count=1", "--",
		"--socket", socket, "--scope-file", sfPath)
	p.cmd.Dir = workspace
	p.cmd.Env = append(os.Environ(), "YOLO_GH_APPROVAL_BROKER_HELPER=1", "YOLO_VERSION=", "YOLO_USER_LAYER=",
		"HOME="+home, "GH_CONFIG_DIR="+ghConfig, "PATH="+filepath.Dir(fakeGH)+":/usr/bin:/bin",
		svcendpoint.AdvertiseHostEnv+"=127.0.0.1")
	p.cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	p.cmd.Stdout, p.cmd.Stderr = &p.logs, &p.logs
	if err := p.cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go func() { _ = p.cmd.Wait(); close(p.done) }()
	t.Cleanup(func() {
		_ = syscall.Kill(-p.cmd.Process.Pid, syscall.SIGKILL)
		<-p.done
		close(p.stop)
		if t.Failed() {
			t.Logf("github-broker output:\n%s", p.logs.String())
		}
	})
	waitGitHubApproval(t, "the production daemon socket", 10*time.Second, func() bool {
		_, err := os.Stat(socket)
		return err == nil
	})
	endpointDir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
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
	broker *githubApprovalBroker
	store  *brokerstore.Store
	calls  string
}

func newProductionGitHubFixture(t *testing.T, userConfig string) *productionGitHubFixture {
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
	fakeGH := filepath.Join(fakeDir, "gh")
	script := fmt.Sprintf("#!/bin/sh\ncase \"$1\" in\n  --version) echo 'gh version 2.101.0 (fake)' ;;\n  auth) echo 'fake-token-1234567890' ;;\n  *) printf '%%s\\n' \"$*\" >> %q; echo 'fake GitHub receipt' ;;\nesac\n", calls)
	if err := os.WriteFile(fakeGH, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	ghConfig := filepath.Join(root, "gh-config")
	if err := os.MkdirAll(ghConfig, 0o700); err != nil {
		t.Fatal(err)
	}
	scopeFile, err := brokerscope.Write(brokerscope.File{Source: ghbroker.Source, LaunchID: "cafe", PID: os.Getpid(),
		Container: "approval-fixture", Workspace: workspace, Repos: []string{"o/r"},
		Grants: []brokerscope.Grant{{Kind: brokerscope.KindRead, Repo: brokerscope.AnyRepo}}})
	if err != nil {
		t.Fatal(err)
	}
	return &productionGitHubFixture{
		broker: startGitHubApprovalBroker(t, home, workspace, fakeGH, ghConfig, scopeFile, userConfig),
		store:  &brokerstore.Store{Dir: paths.BrokerSourceDir(ghbroker.Source), Source: ghbroker.Source},
		calls:  calls,
	}
}

func (f *productionGitHubFixture) forward(argv ...string) (int, string, string) {
	var out, stderr bytes.Buffer
	code := ghbroker.Forward(argv, ghbroker.ForwardEnv{
		Getenv: func(key string) string {
			if key == ghbroker.EndpointEnv {
				return f.broker.endpoint
			}
			return os.Getenv(key)
		},
		Stdin: strings.NewReader(""), Stdout: &out, Stderr: &stderr,
		OriginRepo: func() string { return "o/r" },
	})
	return code, out.String(), stderr.String()
}

func TestProductionStandingWritePolicyRefusesWritesAndKeepsReads(t *testing.T) {
	userConfig := `{"brokered":{"github":{"sets":[{"name":"read-only","mode":"standing"},{"name":"read-write","mode":"standing"}]}}}`
	fixture := newProductionGitHubFixture(t, userConfig)
	if code, out, stderr := fixture.forward("pr", "view", "41", "--repo", "o/r"); code != 0 || !strings.Contains(out, "fake GitHub receipt") {
		t.Fatalf("standing read changed: exit %d, stdout %q, stderr %q", code, out, stderr)
	}
	code, _, stderr := fixture.forward("pr", "comment", "41", "--repo", "o/r", "--body", "must-not-run")
	if code != ghbroker.ExitNoPerm || !strings.Contains(stderr, "brokered.github.sets") || !strings.Contains(stderr, "remove") {
		t.Fatalf("standing write policy was not refused with a repair step: exit %d, stderr %q", code, stderr)
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
	fixture := newProductionGitHubFixture(t, "")

	for _, tc := range []struct {
		name, command, receipt string
		argv                   []string
	}{
		{name: "PR comment", command: "comment", argv: []string{"pr", "comment", "41", "--repo", "o/r", "--body", "approved-comment"}, receipt: "pr comment --repo=o/r --body=approved-comment 41"},
		{name: "issue close", command: "close", argv: []string{"issue", "close", "7", "--repo", "o/r"}, receipt: "issue close --repo=o/r 7"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			type result struct {
				code        int
				out, stderr string
			}
			finished := make(chan result, 1)
			go func() {
				var out, stderr bytes.Buffer
				code := ghbroker.Forward(tc.argv, ghbroker.ForwardEnv{
					Getenv: func(key string) string {
						if key == ghbroker.EndpointEnv {
							return fixture.broker.endpoint
						}
						return os.Getenv(key)
					},
					Stdin: strings.NewReader(""), Stdout: &out, Stderr: &stderr,
					OriginRepo: func() string { return "o/r" },
				})
				finished <- result{code: code, out: out.String(), stderr: stderr.String()}
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
			if !strings.Contains(string(data), tc.receipt) {
				t.Fatalf("fake host gh receipt lacks exact approved action %q: %q", tc.receipt, data)
			}
		})
	}
}
