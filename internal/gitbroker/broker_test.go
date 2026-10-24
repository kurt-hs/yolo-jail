package gitbroker

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/brokeraudit"
	"github.com/mschulkind-oss/yolo-jail/internal/brokerscope"
	"github.com/mschulkind-oss/yolo-jail/internal/gitproto"
	"github.com/mschulkind-oss/yolo-jail/internal/gitremote"
	"github.com/mschulkind-oss/yolo-jail/internal/hostservice"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
	"github.com/mschulkind-oss/yolo-jail/internal/svcendpoint"
)

// TestHelperProcess is the jail's remote helper when the fixture's git runs
// `git-remote-yolo`: the script on git's PATH re-runs this test binary with GO_HELPER=1.
func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_HELPER") != "1" {
		return
	}
	args := os.Args
	for i, a := range args {
		if a == "--" {
			args = args[i+1:]
			break
		}
	}
	os.Exit(gitremote.Run(args, gitremote.Env{Getenv: os.Getenv, Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr}))
}

const origin = "git@fixture.test:o/r.git"

// lockedBuffer is the broker's log, written from several goroutines.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

type fixture struct {
	t         *testing.T
	root      string
	bare      string // srv/o/r.git
	ws        string
	bin       string // the broker's fake ssh
	helperBin string // git-remote-yolo
	record    string // where the fake ssh records its argv and env
	scopeFile string
	endpoint  string
	broker    *Broker
	log       *lockedBuffer
}

// sshScript is the fake host ssh: it records its argv and env, then runs its last argument
// (git's remote command) with sh from the directory holding o/r.git, as sshd would run it in
// the login's home.
//
// A body of "tee" runs git's remote command the same way, recording every byte the broker
// sent the upstream in record/stdin.
func sshScript(record, srv, body string) string {
	if body == "tee" {
		body = `PATH="$(git --exec-path):$PATH"
cd ` + srv + ` || exit 1
tee ` + record + `/stdin | sh -c "$last"`
	}
	if body == "" {
		body = `PATH="$(git --exec-path):$PATH"
cd ` + srv + ` || exit 1
exec sh -c "$last"`
	}
	return `#!/bin/sh
printf '%s\n' "$@" >> ` + record + `/argv
env > ` + record + `/env
for a in "$@"; do last=$a; done
` + body + "\n"
}

func gitEnv(extra ...string) []string {
	var env []string
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "GIT_") || strings.HasPrefix(kv, "YOLO_SERVICE_") {
			continue
		}
		env = append(env, kv)
	}
	return append(env, append([]string{
		"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0",
		"GIT_AUTHOR_NAME=fixture", "GIT_AUTHOR_EMAIL=f@x", "GIT_COMMITTER_NAME=fixture", "GIT_COMMITTER_EMAIL=f@x",
	}, extra...)...)
}

func run(t *testing.T, dir string, env []string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func mustGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := run(t, dir, gitEnv(), args...)
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(out)
}

func writeExec(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}

// newFixture builds the bare repository, the workspace clone, the fake ssh, the scope file
// approving repos, and a broker serving an endpoint. sshBody, when set, replaces the fake
// ssh's default of running git's remote command.
func newFixture(t *testing.T, sshBody string, repos ...string) *fixture {
	t.Helper()
	return newFixtureOpts(t, fixtureOpts{sshBody: sshBody, repos: repos})
}

type fixtureOpts struct {
	sshBody string
	repos   []string
	// sshInWorkspace puts the fake ssh inside the workspace, where an agent can rewrite it.
	sshInWorkspace bool
}

