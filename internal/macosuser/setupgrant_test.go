package macosuser

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// setupgrant_test.go pins `yolo macos-setup`'s administrator act G0 for the one run-as rule
// (task 7 of docs/design/macos-user-privilege-lifecycle-plan.md) and its undo in
// `yolo macos-teardown`.

func TestSetupInstallsTheRuleThenProvesItWithAnEmptyCache(t *testing.T) {
	var rec []string
	d := mockDeps(&rec)
	d.StagingRootFacts = func(string) StagingRootFacts { return StagingRootFacts{} } // fresh Mac
	d.HostUser = func() string { return "matt" }
	if rc := MacosSetup(d); rc != 0 {
		t.Fatalf("rc = %d", rc)
	}
	order := []string{
		"run:/usr/bin/sudo /usr/sbin/visudo -c -q -f /etc/sudoers.d/.yolo-jail.new",
		"run:/usr/bin/sudo /bin/mv -f /etc/sudoers.d/.yolo-jail.new /etc/sudoers.d/yolo-jail",
		"run:/usr/bin/sudo /usr/sbin/visudo -c -q",
		"run:/usr/bin/sudo -k",
		"run:/usr/bin/sudo -n --user=_yolojail /usr/bin/env /usr/bin/true",
	}
	last := -1
	for _, w := range order {
		i := slices.Index(rec, w)
		if i <= last {
			t.Fatalf("%q missing or out of order:\n%s", w, strings.Join(rec, "\n"))
		}
		last = i
	}
}

func TestSetupRefusesASecondEnrolledUserBeforeAnySudo(t *testing.T) {
	var rec []string
	var buf bytes.Buffer
	d := mockDeps(&rec)
	d.Out = &buf
	d.StagingRootFacts = func(string) StagingRootFacts {
		return StagingRootFacts{Exists: true, IsDir: true, UID: 502, Mode: 0o755}
	}
	if rc := MacosSetup(d); rc != 1 {
		t.Fatalf("rc = %d", rc)
	}
	if recAt(rec, "sudo") >= 0 {
		t.Errorf("ran sudo before refusing: %v", rec)
	}
	if !strings.Contains(buf.String(), "yolo macos-teardown") {
		t.Errorf("the refusal names no remedy:\n%s", buf.String())
	}
}

// stubAccountNames makes the owner lookup answer from names, for the length of the test.
func stubAccountNames(t *testing.T, names map[uint32]string) {
	t.Helper()
	prev := accountNameOfUID
	accountNameOfUID = func(uid uint32) string { return names[uid] }
	t.Cleanup(func() { accountNameOfUID = prev })
}

// A root the sandbox account made first is a squat, not an enrollment: setup refuses it without
// adopting it, before any sudo, and names how to inspect and remove it.
func TestSetupRefusesAStagingRootTheSandboxAccountSquatted(t *testing.T) {
	stubAccountNames(t, map[uint32]string{601: SandboxUser})
	var rec []string
	var buf bytes.Buffer
	d := mockDeps(&rec)
	d.Out = &buf
	d.StagingRootFacts = func(string) StagingRootFacts {
		return StagingRootFacts{Exists: true, IsDir: true, UID: 601, Mode: 0o755}
	}
	if rc := MacosSetup(d); rc != 1 {
		t.Fatalf("rc = %d", rc)
	}
	if recAt(rec, "sudo") >= 0 {
		t.Errorf("ran sudo before refusing: %v", rec)
	}
	want := "/Users/Shared/yolo-staged already exists and belongs to _yolojail, not to you. Setup does " +
		"not adopt it: whoever owns it controls what the sandbox reads. Inspect it with " +
		"`ls -leR /Users/Shared/yolo-staged`, remove it with `sudo rm -rf /Users/Shared/yolo-staged`, " +
		"then run yolo macos-setup again."
	if !strings.Contains(buf.String(), want) {
		t.Errorf("squat refusal:\n%s\nwant %q", buf.String(), want)
	}
}

// grepRun is mockDeps' Run with the existing-rule check answering grepRC and the root -n probe
// answering rootRC.
func grepRun(rec *[]string, grepRC, rootRC int) func([]string) int {
	return func(argv []string) int {
		joined := strings.Join(argv, " ")
		*rec = append(*rec, "run:"+joined)
		switch {
		case strings.HasPrefix(joined, "/usr/bin/sudo /usr/bin/grep -qxF"):
			return grepRC
		case joined == "/usr/bin/sudo -n /usr/bin/true":
			return rootRC
		}
		return 0
	}
}

