package hostrun

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/brokeraudit"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
	"github.com/mschulkind-oss/yolo-jail/internal/runtime"
)

var fixedNow = time.Date(2026, 10, 23, 14, 33, 7, 0, time.UTC)

// hostFixture is a workspace with a `.yolo`, a home, a bin/ of fake programs and an audit log,
// all under one symlink-resolved temp root.
type hostFixture struct {
	root, ws, home, bin, audit string
	stdout, stderr             *os.File
	env                        HostEnv
}

func evalTemp(t *testing.T) string {
	t.Helper()
	d, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func writeScript(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

// coreutilsDir is the resolved directory of `sleep`, which the fake programs use; a test skips
// where it sits under a directory holding `.yolo` (the PATH check would refuse it).
func coreutilsDir(t *testing.T) string {
	t.Helper()
	p, err := exec.LookPath("sleep")
	if err != nil {
		t.Skip("no sleep on PATH")
	}
	p, _ = filepath.EvalSymlinks(p)
	d := filepath.Dir(p)
	if yoloAncestor(d) != "" {
		t.Skipf("%s sits under a directory holding .yolo", d)
	}
	return d
}

func newHostFixture(t *testing.T) *hostFixture {
	t.Helper()
	t.Setenv("YOLO_VERSION", "")
	f := &hostFixture{root: evalTemp(t)}
	f.ws = filepath.Join(f.root, "ws")
	f.home = filepath.Join(f.root, "home")
	f.bin = filepath.Join(f.root, "tools", "bin")
	f.audit = filepath.Join(f.root, "audit", "audit.jsonl")
	for _, d := range []string{filepath.Join(f.ws, ".yolo"), f.home, f.bin} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", f.home)
	writeScript(t, filepath.Join(f.bin, "say"), `printf '%s\n' "$*"`)
	writeScript(t, filepath.Join(f.bin, "slow"), `while [ ! -e "$1" ]; do sleep 0.02; done; echo out; echo err >&2`)
	writeScript(t, filepath.Join(f.bin, "hang"), `echo $$ > "$1.tmp"; mv "$1.tmp" "$1"; exec sleep 30`)
	writeScript(t, filepath.Join(f.bin, "mark"), `: > "$1"`)
	var err error
	if f.stdout, err = os.Create(filepath.Join(f.root, "term-out")); err != nil {
		t.Fatal(err)
	}
	if f.stderr, err = os.Create(filepath.Join(f.root, "term-err")); err != nil {
		t.Fatal(err)
	}
	stdin, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.stdout.Close(); f.stderr.Close(); stdin.Close() })
	path := f.bin + string(os.PathListSeparator) + coreutilsDir(t)
	f.env = HostEnv{
		Stdin:   stdin,
		Stdout:  f.stdout,
		Stderr:  f.stderr,
		Environ: func() []string { return []string{"PATH=" + path, "HOME=" + f.home} },
		Now:     func() time.Time { return fixedNow },
		Rand:    bytes.NewReader(bytes.Repeat([]byte{0x5a}, 64)),
		Audit:   brokeraudit.Open(f.audit, nil),
		Version: "test-1",
	}
	return f
}

func (f *hostFixture) setPath(entries ...string) {
	p := strings.Join(entries, string(os.PathListSeparator))
	f.env.Environ = func() []string { return []string{"PATH=" + p, "HOME=" + f.home} }
}

func (f *hostFixture) run(t *testing.T, argv ...string) int {
	t.Helper()
	return RunHost(f.env, Line{Label: "t1", Argv: argv}, f.ws, f.ws)
}

func (f *hostFixture) termErr(t *testing.T) string {
	t.Helper()
	b, _ := os.ReadFile(f.stderr.Name())
	return string(b)
}

func (f *hostFixture) termOut(t *testing.T) string {
	t.Helper()
	b, _ := os.ReadFile(f.stdout.Name())
	return string(b)
}

func (f *hostFixture) mailbox() string { return filepath.Join(f.ws, ".yolo", MailboxName) }

// runDirs lists the mailbox's run directories, by name.
func (f *hostFixture) runDirs(t *testing.T) []string {
	t.Helper()
	ents, err := os.ReadDir(f.mailbox())
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range ents {
		if _, ok := ParseRunID(e.Name()); ok {
			out = append(out, e.Name())
		}
	}
	return out
}

func (f *hostFixture) onlyRun(t *testing.T) string {
	t.Helper()
	d := f.runDirs(t)
	if len(d) != 1 {
		t.Fatalf("want one run directory, got %q", d)
	}
	return filepath.Join(f.mailbox(), d[0])
}