func newFixtureOpts(t *testing.T, o fixtureOpts) *fixture {
	t.Helper()
	sshBody, repos := o.sshBody, o.repos
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git on PATH")
	}
	t.Setenv("HOME", t.TempDir())
	t.Setenv("YOLO_USER_LAYER", "")
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{t: t, root: root, bare: filepath.Join(root, "srv", "o", "r.git"), ws: filepath.Join(root, "ws"),
		bin: filepath.Join(root, "bin"), helperBin: filepath.Join(root, "helperbin"), record: filepath.Join(root, "record"),
		log: &lockedBuffer{}}
	for _, d := range []string{filepath.Dir(f.bare), f.record} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	mustGit(t, root, "init", "-q", "--bare", "-b", "main", f.bare)
	seed := filepath.Join(root, "seed")
	mustGit(t, root, "init", "-q", "-b", "main", seed)
	if err := os.WriteFile(filepath.Join(seed, "README"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, seed, "add", "README")
	mustGit(t, seed, "commit", "-q", "-m", "seed")
	mustGit(t, seed, "push", "-q", f.bare, "main")
	mustGit(t, root, "clone", "-q", f.bare, f.ws)
	mustGit(t, f.ws, "remote", "set-url", "origin", origin)
	if o.sshInWorkspace {
		f.bin = filepath.Join(f.ws, ".tools")
	}

	writeExec(t, filepath.Join(f.bin, "ssh"), sshScript(f.record, filepath.Join(root, "srv"), sshBody))
	writeExec(t, filepath.Join(f.helperBin, "git-remote-yolo"),
		"#!/bin/sh\nexec \"$TEST_BINARY\" -test.run='^TestHelperProcess$' -- \"$@\"\n")

	if len(repos) == 0 {
		repos = []string{origin}
	}
	sf := brokerscope.File{Source: Source, LaunchID: "fixture-launch", PID: os.Getpid(), Container: "fixture-container",
		Workspace: f.ws, Form: brokerscope.FormRemoteURL, Repos: repos}
	f.scopeFile, err = brokerscope.Write(sf)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	brokerPath := f.bin + string(os.PathListSeparator) + os.Getenv("PATH")
	getenv := func(k string) string {
		if k == "PATH" {
			return brokerPath
		}
		return os.Getenv(k)
	}
	b, cleanup, err := newBroker(ctx, f.scopeFile, sf, f.ws, f.log, getenv)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	f.broker = b
	f.endpoint = serve(t, b.Handle)
	return f
}

// serve starts an in-test loopback-TLS endpoint running handler and returns its endpoint file.
func serve(t *testing.T, handler hostservice.Handler) string {
	t.Helper()
	t.Setenv(svcendpoint.AdvertiseHostEnv, "127.0.0.1")
	dir, err := os.MkdirTemp("/tmp", "yj-gitbroker-test-")
	if err != nil {
		t.Fatal(err)
	}
	endpoint := filepath.Join(dir, "git-broker.endpoint")
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = hostservice.ServeEndpoint(handler, endpoint, stop)
	}()
	for i := 0; i < 200; i++ {
		if _, err := os.Stat(endpoint); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Cleanup(func() {
		close(stop)
		<-done
		_ = os.RemoveAll(dir)
	})
	return endpoint
}

// git runs the real git in the workspace the way the jail's does: every git@ remote
// rewritten to the helper, which reaches the fixture's endpoint.
func (f *fixture) git(args ...string) (string, error) {
	f.t.Helper()
	env := gitEnv("GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=url.yolo::git@.insteadOf", "GIT_CONFIG_VALUE_0=git@",
		"PATH="+f.helperBin+string(os.PathListSeparator)+os.Getenv("PATH"),
		gitremote.EndpointEnv+"="+f.endpoint, "GO_HELPER=1", "TEST_BINARY="+os.Args[0])
	return run(f.t, f.ws, env, args...)
}

// helper runs the jail's helper in-process against the fixture for url and service, with
// git's bytes after the connect line, and returns its exit code, stdout and stderr.
func (f *fixture) helper(url, service string, gitBytes []byte) (int, string, string) {
	f.t.Helper()
	var out, errb bytes.Buffer
	in := append([]byte("capabilities\nconnect "+service+"\n"), gitBytes...)
	rc := gitremote.Run([]string{"origin", url}, gitremote.Env{
		Getenv: func(k string) string {
			if k == gitremote.EndpointEnv {
				return f.endpoint
			}
			return ""
		},
		Stdin: bytes.NewReader(in), Stdout: &out, Stderr: &errb})
	return rc, out.String(), errb.String()
}

func (f *fixture) sshRan() bool {
	_, err := os.Stat(filepath.Join(f.record, "argv"))
	return err == nil
}

func (f *fixture) setScope(repos ...string) {
	f.t.Helper()
	sf, err := brokerscope.ReadFile(f.scopeFile)
	if err != nil {
		f.t.Fatal(err)
	}
	sf.Repos = repos
	if _, err := brokerscope.Write(sf); err != nil {
		f.t.Fatal(err)
	}
}