// A rule in /etc/sudoers.d/yolo-jail that is not this user's (grep exits 1 while sudo itself
// ran) is another macOS user's enrollment: refused before anything is created or installed.
func TestSetupRefusesWhenTheInstalledRuleNamesAnotherUser(t *testing.T) {
	var rec []string
	var buf bytes.Buffer
	d := mockDeps(&rec)
	d.Out = &buf
	d.Run = grepRun(&rec, 1, 0)
	if rc := MacosSetup(d); rc != 1 {
		t.Fatalf("rc = %d", rc)
	}
	if i := slices.Index(rec, "run:/usr/bin/sudo /usr/bin/grep -qxF -e matt ALL=(_yolojail) "+
		"NOPASSWD: /usr/bin/env /etc/sudoers.d/yolo-jail"); i < 0 {
		t.Errorf("the existing rule was not asked about:\n%s", strings.Join(rec, "\n"))
	}
	for _, never := range []string{"visudo", "dscl", "/bin/mkdir", "write:"} {
		if recAt(rec, never) >= 0 {
			t.Errorf("went on after refusing (%s):\n%s", never, strings.Join(rec, "\n"))
		}
	}
	out := buf.String()
	if !strings.Contains(out, "This Mac's macos-user backend is set up for another macOS user") ||
		!strings.Contains(out, "That user runs yolo macos-teardown, then you run yolo macos-setup.") {
		t.Errorf("second-user refusal:\n%s", out)
	}
}

// A grep exit of 1 that sudo produced (the password was not given) is not another user's rule:
// setup stops, changes nothing, and does not tell the user someone else is enrolled.
func TestSetupDoesNotMistakeAFailedSudoForAnotherUsersRule(t *testing.T) {
	var rec []string
	var buf bytes.Buffer
	d := mockDeps(&rec)
	d.Out = &buf
	d.Run = grepRun(&rec, 1, 1)
	if rc := MacosSetup(d); rc != 1 {
		t.Fatalf("rc = %d", rc)
	}
	if strings.Contains(buf.String(), "another macOS user") {
		t.Errorf("blamed another user for a failed sudo:\n%s", buf.String())
	}
	if !strings.Contains(buf.String(), "yolo macos-setup") || recAt(rec, "visudo") >= 0 {
		t.Errorf("printed %q and ran %v", buf.String(), rec)
	}
}

// The rule setup copies into place is exactly SudoersRule's, staged under the inert dot name,
// owned root:wheel 0440 before visudo checks it.
func TestSetupStagesExactlyTheRuleUnderTheInertName(t *testing.T) {
	var rec []string
	d := mockDeps(&rec)
	var tmp, content string
	d.WriteHostFile = func(path, c string, _ os.FileMode) bool {
		rec = append(rec, "write:"+path)
		tmp, content = path, c
		return true
	}
	if rc := MacosSetup(d); rc != 0 {
		t.Fatalf("rc = %d", rc)
	}
	want, _ := SudoersRule("matt")
	if content != want {
		t.Errorf("rule written =\n%s\nwant\n%s", content, want)
	}
	order := []string{
		"write:" + tmp,
		"run:/usr/bin/sudo /bin/cp " + tmp + " /etc/sudoers.d/.yolo-jail.new",
		"run:/usr/bin/sudo /usr/sbin/chown root:wheel /etc/sudoers.d/.yolo-jail.new",
		"run:/usr/bin/sudo /bin/chmod 0440 /etc/sudoers.d/.yolo-jail.new",
		"run:/usr/bin/sudo /usr/sbin/visudo -c -q -f /etc/sudoers.d/.yolo-jail.new",
	}
	last := -1
	for _, w := range order {
		i := slices.Index(rec, w)
		if i <= last {
			t.Fatalf("%q missing or out of order:\n%s", w, strings.Join(rec, "\n"))
		}
		last = i
	}
	if _, err := os.Stat(tmp); !os.IsNotExist(err) {
		t.Errorf("the host temp file %s was left behind (%v)", tmp, err)
	}
}