func readJSON(t *testing.T, path string, v any) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, v); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
}

func (f *hostFixture) auditEvents(t *testing.T) []brokeraudit.Event {
	t.Helper()
	ev, _, err := brokeraudit.Read(f.audit)
	if err != nil {
		t.Fatal(err)
	}
	return ev
}

func TestHostRunDeliversStdoutResultAndAudit(t *testing.T) {
	f := newHostFixture(t)
	if rc := f.run(t, "say", "hi"); rc != 0 {
		t.Fatalf("rc %d\n%s", rc, f.termErr(t))
	}
	dir := f.onlyRun(t)
	if got, _ := os.ReadFile(filepath.Join(dir, StdoutFile)); string(got) != "hi\n" {
		t.Fatalf("stdout file %q", got)
	}
	if got := f.termOut(t); got != "hi\n" {
		t.Fatalf("the terminal saw %q", got)
	}
	var res Result
	readJSON(t, filepath.Join(dir, ResultFile), &res)
	if res.Exit == nil || *res.Exit != 0 || res.Label != "t1" || res.Program != filepath.Join(f.bin, "say") ||
		res.Workspace != f.ws || res.Cwd != f.ws || res.StdoutBytes != 3 || res.YoloVersion != "test-1" ||
		!slices.Equal(res.Argv, []string{"say", "hi"}) || res.ID != filepath.Base(dir) {
		t.Fatalf("result %+v", res)
	}
	var req Request
	readJSON(t, filepath.Join(dir, RequestFile), &req)
	if req.ID != res.ID || req.Label != "t1" || req.Program != res.Program {
		t.Fatalf("request %+v", req)
	}
	for _, name := range []string{RequestFile, ResultFile, StdoutFile, StderrFile, HeartbeatFile} {
		fi, err := os.Lstat(filepath.Join(dir, name))
		if err != nil || fi.Mode().Perm() != 0o600 {
			t.Errorf("%s: %v %v", name, fi, err)
		}
	}
	if fi, _ := os.Lstat(dir); fi.Mode().Perm() != 0o700 {
		t.Errorf("run dir mode %v", fi.Mode())
	}
	if fi, _ := os.Lstat(f.mailbox()); fi.Mode().Perm() != 0o700 {
		t.Errorf("mailbox mode %v", fi.Mode())
	}
	errOut := f.termErr(t)
	lines := strings.Split(strings.TrimSpace(errOut), "\n")
	if last := lines[len(lines)-1]; !strings.Contains(last, "Nothing to paste back.") || !strings.Contains(last, res.ID) {
		t.Fatalf("last line %q", last)
	}
	for _, want := range []string{"runs:     say hi", "program:  " + res.Program, "in:       " + f.ws, "agent-writable", "result:   ", "(label t1)"} {
		if !strings.Contains(errOut, want) {
			t.Errorf("disclosure lacks %q:\n%s", want, errOut)
		}
	}
	if strings.Contains(errOut, "env:") {
		t.Errorf("an env: line with none of its variables set:\n%s", errOut)
	}
	ev := f.auditEvents(t)
	if len(ev) != 1 {
		t.Fatalf("audit %+v", ev)
	}
	e := ev[0]
	wantJail := paths.HostServicesDirName(paths.JailShortHash(runtime.FromWorkspace(f.ws)))
	if e.Service != "host-run" || e.Set != "human-run" || e.Outcome != "ran" || e.Request != res.ID ||
		e.Workspace != f.ws || e.Jail != wantJail || e.Exit == nil || *e.Exit != 0 || e.BytesOut != 3 {
		t.Fatalf("audit %+v", e)
	}
}