func auditEvents(t *testing.T) []brokeraudit.Event {
	t.Helper()
	data, err := os.ReadFile(paths.BrokerAuditLog())
	if err != nil {
		t.Fatalf("no audit log: %v", err)
	}
	var evs []brokeraudit.Event
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var e brokeraudit.Event
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Fatalf("audit line %q: %v", line, err)
		}
		evs = append(evs, e)
	}
	return evs
}

func auditRaw(t *testing.T) string {
	t.Helper()
	data, _ := os.ReadFile(paths.BrokerAuditLog())
	return string(data)
}

func TestFetchThroughTheBroker(t *testing.T) {
	f := newFixture(t, "")
	out, err := f.git("fetch", "origin")
	if err != nil {
		t.Fatalf("git fetch: %v\n%s", err, out)
	}
	if got, want := mustGit(t, f.ws, "rev-parse", "refs/remotes/origin/main"), mustGit(t, f.bare, "rev-parse", "main"); got != want {
		t.Fatalf("origin/main %s, bare main %s", got, want)
	}
	argv, err := os.ReadFile(filepath.Join(f.record, "argv"))
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(strings.Fields(strings.ReplaceAll(string(argv), "\n", " ")), " ")
	if !strings.HasSuffix(joined, "fixture.test git-upload-pack 'o/r.git'") {
		t.Fatalf("ssh argv %q", joined)
	}
	if !strings.Contains(joined, "-o ClearAllForwardings=yes") {
		t.Fatalf("ssh argv %q has no ClearAllForwardings", joined)
	}
	evs := auditEvents(t)
	if len(evs) != 1 {
		t.Fatalf("audit events %+v", evs)
	}
	raw := auditRaw(t)
	for _, want := range []string{`"service":"git"`, `"set":"read"`, `"repo":"fixture.test/o/r"`} {
		if !strings.Contains(raw, want) {
			t.Fatalf("audit log lacks %s:\n%s", want, raw)
		}
	}
	if evs[0].Remote != origin {
		t.Fatalf("audit remote %q", evs[0].Remote)
	}
}

func (f *fixture) localCommit() {
	f.t.Helper()
	if err := os.WriteFile(filepath.Join(f.ws, "new"), []byte("new\n"), 0o644); err != nil {
		f.t.Fatal(err)
	}
	mustGit(f.t, f.ws, "add", "new")
	mustGit(f.t, f.ws, "commit", "-q", "-m", "local")
}

func TestPushRefusedNothingForwarded(t *testing.T) {
	f := newFixture(t, "tee")
	before := mustGit(t, f.bare, "rev-parse", "main")
	f.localCommit()
	out, err := f.git("push", "origin", "HEAD:main")
	if ee, ok := err.(*exec.ExitError); !ok || ee.ExitCode() != 1 {
		t.Fatalf("git push: %v (want exit 1)\n%s", err, out)
	}
	t.Logf("refused push output:\n%s", out)
	for _, want := range []string{"! [remote rejected]", "needs approval, which this version cannot ask for"} {
		if !strings.Contains(out, want) {
			t.Fatalf("push output lacks %q:\n%s", want, out)
		}
	}
	if after := mustGit(t, f.bare, "rev-parse", "main"); after != before {
		t.Fatalf("bare main moved %s -> %s", before, after)
	}
	// The upstream was sent an empty command list and nothing else.
	if sent, err := os.ReadFile(filepath.Join(f.record, "stdin")); err != nil || string(sent) != "0000" {
		t.Fatalf("the upstream received %q (%v), want exactly \"0000\"", sent, err)
	}
	var push *brokeraudit.Event
	evs := auditEvents(t)
	for i, e := range evs {
		if len(e.Argv) == 1 && e.Argv[0] == "git-receive-pack" {
			push = &evs[i]
		}
	}
	if push == nil || push.Set != "refused" || len(push.Refs) != 1 || push.Refs[0].Ref != "refs/heads/main" {
		t.Fatalf("push audit %+v", push)
	}
}

func TestPushOutsideHeadsAndTagsRefused(t *testing.T) {
	f := newFixture(t, "")
	f.localCommit()
	out, err := f.git("push", "origin", "HEAD:refs/notes/x")
	if err == nil {
		t.Fatalf("push to refs/notes/x succeeded:\n%s", out)
	}
	if !strings.Contains(out, "! [remote rejected]") || !strings.Contains(out, "refs/heads/") || !strings.Contains(out, "refs/tags/") {
		t.Fatalf("push output:\n%s", out)
	}
	if _, err := run(t, f.bare, gitEnv(), "rev-parse", "--verify", "-q", "refs/notes/x"); err == nil {
		t.Fatal("refs/notes/x reached the upstream")
	}
}