func TestSetupInstallsNothingWhenVisudoRejectsTheRule(t *testing.T) {
	var rec []string
	var buf bytes.Buffer
	d := mockDeps(&rec)
	d.Out = &buf
	d.Run = func(argv []string) int {
		joined := strings.Join(argv, " ")
		rec = append(rec, "run:"+joined)
		if joined == "/usr/bin/sudo /usr/sbin/visudo -c -q -f "+SudoersStagingFile {
			return 1
		}
		return 0
	}
	if rc := MacosSetup(d); rc != 1 {
		t.Fatalf("rc = %d", rc)
	}
	if recAt(rec, "/bin/mv") >= 0 || recAt(rec, "-n --user=_yolojail") >= 0 {
		t.Errorf("went on after visudo rejected the rule:\n%s", strings.Join(rec, "\n"))
	}
	if slices.Index(rec, "run:/usr/bin/sudo /bin/rm -f "+SudoersStagingFile) < 0 {
		t.Errorf("the rejected staging file was not removed:\n%s", strings.Join(rec, "\n"))
	}
	out := buf.String()
	if !strings.Contains(out, "✗ visudo rejected the rule, so nothing was installed and your sudo "+
		"configuration is unchanged:") ||
		!strings.Contains(out, "This is a yolo bug; report it at "+
			"https://github.com/mschulkind-oss/yolo-jail/issues with that output.") {
		t.Errorf("visudo refusal:\n%s", out)
	}
}

func TestSetupRefusesWhenTheProbeFailsAfterTheInstall(t *testing.T) {
	var rec []string
	var buf bytes.Buffer
	d := mockDeps(&rec)
	d.Out = &buf
	probe := strings.Join(RunAsGrantProbeArgv(), " ")
	d.Run = func(argv []string) int {
		joined := strings.Join(argv, " ")
		rec = append(rec, "run:"+joined)
		if joined == probe {
			return 1
		}
		return 0
	}
	if rc := MacosSetup(d); rc != 1 {
		t.Fatalf("rc = %d", rc)
	}
	out := buf.String()
	if !strings.Contains(out, "✗ The rule is installed, but sudo still asks for a password to run as "+
		"_yolojail. Check that /etc/sudoers reads /private/etc/sudoers.d "+
		"(`sudo grep -n includedir /etc/sudoers`)") || strings.Contains(out, "backend ready") {
		t.Errorf("probe-failure verdict:\n%s", out)
	}
}

func TestSetupRefusesAUserNameItCannotWriteIntoTheRule(t *testing.T) {
	var rec []string
	var buf bytes.Buffer
	d := mockDeps(&rec)
	d.Out = &buf
	d.HostUser = func() string { return "m att" }
	if rc := MacosSetup(d); rc != 1 {
		t.Fatalf("rc = %d", rc)
	}
	if recAt(rec, "sudo") >= 0 {
		t.Errorf("ran sudo for a name it cannot use: %v", rec)
	}
	want := "Your user name m att cannot be written into a sudo rule safely. The macos-user backend " +
		"supports names of letters, digits, '_', '.' and '-'."
	if !strings.Contains(buf.String(), want) {
		t.Errorf("unsafe-name refusal:\n%s", buf.String())
	}
}

// The ready verdict says launches need no password, where it used to say sudo prompts per run.
func TestSetupsReadyVerdictPromisesNoPassword(t *testing.T) {
	var buf bytes.Buffer
	d := mockDeps(nil)
	d.Out = &buf
	if rc := MacosSetup(d); rc != 0 {
		t.Fatalf("rc = %d", rc)
	}
	out := buf.String()
	if !strings.Contains(out, "Launches start and stop with no password from now on.") ||
		strings.Contains(out, "sudo will prompt per run") {
		t.Errorf("verdict:\n%s", out)
	}
	if !strings.Contains(out, "Granting matt one standing right: run commands as _yolojail, never "+
		"as root, without a password (/etc/sudoers.d/yolo-jail). Launches then start and stop with "+
		"no prompt.") {
		t.Errorf("setup did not disclose the standing right:\n%s", out)
	}
}

// legacyKey is a session key an older yolo's launch would have recorded.
func legacyKey(id string) string { return SessionKey("proj-0123abcd", id) }

