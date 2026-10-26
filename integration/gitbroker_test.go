package integration

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/brokeraudit"
	"github.com/mschulkind-oss/yolo-jail/internal/config"
	"github.com/mschulkind-oss/yolo-jail/internal/packsrc"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
	naming "github.com/mschulkind-oss/yolo-jail/internal/runtime"
	"github.com/mschulkind-oss/yolo-jail/internal/testsupport"
)

// THE GIT BROKER, END TO END (docs/design/git-broker.md, git-broker-plan.md task 7).
//
// A real jail selecting the `git` pack, whose user config grants read access to every
// workspace (boundary-broker.md BB-D105, git-broker.md GB-D15), runs the image's real `git fetch`
// and `git push` against a workspace whose origin is `git@fixture.test:o/r.git`. The pack's `env`
// contribution, `served_by` the loophole, rewrites the remote to `yolo::git@…`, git runs the
// pack's `git-remote-yolo` intercept, which forwards to `yolo git-remote`, and the host broker
// runs the host's `ssh`. Nothing reaches a network: the host's `ssh` here is a FAKE first on the
// launcher's PATH, which the per-jail daemon inherits, and which runs git's remote command
// against a bare repository on the host.
//
// > [!WARNING]
// > Under podman-in-podman the CLI forces `--net=host`, the one mode in which a
// > loopback-forwarding bug cannot reproduce (reachability_test.go's header). A green nested
// > run proves the chain is WIRED; only a real jail on a rootless host, or CI, proves the hop.

// gitBrokerFixture is one prepared workspace: a git checkout whose origin is
// git@fixture.test:o/r.git, a user config selecting the pack, a private state dir, a bare
// repository on the host with one commit on main, and a fake host `ssh` first on the launcher's
// PATH serving it.
type gitBrokerFixture struct {
	dir      string
	opts     []runOption
	bare     string
	argvLog  string
	upstream string // the bare repository's main at the start
}

// hostGit runs the host's git in dir, hermetically.
func hostGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = testsupport.HermeticGitEnv(packsrc.CleanGitEnv(os.Environ()))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func newGitBrokerFixture(t *testing.T, read bool) gitBrokerFixture {
	t.Helper()
	if read {
		return newGitBrokerFixtureWith(t, `{"packs": ["git"], "brokered": {"git": {"grants": [`+gitReadGrant+`]}}}`, `{}`)
	}
	return newGitBrokerFixtureWith(t, `{"packs": ["git"]}`, `{}`)
}

// gitReadGrant is the user config's read grant for every workspace (BB-D107): selecting the pack
// grants nothing without it.
const gitReadGrant = `{"kind": "read", "repo": "*"}`