func TestOutOfScopeRefused(t *testing.T) {
	f := newFixture(t, "")
	mustGit(t, f.ws, "remote", "add", "other", "git@other.test:o/r.git")
	out, err := f.git("fetch", "other")
	if err == nil {
		t.Fatalf("out-of-scope fetch succeeded:\n%s", out)
	}
	if !strings.Contains(out, "approved scope") || !strings.Contains(out, "config-change gate") {
		t.Fatalf("fetch output:\n%s", out)
	}
	if f.sshRan() {
		t.Fatal("the fake ssh ran for an out-of-scope remote")
	}
	if !strings.Contains(auditRaw(t), `"set":"out-of-scope"`) {
		t.Fatalf("no out-of-scope audit event:\n%s", auditRaw(t))
	}
}

func TestSSHHostStartingWithDashRefused(t *testing.T) {
	const bad = "ssh://-oProxyCommand=x/r"
	f := newFixture(t, "", bad)
	rc, stdout, stderr := f.helper(bad, "git-upload-pack", nil)
	if rc == 0 || stdout != "connect\n\n" {
		t.Fatalf("rc %d stdout %q stderr %q", rc, stdout, stderr)
	}
	if f.sshRan() {
		t.Fatal("the fake ssh ran for a host starting with -")
	}
}

func TestOnlySSHAndHTTPSTransports(t *testing.T) {
	f := newFixture(t, "")
	for _, bad := range []string{"file:///srv/r.git", "ext::sh -c x", "ext::sh", "fd::3", "-oProxyCommand=x:r",
		"git@fixture.test:-oFoo", "git+ssh://fixture.test/o/r", "http://fixture.test/o/r.git", "/srv/o/r.git"} {
		f.setScope(bad)
		rc, stdout, stderr := f.helper(bad, "git-upload-pack", nil)
		if rc == 0 || stdout != "connect\n\n" {
			t.Errorf("%q: rc %d stdout %q stderr %q", bad, rc, stdout, stderr)
		}
		if f.sshRan() {
			t.Fatalf("%q: the fake ssh ran", bad)
		}
	}
	const https = "https://fixture.test/o/r.git"
	f.setScope(https)
	rc, stdout, stderr := f.helper(https, "git-upload-pack", nil)
	if rc == 0 || stdout != "connect\n\n" || !strings.Contains(stderr, "HTTPS remotes arrive in the next build; use the SSH remote") {
		t.Fatalf("https: rc %d stdout %q stderr %q", rc, stdout, stderr)
	}
	if f.sshRan() {
		t.Fatal("the fake ssh ran for an https remote")
	}
}

func TestUploadArchiveRefused(t *testing.T) {
	f := newFixture(t, "")
	var out, errb bytes.Buffer
	// The helper itself refuses the service; ask the broker directly through a raw session.
	rc := f.broker.serveRequest(context.Background(), request{Service: "git-upload-archive", URL: origin}, "fixture",
		nil, &out, &errb)
	if rc == 0 || f.sshRan() || !strings.Contains(errb.String(), "git-upload-archive") {
		t.Fatalf("rc %d stderr %q", rc, errb.String())
	}
}

// The receive-pack path forwards the bytes the command reader buffered, once judged, and never
// re-encodes them: a shallow line, the client's capability order and push options survive.
func TestReceivePackForwardsTheTeedBytes(t *testing.T) {
	var adv bytes.Buffer
	aw := gitproto.NewWriter(&adv)
	aw.Line("0000000000000000000000000000000000000000 capabilities^{}\x00report-status side-band-64k push-options\n")
	aw.Flush()
	root := t.TempDir()
	advFile, stdinFile := filepath.Join(root, "adv"), filepath.Join(root, "stdin")
	if err := os.WriteFile(advFile, adv.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	f := newFixture(t, "cat "+advFile+"\ncat > "+stdinFile)
	f.broker.cover = func(c gitproto.Command) (string, string, bool) { return "create-branch", "create-branch", true }

	const one, zero = "1111111111111111111111111111111111111111", "0000000000000000000000000000000000000000"
	var push bytes.Buffer
	pw := gitproto.NewWriter(&push)
	pw.Line("shallow 2222222222222222222222222222222222222222\n")
	pw.Line(zero + " " + one + " refs/heads/agent/x\x00side-band-64k report-status push-options agent=git/2.55.0\n")
	pw.Flush()
	pw.Line("ci.skip\n")
	pw.Flush()
	push.WriteString("PACK-not-really-a-pack")
	rc, stdout, stderr := f.helper(origin, "git-receive-pack", push.Bytes())
	if rc != 0 {
		t.Fatalf("rc %d stdout %q stderr %q", rc, stdout, stderr)
	}
	got, err := os.ReadFile(stdinFile)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, push.Bytes()) {
		t.Fatalf("upstream received %q\nwant %q", got, push.Bytes())
	}
	if !strings.HasPrefix(stdout, "connect\n\n\n"+adv.String()) {
		t.Fatalf("client did not get the advertisement: %q", stdout)
	}
}