// PL-8: setup removes, with sudo, exactly the five legacy files of each session whose record is
// free AND whose legacy profile exists, then the record. A live session's record and files, and a
// gone session that left nothing in the legacy dir, are not setup's to touch.
func TestSetupRemovesOnlyTheLegacyFilesOfEndedOlderSessions(t *testing.T) {
	dir := t.TempDir()
	gone := legacyKey("00000000000000a1")   // ended, older yolo: setup's
	ours := legacyKey("00000000000000b2")   // ended, this yolo: the launch sweep's
	living := legacyKey("00000000000000c3") // still running under its old launcher
	for _, k := range []string{gone, ours} {
		if err := os.WriteFile(filepath.Join(dir, k+sessionRecordSuffix), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	held, err := openSessionRecord(dir, living)
	if err != nil {
		t.Fatal(err)
	}
	defer held.close()

	var rec []string
	var buf bytes.Buffer
	d := mockDeps(&rec)
	d.Out = &buf
	d.SessionRecordDir = func() string { return dir }
	d.PathExists = func(p string) bool {
		return p == SessionProfilePath(gone, LegacyStateDir()) ||
			p == SessionProfilePath(living, LegacyStateDir())
	}
	if rc := MacosSetup(d); rc != 0 {
		t.Fatalf("rc = %d\n%s", rc, buf.String())
	}
	want := "run:/usr/bin/sudo /bin/rm -f " + strings.Join(SessionFilePaths(gone, LegacyStateDir()), " ")
	if slices.Index(rec, want) < 0 {
		t.Errorf("the ended older session's legacy files were not removed:\n%s", strings.Join(rec, "\n"))
	}
	for _, k := range []string{ours, living} {
		if recAt(rec, k) >= 0 {
			t.Errorf("touched session %s:\n%s", k, strings.Join(rec, "\n"))
		}
	}
	if recAt(rec, "/var/yolo-jail/bin") >= 0 || recAt(rec, "rm -rf /var/yolo-jail") >= 0 {
		t.Errorf("touched a legacy tree:\n%s", strings.Join(rec, "\n"))
	}
	if _, err := os.Stat(filepath.Join(dir, gone+sessionRecordSuffix)); !os.IsNotExist(err) {
		t.Errorf("the swept record is still there (%v)", err)
	}
	for _, k := range []string{ours, living} {
		if _, err := os.Stat(filepath.Join(dir, k+sessionRecordSuffix)); err != nil {
			t.Errorf("record %s removed: %v", k, err)
		}
	}
	if !strings.Contains(buf.String(), "yolo stores") {
		t.Errorf("setup does not point at `yolo stores` for the legacy trees:\n%s", buf.String())
	}
}

func TestTeardownRemovesTheRuleAndTheStagingRootButNotTheLegacyDir(t *testing.T) {
	var rec []string
	var buf bytes.Buffer
	d := mockDeps(&rec)
	d.Out = &buf
	d.PathIsDir = func(p string) bool { return p == LegacyStateDir() }
	if rc := MacosTeardown(d); rc != 0 {
		t.Fatalf("rc = %d", rc)
	}
	order := []string{"run:/usr/bin/sudo /bin/rm -f /etc/sudoers.d/yolo-jail"}
	for _, c := range removeSealedCommands(StagingRoot()) {
		order = append(order, "run:"+strings.Join(c, " "))
	}
	last := -1
	for _, w := range order {
		i := slices.Index(rec, w)
		if i <= last {
			t.Fatalf("%q missing or out of order:\n%s", w, strings.Join(rec, "\n"))
		}
		last = i
	}
	if strings.Contains(rec[slices.Index(rec, order[len(order)-1])], "sudo") {
		t.Errorf("the staging root was removed with sudo")
	}
	if recAt(rec, LegacyStateDir()) >= 0 {
		t.Errorf("teardown touched %s:\n%s", LegacyStateDir(), strings.Join(rec, "\n"))
	}
	if !strings.Contains(buf.String(), "yolo stores") {
		t.Errorf("teardown does not say where the legacy dir is listed:\n%s", buf.String())
	}
}

// Teardown never removes a staging root another account owns: it is not this user's enrollment.
func TestTeardownLeavesAStagingRootItDoesNotOwn(t *testing.T) {
	var rec []string
	d := mockDeps(&rec)
	d.StagingRootFacts = func(string) StagingRootFacts {
		return StagingRootFacts{Exists: true, IsDir: true, UID: 502, Mode: 0o755}
	}
	if rc := MacosTeardown(d); rc != 0 {
		t.Fatalf("rc = %d", rc)
	}
	if recAt(rec, "rm -rf "+StagingRoot()) >= 0 {
		t.Errorf("removed another account's staging root:\n%s", strings.Join(rec, "\n"))
	}
}