// newGitBrokerFixtureWith is newGitBrokerFixture with the user config and the workspace config
// given.
func newGitBrokerFixtureWith(t *testing.T, userConfig, workspaceConfig string) gitBrokerFixture {
	t.Helper()
	requireJail(t)
	if rt := detectRuntime(); rt != "podman" {
		t.Skipf("the git-broker chain needs podman, and this runtime is %q", rt)
	}
	// The upstream: a bare repository at <srv>/o/r.git with one commit on main.
	srv := t.TempDir()
	seed := t.TempDir()
	hostGit(t, seed, "init", "-q", "-b", "main")
	hostGit(t, seed, "-c", "user.name=it", "-c", "user.email=it@example.test", "commit", "-q", "--allow-empty", "-m", "seed")
	bare := filepath.Join(srv, "o", "r.git")
	if err := os.MkdirAll(filepath.Dir(bare), 0o755); err != nil {
		t.Fatal(err)
	}
	hostGit(t, srv, "clone", "-q", "--bare", seed, bare)
	upstream := hostGit(t, bare, "rev-parse", "refs/heads/main")

	dir := writeProject(t, workspaceConfig)
	hostGit(t, dir, "init", "-q", "-b", "main")
	hostGit(t, dir, "remote", "add", "origin", "git@fixture.test:o/r.git")

	packHome(t, userConfig)
	macArchivePrivateState(t)

	// The fake host ssh: it records its argv, then runs its last argument (git's remote
	// command) from the directory holding o/r.git, as sshd would in the login's home. It answers
	// `ssh -V` as OpenSSH does, since the loophole's self-check fails an ssh that does not run.
	bin := t.TempDir()
	argvLog := filepath.Join(bin, "argv.log")
	script := "#!/bin/sh\n" +
		"printf '%s\\n' \"$*\" >> '" + argvLog + "'\n" +
		"[ \"$1\" = -V ] && { echo 'OpenSSH_9.9p1 fixture' >&2; exit 0; }\n" +
		"for a in \"$@\"; do last=$a; done\n" +
		"PATH=\"$(git --exec-path):$PATH\"\n" +
		"cd '" + srv + "' || exit 1\n" +
		"exec sh -c \"$last\"\n"
	if err := os.WriteFile(filepath.Join(bin, "ssh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	env := []string{
		"PATH=" + bin + string(os.PathListSeparator) + os.Getenv("PATH"),
		"YOLO_NO_AUTO_IMAGE_REAP=1",
	}
	return gitBrokerFixture{dir: dir, opts: []runOption{withEnv(env...)}, bare: bare, argvLog: argvLog, upstream: upstream}
}

// recordScope approves the workspace's remotes from the host, as a human would with `yolo check
// --accept-config-changes`, and fails unless the check says it recorded want. A [FAIL] naming
// another jail on the machine is set aside, as githubBrokerFixture.recordScope does.
func (fx gitBrokerFixture) recordScope(t *testing.T, want string) {
	t.Helper()
	r := runCommand(t, fx.dir, []string{"check", "--no-build", config.AcceptConfigChangesFlag},
		append(fx.opts, withHostSemantics())...)
	if line := "git-broker repository scope recorded: " + want; !strings.Contains(r.combined(), line) {
		t.Fatalf("yolo check --accept-config-changes did not record %q:\nrc %d\n%s", line, r.rc, r.combined())
	}
	if r.rc != 0 {
		own, foreign := foreignJailFails(r.stdout, naming.FromWorkspace(fx.dir))
		if r.rc != 1 || len(own) > 0 || len(foreign) == 0 {
			t.Fatalf("yolo check --accept-config-changes recorded the scope and then exited %d:\n%s", r.rc, r.combined())
		}
	}
}

// gitBrokerDaemonLog is the host daemon's log, for a failure message.
func gitBrokerDaemonLog(t *testing.T) string {
	t.Helper()
	p := filepath.Join(paths.GlobalStorageUnder(os.Getenv("HOME")), "logs", "host-service-git-broker.log")
	b, err := os.ReadFile(p)
	if err != nil {
		return "\n(no host daemon log at " + p + ": " + err.Error() + ")"
	}
	return "\n--- " + p + " ---\n" + string(b)
}

// gitInJail is the jail half's git: the workspace is the host's, so its owner may not be the
// jail's user, and a commit needs an identity the jail may not have.
const gitInJail = `g() { git -c safe.directory='*' -c user.name=it -c user.email=it@example.test "$@"; }` + "\n"

func TestGitBrokerFetchAndRefusal(t *testing.T) {
	fx := newGitBrokerFixture(t, true)
	fx.recordScope(t, "git@fixture.test:o/r.git")

	script := gitInJail + ghScript(
		"env | grep -c '^GIT_CONFIG_COUNT=3$'", // A: the rewrite is in the env
		"g fetch origin",                       // B: a read
		"g rev-parse refs/remotes/origin/main", // C: what it fetched
		"g checkout -q -b work origin/main && g commit -q --allow-empty -m agent", // D
		"g push origin HEAD:main", // E: refused
	)
	r := runCommand(t, fx.dir, []string{"run", "--", "bash", "-lc", script}, fx.opts...)
	if r.rc != 0 {
		t.Fatalf("the launch failed: rc %d\n%s%s", r.rc, r.combined(), gitBrokerDaemonLog(t))
	}
	out := r.combined()
	for k, want := range map[string]string{"RCA": "0", "RCB": "0", "RCC": "0", "RCD": "0", "RCE": "1"} {
		if got := kvLine(r.stdout, k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
	if !strings.Contains(r.stdout, fx.upstream) {
		t.Errorf("the fetch did not bring the upstream's main %s:\n%s", fx.upstream, out)
	}
	for _, want := range []string{"! [remote rejected]", "needs approval, which this version cannot ask for"} {
		if !strings.Contains(out, want) {
			t.Errorf("the refused push does not say %q:\n%s", want, out)
		}
	}
	if t.Failed() {
		t.Fatalf("output:\n%s%s", out, gitBrokerDaemonLog(t))
	}
	if got := hostGit(t, fx.bare, "rev-parse", "refs/heads/main"); got != fx.upstream {
		t.Errorf("the upstream's main moved to %s after a refused push", got)
	}
	logged, _ := os.ReadFile(fx.argvLog)
	if !strings.Contains(string(logged), "git@fixture.test git-upload-pack 'o/r.git'") ||
		!strings.Contains(string(logged), "ClearAllForwardings=yes") {
		t.Errorf("the host ssh was not run as the broker runs it:\n%s", logged)
	}

	// `yolo audit --service git` on the host: one read and one refused push.
	a := runCommand(t, fx.dir, []string{"audit", "--json", "--service", "git"}, withHostSemantics())
	var sets []string
	for _, line := range strings.Split(strings.TrimSpace(a.stdout), "\n") {
		var e brokeraudit.Event
		if json.Unmarshal([]byte(line), &e) == nil {
			sets = append(sets, e.Set)
		}
	}
	if strings.Join(sets, ",") != "read,refused" {
		t.Errorf("yolo audit --service git listed sets %v, want [read refused]:\n%s", sets, a.combined())
	}
}

// With the pack selected and NO read grant the broker runs and grants nothing (BB-D104, BB-D108,
// BB-D110): this fixture uses SSH, so its fetch and push are refused naming the grant, with the
// host's ssh never run. Anonymous public HTTPS fetch is covered through the fixture HTTPS broker
// in internal/gitbroker/https_test.go.
func TestGitBrokerWithNoGrantRefusesTheFetch(t *testing.T) {
	fx := newGitBrokerFixture(t, false)
	script := gitInJail + ghScript(
		"env | grep -c '^GIT_CONFIG_COUNT=3$'",
		"g fetch origin",
	)
	r := runCommand(t, fx.dir, []string{"run", "--", "bash", "-lc", script}, fx.opts...)
	if r.rc != 0 {
		t.Fatalf("the launch failed: rc %d\n%s%s", r.rc, r.combined(), gitBrokerDaemonLog(t))
	}
	if got := kvLine(r.stdout, "RCA"); got != "0" {
		t.Errorf("the rewrite is not in the env (grep exited %q):\n%s", got, r.combined())
	}
	if got := kvLine(r.stdout, "RCB"); got == "0" || got == "" {
		t.Errorf("git fetch exited %q with no read grant, want a failure:\n%s", got, r.combined())
	}
	for _, want := range []string{"holds no read grant", `"kind": "read"`, "yolo config apply"} {
		if !strings.Contains(r.combined(), want) {
			t.Errorf("the refused fetch does not say %q:\n%s", want, r.combined())
		}
	}
	if logged, _ := os.ReadFile(fx.argvLog); len(logged) != 0 {
		t.Errorf("the host ssh ran with no read grant:\n%s", logged)
	}
}

// CREATE-BRANCH AND THE WORKING SET, END TO END (git-broker-plan.md task 9). The user's own
// config allows the kind, the workspace config names the grant, and both join at the host's
// `yolo check --accept-config-changes`. A new branch pushed from the jail lands in the bare
// repository and joins the workspace's working set; a second push lands; the jail restarts (a
// new launch, a new broker) and a third push lands, on the working set alone; then `yolo approve
// --revoke all --workspace <ws>` on the host ends the entry, and a fourth push is refused.
func TestGitBrokerCreateBranch(t *testing.T) {
	fx := newGitBrokerFixtureWith(t,
		`{"packs": ["git"], "brokered": {"git": {"grant_kinds": ["create-branch"], "grants": [`+gitReadGrant+`]}}}`,
		`{"brokered": {"git": {"grants": [{"kind": "create-branch", "repo": "fixture.test/o/r"}]}}}`)
	fx.recordScope(t, "git@fixture.test:o/r.git")
	const ref = "refs/heads/agent/x"

	// launch runs one jail and its pushes; it returns the combined output (git reports a rejected ref on stderr) and the jail's HEAD.
	launch := func(step string, cmds ...string) (string, string) {
		t.Helper()
		cmds = append(cmds, "g rev-parse HEAD | sed 's/^/HEAD=/'")
		r := runCommand(t, fx.dir, []string{"run", "--", "bash", "-lc", gitInJail + ghScript(cmds...)}, fx.opts...)
		t.Logf("%s: rc %d\n%s", step, r.rc, r.combined())
		if r.rc != 0 {
			t.Fatalf("%s: the launch failed: rc %d\n%s%s", step, r.rc, r.combined(), gitBrokerDaemonLog(t))
		}
		return r.combined(), kvLine(r.stdout, "HEAD")
	}
	landed := func(step, head string) {
		t.Helper()
		if got := hostGit(t, fx.bare, "rev-parse", ref); got != head || head == "" {
			t.Fatalf("%s: the upstream's %s is %s, want the jail's HEAD %q%s", step, ref, got, head, gitBrokerDaemonLog(t))
		}
	}

	// 1 and 2: the creation under the grant, then an update the working set covers.
	out, head := launch("first launch",
		"g fetch -q origin && g checkout -q -b work origin/main && g commit -q --allow-empty -m one",
		"g push -q origin HEAD:"+ref,
		"g commit -q --allow-empty -m two && g push -q origin HEAD:"+ref)
	if kvLine(out, "RCB") != "0" || kvLine(out, "RCC") != "0" {
		t.Fatalf("first launch: the pushes failed:\n%s%s", out, gitBrokerDaemonLog(t))
	}
	landed("second push", head)

	// The host lists the entry.
	listing := runCommand(t, fx.dir, []string{"approve"}, withHostSemantics())
	t.Logf("yolo approve:\n%s", listing.combined())
	if listing.rc != 0 || !strings.Contains(listing.stdout, "working set  git") ||
		!strings.Contains(listing.stdout, "fixture.test/o/r") || !strings.Contains(listing.stdout, ref) {
		t.Fatalf("yolo approve does not list the working-set entry: rc %d\n%s", listing.rc, listing.combined())
	}

	// 3: a new launch, and only the working set covers the push.
	out, head = launch("second launch", "g commit -q --allow-empty -m three && g push -q origin HEAD:"+ref)
	if kvLine(out, "RCA") != "0" {
		t.Fatalf("after the restart, the push failed:\n%s%s", out, gitBrokerDaemonLog(t))
	}
	landed("third push", head)

	// 4: the host revokes the workspace's grants and working set, and the push is refused.
	rv := runCommand(t, fx.dir, []string{"approve", "--revoke", "all", "--workspace", fx.dir}, withHostSemantics())
	t.Logf("yolo approve --revoke all --workspace: rc %d\n%s", rv.rc, rv.combined())
	if rv.rc != 0 {
		t.Fatalf("yolo approve --revoke all --workspace: rc %d\n%s", rv.rc, rv.combined())
	}
	before := hostGit(t, fx.bare, "rev-parse", ref)
	out, _ = launch("third launch", "g commit -q --allow-empty -m four && g push origin HEAD:"+ref)
	if kvLine(out, "RCA") != "1" || !strings.Contains(out, "needs approval, which this version cannot ask for") {
		t.Fatalf("after the revoke, the push was not refused:\n%s%s", out, gitBrokerDaemonLog(t))
	}
	if got := hostGit(t, fx.bare, "rev-parse", ref); got != before {
		t.Fatalf("the upstream's %s moved to %s after the revoke", ref, got)
	}

	// The audit names what covered each push.
	a := runCommand(t, fx.dir, []string{"audit", "--json", "--service", "git"}, withHostSemantics())
	var sets []string
	for _, line := range strings.Split(strings.TrimSpace(a.stdout), "\n") {
		var e brokeraudit.Event
		if json.Unmarshal([]byte(line), &e) == nil && len(e.Argv) == 1 && e.Argv[0] == "git-receive-pack" {
			sets = append(sets, e.Set)
		}
	}
	if strings.Join(sets, ",") != "create-branch,working-set,working-set,refused" {
		t.Errorf("the pushes' audit sets are %v, want [create-branch working-set working-set refused]:\n%s", sets, a.combined())
	}
}

// A BRANCH DECLARED IN CONFIG, END TO END (docs/design/git-broker.md GB-D12). The user's own
// config allows the kind, the workspace config declares main as a working-set branch, and both
// join at the host's `yolo check --accept-config-changes`: a jail's push to main lands in the
// bare repository, audited as the working set, covered by the grant. A second workspace whose
// user config allows the kind but whose workspace config declares no branch is refused the same
// push, and its main does not move.
func TestGitBrokerConfigBranch(t *testing.T) {
	const userCfg = `{"packs": ["git"], "brokered": {"git": {"grant_kinds": ["branch"], "grants": [` + gitReadGrant + `]}}}`
	const push = "g fetch -q origin && g checkout -q -b work origin/main && g commit -q --allow-empty -m agent && g push origin HEAD:main"
	launch := func(fx gitBrokerFixture, step string) (string, string) {
		t.Helper()
		r := runCommand(t, fx.dir, []string{"run", "--", "bash", "-lc", gitInJail + ghScript(push, "g rev-parse HEAD | sed 's/^/HEAD=/'")}, fx.opts...)
		t.Logf("%s: rc %d\n%s", step, r.rc, r.combined())
		if r.rc != 0 {
			t.Fatalf("%s: the launch failed: rc %d\n%s%s", step, r.rc, r.combined(), gitBrokerDaemonLog(t))
		}
		return r.combined(), kvLine(r.stdout, "HEAD")
	}

	fx := newGitBrokerFixtureWith(t, userCfg,
		`{"brokered": {"git": {"grants": [{"kind": "branch", "repo": "fixture.test/o/r", "branch": "main"}]}}}`)
	fx.recordScope(t, "git@fixture.test:o/r.git")
	out, head := launch(fx, "with the grant")
	if kvLine(out, "RCA") != "0" {
		t.Fatalf("with the grant, the push to main failed:\n%s%s", out, gitBrokerDaemonLog(t))
	}
	if got := hostGit(t, fx.bare, "rev-parse", "refs/heads/main"); got != head || head == "" || got == fx.upstream {
		t.Fatalf("the upstream's main is %s, want the jail's HEAD %q%s", got, head, gitBrokerDaemonLog(t))
	}
	a := runCommand(t, fx.dir, []string{"audit", "--json", "--service", "git"}, withHostSemantics())
	var covered []string
	for _, line := range strings.Split(strings.TrimSpace(a.stdout), "\n") {
		var e brokeraudit.Event
		if json.Unmarshal([]byte(line), &e) == nil && len(e.Argv) == 1 && e.Argv[0] == "git-receive-pack" {
			for _, ref := range e.Refs {
				covered = append(covered, e.Set+"/"+ref.CoveredBy)
			}
		}
	}
	if strings.Join(covered, ",") != "working-set/branch:main" {
		t.Errorf("the push's audit is %v, want [working-set/branch:main]:\n%s", covered, a.combined())
	}

	without := newGitBrokerFixtureWith(t, userCfg, `{}`)
	without.recordScope(t, "git@fixture.test:o/r.git")
	out, _ = launch(without, "without the grant")
	if kvLine(out, "RCA") != "1" || !strings.Contains(out, "needs approval, which this version cannot ask for") {
		t.Fatalf("without the grant, the push to main was not refused:\n%s%s", out, gitBrokerDaemonLog(t))
	}
	if got := hostGit(t, without.bare, "rev-parse", "refs/heads/main"); got != without.upstream {
		t.Fatalf("without the grant, the upstream's main moved to %s", got)
	}
}