func TestSSHEnvBuiltFromNothing(t *testing.T) {
	t.Setenv("GH_TOKEN", "leak")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "leak")
	t.Setenv("SSH_AUTH_SOCK", "/tmp/fixture-agent.sock")
	f := newFixture(t, "")
	if out, err := f.git("fetch", "origin"); err != nil {
		t.Fatalf("git fetch: %v\n%s", err, out)
	}
	env, err := os.ReadFile(filepath.Join(f.record, "env"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(env), "SSH_AUTH_SOCK=/tmp/fixture-agent.sock") || strings.Contains(string(env), "leak") {
		t.Fatalf("ssh env:\n%s", env)
	}
}

func TestNoHostCredentialNamesTheFix(t *testing.T) {
	f := newFixture(t, "echo 'Permission denied (publickey).' >&2\nexit 255")
	out, err := f.git("fetch", "origin")
	if err == nil {
		t.Fatalf("fetch succeeded:\n%s", out)
	}
	if !strings.Contains(out, "Permission denied (publickey).") || !strings.Contains(out, "ssh-add -l") {
		t.Fatalf("fetch output:\n%s", out)
	}
}

func TestBrokerLogNamesNoRepository(t *testing.T) {
	// The framework's access lines go to the daemon's stderr too, so they are read with it.
	access := &lockedBuffer{}
	saved := hostservice.Logger
	hostservice.Logger = log.New(access, "", 0)
	t.Cleanup(func() { hostservice.Logger = saved })
	f := newFixture(t, "")
	if out, err := f.git("fetch", "origin"); err != nil {
		t.Fatalf("git fetch: %v\n%s", err, out)
	}
	f.localCommit()
	if out, err := f.git("push", "origin", "HEAD:main"); err == nil {
		t.Fatalf("push succeeded:\n%s", out)
	}
	logText := f.log.String() + access.String()
	if strings.TrimSpace(logText) == "" {
		t.Fatal("the broker logged nothing")
	}
	for _, leak := range []string{"fixture.test", "o/r", f.ws} {
		if strings.Contains(logText, leak) {
			t.Fatalf("broker log names %q:\n%s", leak, logText)
		}
	}
}

func TestSSHArgv(t *testing.T) {
	for _, tc := range []struct {
		url, protocol string
		want          []string
	}{
		{"git@github.com:o/r.git", "", []string{"-x", "-T", "-o", "ClearAllForwardings=yes", "-o", "PermitLocalCommand=no",
			"-o", "ConnectTimeout=30", "--", "git@github.com", "git-upload-pack 'o/r.git'"}},
		{"ssh://git@host:2222/~/repo.git", "version=2", []string{"-x", "-T", "-o", "ClearAllForwardings=yes", "-o", "PermitLocalCommand=no",
			"-o", "ConnectTimeout=30", "-p", "2222", "-o", "SendEnv=GIT_PROTOCOL", "--", "git@host", "git-upload-pack '~/repo.git'"}},
		{"ssh://host/srv/it's.git", "version=9", []string{"-x", "-T", "-o", "ClearAllForwardings=yes", "-o", "PermitLocalCommand=no",
			"-o", "ConnectTimeout=30", "--", "host", `git-upload-pack '/srv/it'\''s.git'`}},
	} {
		got, err := sshArgv(tc.url, "git-upload-pack", tc.protocol)
		if err != nil || strings.Join(got, "\x00") != strings.Join(tc.want, "\x00") {
			t.Errorf("sshArgv(%q) = %q, %v\nwant %q", tc.url, got, err, tc.want)
		}
	}
}