func TestHostRunWritesNoStreamUntilTheChildExits(t *testing.T) {
	f := newHostFixture(t)
	gate := filepath.Join(f.root, "gate")
	done := make(chan int, 1)
	go func() { done <- f.run(t, "slow", gate) }()
	var dir string
	deadline := time.Now().Add(10 * time.Second)
	for dir == "" && time.Now().Before(deadline) {
		if ents, _ := os.ReadDir(f.mailbox()); len(ents) > 0 {
			for _, e := range ents {
				if _, ok := ParseRunID(e.Name()); ok {
					dir = filepath.Join(f.mailbox(), e.Name())
				}
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	if dir == "" {
		t.Fatal("no run directory appeared")
	}
	if _, err := os.Lstat(filepath.Join(dir, RequestFile)); err != nil {
		t.Fatalf("the run directory appeared without request.json: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(dir, HeartbeatFile)); err != nil {
		t.Fatalf("no heartbeat: %v", err)
	}
	for _, name := range []string{StdoutFile, StderrFile, ResultFile} {
		if _, err := os.Lstat(filepath.Join(dir, name)); err == nil {
			t.Fatalf("%s exists while the child runs", name)
		}
	}
	if err := os.WriteFile(gate, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if rc := <-done; rc != 0 {
		t.Fatalf("rc %d\n%s", rc, f.termErr(t))
	}
	if got, _ := os.ReadFile(filepath.Join(dir, StderrFile)); string(got) != "err\n" {
		t.Fatalf("stderr %q", got)
	}
}

// treeSnapshot lists every path under root with its mode and size.
func treeSnapshot(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		fi, _ := os.Lstat(p)
		out = append(out, p+" "+fi.Mode().String()+" "+strconv.FormatInt(fi.Size(), 10))
		return nil
	})
	return out
}

func TestHostRunRefusesWithoutAUsableYolo(t *testing.T) {
	t.Run("absent", func(t *testing.T) {
		f := newHostFixture(t)
		os.Remove(filepath.Join(f.ws, ".yolo"))
		before := treeSnapshot(t, f.root)
		if rc := f.run(t, "say", "hi"); rc != 64 {
			t.Fatalf("rc %d", rc)
		}
		if e := f.termErr(t); !strings.Contains(e, "launch") || !strings.Contains(e, "nothing ran") {
			t.Fatalf("%s", e)
		}
		after := treeSnapshot(t, f.root)
		// The terminal files grew; nothing else changed.
		strip := func(s []string) []string {
			return slices.DeleteFunc(slices.Clone(s), func(x string) bool { return strings.Contains(x, "term-") })
		}
		if !slices.Equal(strip(before), strip(after)) {
			t.Fatalf("the tree changed:\nbefore %q\nafter  %q", before, after)
		}
		if f.termOut(t) != "" {
			t.Fatal("the command ran")
		}
	})
	t.Run("yolo is a link", func(t *testing.T) {
		f := newHostFixture(t)
		elsewhere := filepath.Join(f.root, "elsewhere")
		os.Mkdir(elsewhere, 0o755)
		os.Remove(filepath.Join(f.ws, ".yolo"))
		os.Symlink(elsewhere, filepath.Join(f.ws, ".yolo"))
		if rc := f.run(t, "say", "hi"); rc != 64 {
			t.Fatalf("rc %d", rc)
		}
		if e := f.termErr(t); !strings.Contains(e, "link") {
			t.Fatalf("%s", e)
		}
		if ents, _ := os.ReadDir(elsewhere); len(ents) != 0 {
			t.Fatalf("wrote through the link: %v", ents)
		}
	})
	t.Run("host-run is a link", func(t *testing.T) {
		f := newHostFixture(t)
		elsewhere := filepath.Join(f.root, "elsewhere")
		os.Mkdir(elsewhere, 0o755)
		os.Symlink(elsewhere, f.mailbox())
		if rc := f.run(t, "say", "hi"); rc != 64 {
			t.Fatalf("rc %d", rc)
		}
		if e := f.termErr(t); !strings.Contains(e, "link") {
			t.Fatalf("%s", e)
		}
		if ents, _ := os.ReadDir(elsewhere); len(ents) != 0 {
			t.Fatalf("wrote through the link: %v", ents)
		}
		if f.termOut(t) != "" {
			t.Fatal("the command ran")
		}
	})
}

// With the random source fixed, the build name is known in advance, so a FIFO and a hardlink
// planted there make the create fail: the run is refused before exec, and neither changes.
func TestHostRunRefusesAPlantedBuildDirectory(t *testing.T) {
	f := newHostFixture(t)
	id, _ := NewRunID(fixedNow, bytes.NewReader(bytes.Repeat([]byte{0x5a}, 4)))
	tmp := filepath.Join(f.mailbox(), TmpName(id))
	os.MkdirAll(tmp, 0o700)
	if err := syscall.Mkfifo(filepath.Join(tmp, StdoutFile), 0o600); err != nil {
		t.Fatal(err)
	}
	victim := filepath.Join(f.root, "victim")
	os.WriteFile(victim, []byte("keep"), 0o600)
	if err := os.Link(victim, filepath.Join(tmp, RequestFile)); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(f.root, "ran")
	if rc := f.run(t, "mark", marker); rc != 64 {
		t.Fatalf("rc %d\n%s", rc, f.termErr(t))
	}
	if _, err := os.Lstat(marker); err == nil {
		t.Fatal("the command ran")
	}
	if e := f.termErr(t); !strings.Contains(e, "cannot write to") || !strings.Contains(e, "without `yolo host-run`") {
		t.Fatalf("%s", e)
	}
	if b, _ := os.ReadFile(victim); string(b) != "keep" {
		t.Fatalf("victim %q", b)
	}
	if fi, _ := os.Lstat(filepath.Join(tmp, StdoutFile)); fi.Mode()&fs.ModeNamedPipe == 0 {
		t.Fatalf("the FIFO was replaced: %v", fi.Mode())
	}
}

// A FIFO and a hardlink the jail plants inside the live run directory fail the stream creates:
// the run already happened, the delivery fails visibly, and neither planted file changes.
func TestHostRunDeliveryFailureLeavesPlantedFilesAlone(t *testing.T) {
	f := newHostFixture(t)
	gate := filepath.Join(f.root, "gate")
	done := make(chan int, 1)
	go func() { done <- f.run(t, "slow", gate) }()
	var dir string
	for i := 0; i < 2000 && dir == ""; i++ {
		for _, e := range func() []os.DirEntry { e, _ := os.ReadDir(f.mailbox()); return e }() {
			if _, ok := ParseRunID(e.Name()); ok {
				dir = filepath.Join(f.mailbox(), e.Name())
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	if dir == "" {
		t.Fatal("no run directory")
	}
	syscall.Mkfifo(filepath.Join(dir, StdoutFile), 0o600)
	victim := filepath.Join(f.root, "victim")
	os.WriteFile(victim, []byte("keep"), 0o600)
	os.Link(victim, filepath.Join(dir, StderrFile))
	os.WriteFile(gate, nil, 0o600)
	if rc := <-done; rc != 0 {
		t.Fatalf("rc %d (want the child's own 0)", rc)
	}
	if e := f.termErr(t); !strings.Contains(e, "delivery failed") || !strings.Contains(e, "copy the output above") {
		t.Fatalf("%s", e)
	}
	if b, _ := os.ReadFile(victim); string(b) != "keep" {
		t.Fatalf("victim %q", b)
	}
	if fi, _ := os.Lstat(filepath.Join(dir, StdoutFile)); fi.Mode()&fs.ModeNamedPipe == 0 {
		t.Fatalf("FIFO replaced: %v", fi.Mode())
	}
	if _, err := os.Lstat(filepath.Join(dir, ResultFile)); err == nil {
		t.Fatal("result.json written after a failed delivery")
	}
}

func TestHostRunRefusesAWritablePathEntry(t *testing.T) {
	cases := map[string]func(f *hostFixture) string{
		"empty":     func(*hostFixture) string { return "" },
		"dot":       func(*hostFixture) string { return "." },
		"workspace": func(f *hostFixture) string { return filepath.Join(f.ws, "bin") },
		"yolo-holder": func(f *hostFixture) string {
			h := filepath.Join(f.home, ".yolo-holder")
			os.MkdirAll(filepath.Join(h, ".yolo"), 0o755)
			os.MkdirAll(filepath.Join(h, "bin"), 0o755)
			return filepath.Join(h, "bin")
		},
		"state": func(f *hostFixture) string { return paths.GlobalCache() },
	}
	for name, entry := range cases {
		t.Run(name, func(t *testing.T) {
			f := newHostFixture(t)
			bad := entry(f)
			f.setPath(f.bin, coreutilsDir(t), bad)
			marker := filepath.Join(f.root, "ran")
			if rc := f.run(t, "mark", marker); rc != 64 {
				t.Fatalf("rc %d\n%s", rc, f.termErr(t))
			}
			if _, err := os.Lstat(marker); err == nil {
				t.Fatal("the command ran")
			}
			var ref Refusal
			readJSON(t, filepath.Join(f.onlyRun(t), RefusedFile), &ref)
			if ref.Step != 9 || ref.Label != "t1" || ref.Next == "" || ref.Workspace != f.ws {
				t.Fatalf("%+v", ref)
			}
			if !strings.Contains(f.termErr(t), "PATH") {
				t.Fatalf("%s", f.termErr(t))
			}
			ev := f.auditEvents(t)
			if len(ev) != 1 || ev[0].Set != "refused" || ev[0].Outcome != "refused" || ev[0].Reason == "" {
				t.Fatalf("audit %+v", ev)
			}
		})
	}
}

// TestHostRunAllowsAndSkipsYolosHostWrapperDir is the host launch-wrapper dir yolo itself asks
// users to PREPEND to PATH (paths.WrapDir, host-agent-environment.md §5.1). It sits under yolo's
// state directory but no jail mounts it (hostpath.ManagedDirs), so the PATH check lets it stand,
// and the program is resolved past it the way a host launch resolves its target (HE-D5): `say`
// runs the real tool, not the wrapper in front of it.
func TestHostRunAllowsAndSkipsYolosHostWrapperDir(t *testing.T) {
	f := newHostFixture(t)
	wrap := paths.WrapDir()
	if err := os.MkdirAll(wrap, 0o755); err != nil {
		t.Fatal(err)
	}
	writeScript(t, filepath.Join(wrap, "say"), `echo WRAPPED`)
	f.setPath(wrap, f.bin, coreutilsDir(t))
	if rc := f.run(t, "say", "hi"); rc != 0 {
		t.Fatalf("rc %d\n%s", rc, f.termErr(t))
	}
	if got := f.termOut(t); got != "hi\n" {
		t.Fatalf("the terminal saw %q", got)
	}
	var res Result
	readJSON(t, filepath.Join(f.onlyRun(t), ResultFile), &res)
	if res.Program != filepath.Join(f.bin, "say") {
		t.Fatalf("program %q, want the real say past the wrapper dir", res.Program)
	}
}

// TestHostRunStillRefusesTheRestOfTheStateDir: allowing the wrapper dir allows nothing else under
// yolo's state directory, which a jail can write through its mounts (the shared cache, here).
func TestHostRunStillRefusesTheRestOfTheStateDir(t *testing.T) {
	f := newHostFixture(t)
	cache := paths.GlobalCache()
	if err := os.MkdirAll(cache, 0o755); err != nil {
		t.Fatal(err)
	}
	f.setPath(paths.WrapDir(), f.bin, coreutilsDir(t), cache)
	if rc := f.run(t, "say", "hi"); rc != 64 {
		t.Fatalf("rc %d\n%s", rc, f.termErr(t))
	}
	want := "inside yolo's state directory " + paths.GlobalStorage() + ", which a jail can write"
	if !strings.Contains(f.termErr(t), want) {
		t.Fatalf("want %q in\n%s", want, f.termErr(t))
	}
}

// TestPathEntryProblemJudgesAManagedLinkByWhereItLeads: a name inside the managed dir that is a
// link to elsewhere in the state directory is refused for where it resolves.
func TestPathEntryProblemJudgesAManagedLinkByWhereItLeads(t *testing.T) {
	state := evalTemp(t)
	managed := filepath.Join(state, "bin")
	cache := filepath.Join(state, "cache")
	for _, d := range []string{managed, cache} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	link := filepath.Join(managed, "wrap")
	if err := os.Symlink(cache, link); err != nil {
		t.Fatal(err)
	}
	if why := pathEntryProblem(link, []string{state}, []string{managed}); !strings.Contains(why, "state directory") {
		t.Fatalf("a managed link into the cache was allowed: %q", why)
	}
	if why := pathEntryProblem(filepath.Join(managed, "block"), []string{state}, []string{managed}); why != "" {
		t.Fatalf("a managed dir was refused: %q", why)
	}
}

func TestHostRunRefusesTheGHCredentialForm(t *testing.T) {
	f := newHostFixture(t)
	if rc := f.run(t, "gh", "auth", "token"); rc != 64 {
		t.Fatalf("rc %d", rc)
	}
	var ref Refusal
	readJSON(t, filepath.Join(f.onlyRun(t), RefusedFile), &ref)
	if ref.Step != 8 || !strings.Contains(ref.Reason, "credential") {
		t.Fatalf("%+v", ref)
	}
	if _, err := os.Lstat(filepath.Join(f.onlyRun(t), RequestFile)); err == nil {
		t.Fatal("a refusal holds request.json")
	}
}

func TestHostRunStartFailureDeliversANullExit(t *testing.T) {
	f := newHostFixture(t)
	rc := f.run(t, "no-such-program-here")
	if rc == 0 || rc == 64 {
		t.Fatalf("rc %d", rc)
	}
	dir := f.onlyRun(t)
	var res Result
	readJSON(t, filepath.Join(dir, ResultFile), &res)
	if res.Exit != nil {
		t.Fatalf("exit %v", *res.Exit)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, StderrFile)); !strings.Contains(string(b), "no-such-program-here") {
		t.Fatalf("stderr %q", b)
	}
}

func TestHostRunRecordsTheChildsSignal(t *testing.T) {
	f := newHostFixture(t)
	pidFile := filepath.Join(f.root, "pid")
	done := make(chan int, 1)
	go func() { done <- f.run(t, "hang", pidFile) }()
	var pid int
	for i := 0; i < 2000 && pid == 0; i++ {
		if b, err := os.ReadFile(pidFile); err == nil {
			pid, _ = strconv.Atoi(strings.TrimSpace(string(b)))
		}
		time.Sleep(5 * time.Millisecond)
	}
	if pid == 0 {
		t.Fatal("no pid")
	}
	// The exec'd sleep may not have replaced the shell yet; give it a moment.
	time.Sleep(50 * time.Millisecond)
	if err := syscall.Kill(pid, syscall.SIGINT); err != nil {
		t.Fatal(err)
	}
	select {
	case rc := <-done:
		if rc != 130 {
			t.Fatalf("rc %d", rc)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("the host form did not return")
	}
	var res Result
	readJSON(t, filepath.Join(f.onlyRun(t), ResultFile), &res)
	if res.Signal != "interrupt" || res.Exit != nil {
		t.Fatalf("%+v", res)
	}
}

func TestHostRunTruncatesPastTheSpoolCap(t *testing.T) {
	f := newHostFixture(t)
	f.env.SpoolCap = 8
	if rc := f.run(t, "say", "0123456789abcdef"); rc != 0 {
		t.Fatalf("rc %d", rc)
	}
	dir := f.onlyRun(t)
	b, _ := os.ReadFile(filepath.Join(dir, StdoutFile))
	if !strings.HasPrefix(string(b), "01234567\n[yolo host-run: truncated after 8 B; 9 more bytes were shown only on the host terminal]") {
		t.Fatalf("stdout %q", b)
	}
	if got := f.termOut(t); got != "0123456789abcdef\n" {
		t.Fatalf("the terminal got %q", got)
	}
	var res Result
	readJSON(t, filepath.Join(dir, ResultFile), &res)
	if !res.StdoutTruncated || res.StdoutBytes != 17 || res.StderrTruncated {
		t.Fatalf("%+v", res)
	}
}

func TestHostRunPrunesOldRunsOnly(t *testing.T) {
	f := newHostFixture(t)
	old, _ := NewRunID(fixedNow.Add(-25*time.Hour), bytes.NewReader([]byte{1, 2, 3, 4}))
	young, _ := NewRunID(fixedNow.Add(-23*time.Hour), bytes.NewReader([]byte{1, 2, 3, 4}))
	for _, d := range []string{old, young, TmpName(old), "keepme", TmpName(young)} {
		os.MkdirAll(filepath.Join(f.mailbox(), d, "x"), 0o700)
	}
	if rc := f.run(t, "say", "hi"); rc != 0 {
		t.Fatalf("rc %d", rc)
	}
	for d, want := range map[string]bool{old: false, TmpName(old): false, young: true, "keepme": true, TmpName(young): true} {
		_, err := os.Lstat(filepath.Join(f.mailbox(), d))
		if (err == nil) != want {
			t.Errorf("%s present=%v want %v", d, err == nil, want)
		}
	}
}

func TestHostRunDisclosesRiskyEnvironmentAndArgvPaths(t *testing.T) {
	f := newHostFixture(t)
	path := f.bin + string(os.PathListSeparator) + coreutilsDir(t)
	f.env.Environ = func() []string {
		return []string{"PATH=" + path, "LD_PRELOAD=/opt/x.so", "GIT_CONFIG_COUNT=1", "DYLD_INSERT_LIBRARIES=/y"}
	}
	target := filepath.Join(f.ws, "file")
	os.WriteFile(target, nil, 0o600)
	if rc := f.run(t, "say", target); rc != 0 {
		t.Fatalf("rc %d", rc)
	}
	e := f.termErr(t)
	for _, want := range []string{"env:", "LD_PRELOAD=/opt/x.so", "GIT_CONFIG_COUNT=1", "DYLD_INSERT_LIBRARIES=/y", "--git-dir"} {
		if !strings.Contains(e, want) {
			t.Errorf("disclosure lacks %q:\n%s", want, e)
		}
	}
}
