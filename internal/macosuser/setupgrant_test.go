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
		"run:/usr/bin/sudo /usr/sbin/visudo -c -f /etc/sudoers.d/.yolo-jail.new",
		"run:/usr/bin/sudo /bin/mv -f /etc/sudoers.d/.yolo-jail.new /etc/sudoers.d/yolo-jail",
		"run:/usr/bin/sudo /usr/sbin/visudo -c",
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
		case strings.HasPrefix(joined, "/usr/bin/sudo /usr/bin/grep -s -qxF"):
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
	if i := slices.Index(rec, "run:/usr/bin/sudo /usr/bin/grep -s -qxF -e matt ALL=(_yolojail) "+
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
		"run:/usr/bin/sudo /usr/sbin/visudo -c -f /etc/sudoers.d/.yolo-jail.new",
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

// captureVia gives d a RunCapture that answers through d.Run (so the recording and the exit
// codes stay the test's) and prints output[joined] for the command, the way visudo would.
func captureVia(d *Deps, output map[string]string) {
	run := d.Run
	d.RunCapture = func(argv []string) (string, int) {
		return output[strings.Join(argv, " ")], run(argv)
	}
}

const (
	visudoRuleCheck   = "/usr/bin/sudo /usr/sbin/visudo -c -f " + SudoersStagingFile
	visudoConfigCheck = "/usr/bin/sudo /usr/sbin/visudo -c"
)

// indented is s as the refusals print captured text and the rule: each line after two spaces.
func indented(s string) string {
	var b strings.Builder
	for _, l := range strings.Split(strings.TrimRight(s, "\n"), "\n") {
		b.WriteString("  " + l + "\n")
	}
	return b.String()
}

// A passing check stays quiet: visudo's "parsed OK" lines are not the user's business.
func TestSetupDoesNotPrintVisudoOutputWhenTheChecksPass(t *testing.T) {
	var rec []string
	var buf bytes.Buffer
	d := mockDeps(&rec)
	d.Out = &buf
	captureVia(&d, map[string]string{
		visudoRuleCheck:   SudoersStagingFile + ": parsed OK\n",
		visudoConfigCheck: "/etc/sudoers: parsed OK\n",
	})
	if rc := MacosSetup(d); rc != 0 {
		t.Fatalf("rc = %d\n%s", rc, buf.String())
	}
	if strings.Contains(buf.String(), "parsed OK") {
		t.Errorf("printed visudo's output for a passing check:\n%s", buf.String())
	}
}

// Production wires the capture: without it every check would print through the terminal.
func TestRealDepsCaptureVisudoOutput(t *testing.T) {
	if RealDeps(nil, nil, false).RunCapture == nil {
		t.Fatal("RealDeps has no RunCapture, so visudo's checks are not quiet on success")
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
		if joined == visudoRuleCheck {
			return 1
		}
		return 0
	}
	reason := ">>> " + SudoersStagingFile + ": syntax error near line 3 <<<\n"
	captureVia(&d, map[string]string{visudoRuleCheck: reason})
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
	rule, _ := SudoersRule("matt")
	for _, want := range []string{
		"✗ visudo rejected yolo's sudo rule. Nothing was installed.\n\n" + indented(reason) + "\n",
		"This is a yolo bug. Report it with the output above and this rule:\n\n" + indented(rule) +
			"\n  https://github.com/mschulkind-oss/yolo-jail/issues\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("visudo refusal lacks %q:\n%s", want, out)
		}
	}
}

// wholeCheckRun is mockDeps' Run with the whole-configuration check (`visudo -c`) answering
// withRule while the rule is in place and withoutRule once setup has removed it again.
func wholeCheckRun(rec *[]string, withRule, withoutRule int) func([]string) int {
	installed := false
	return func(argv []string) int {
		joined := strings.Join(argv, " ")
		*rec = append(*rec, "run:"+joined)
		switch joined {
		case "/usr/bin/sudo /bin/mv -f " + SudoersStagingFile + " " + SudoersFile:
			installed = true
		case "/usr/bin/sudo /bin/rm -f " + SudoersFile:
			installed = false
		case visudoConfigCheck:
			if installed {
				return withRule
			}
			return withoutRule
		}
		return 0
	}
}

// inOrder fails t unless every entry of want appears in rec, in order (repeats allowed).
func inOrder(t *testing.T, rec, want []string) {
	t.Helper()
	last := -1
	for _, w := range want {
		i := slices.Index(rec[last+1:], w)
		if i < 0 {
			t.Fatalf("%q missing or out of order:\n%s", w, strings.Join(rec, "\n"))
		}
		last += 1 + i
	}
}

// The rule passes on its own but the whole configuration fails with it in place, and fails again
// once setup has removed it: the configuration was already broken, which is not yolo's bug, and
// the refusal names the file visudo blames in a command that edits it.
func TestSetupBlamesTheExistingConfigWhenItFailsWithoutTheRuleToo(t *testing.T) {
	var rec []string
	var buf bytes.Buffer
	d := mockDeps(&rec)
	d.Out = &buf
	d.Run = wholeCheckRun(&rec, 1, 1)
	reason := ">>> /etc/sudoers.d/corp: syntax error near line 3 <<<\n" +
		"/etc/sudoers.d/other:2:5: syntax error\n"
	captureVia(&d, map[string]string{visudoConfigCheck: reason})
	if rc := MacosSetup(d); rc != 1 {
		t.Fatalf("rc = %d", rc)
	}
	inOrder(t, rec, []string{
		"run:/usr/bin/sudo /bin/mv -f " + SudoersStagingFile + " " + SudoersFile,
		"run:" + visudoConfigCheck,
		"run:/usr/bin/sudo /bin/rm -f " + SudoersFile,
		"run:" + visudoConfigCheck,
	})
	if recAt(rec, "-n --user=_yolojail") >= 0 {
		t.Errorf("went on after the whole-configuration check failed:\n%s", strings.Join(rec, "\n"))
	}
	out := buf.String()
	want := "✗ Your sudo configuration is invalid without yolo's rule too. Nothing was installed.\n\n" +
		indented(reason) + "\nFix it, then run setup again:\n\n" +
		"  sudo visudo -f /etc/sudoers.d/corp\n" +
		"  sudo visudo -f /etc/sudoers.d/other\n" +
		"  sudo visudo -c\n" +
		"  yolo macos-setup\n"
	if !strings.Contains(out, want) {
		t.Errorf("refusal lacks\n%s\ngot:\n%s", want, out)
	}
	if strings.Contains(out, "yolo bug") {
		t.Errorf("called a pre-existing sudo problem a yolo bug:\n%s", out)
	}
}

// The whole configuration fails only with the rule in place: that IS yolo's bug.
func TestSetupCallsItAYoloBugWhenOnlyTheRuleBreaksTheConfig(t *testing.T) {
	var rec []string
	var buf bytes.Buffer
	d := mockDeps(&rec)
	d.Out = &buf
	d.Run = wholeCheckRun(&rec, 1, 0)
	reason := ">>> " + SudoersFile + ": syntax error near line 3 <<<\n"
	captureVia(&d, map[string]string{visudoConfigCheck: reason})
	if rc := MacosSetup(d); rc != 1 {
		t.Fatalf("rc = %d", rc)
	}
	out := buf.String()
	rule, _ := SudoersRule("matt")
	for _, want := range []string{
		"✗ visudo rejected your sudo configuration with yolo's rule in place. The rule was " +
			"removed again.\n\n" + indented(reason) + "\n",
		"This is a yolo bug. Report it with the output above and this rule:\n\n" + indented(rule),
	} {
		if !strings.Contains(out, want) {
			t.Errorf("refusal lacks %q:\n%s", want, out)
		}
	}
}

// When the removal of a rule the whole check rejected fails, the rule is still in place, and
// sudo itself may be what broke: the refusal gives the sudo removal, a fallback that needs no
// sudo, and the re-run, each on its own line.
func TestSetupNamesTheRemovalWhenItCannotTakeTheRuleBackOut(t *testing.T) {
	var rec []string
	var buf bytes.Buffer
	d := mockDeps(&rec)
	d.Out = &buf
	inner := wholeCheckRun(&rec, 1, 0)
	d.Run = func(argv []string) int {
		if strings.Join(argv, " ") == "/usr/bin/sudo /bin/rm -f "+SudoersFile {
			rec = append(rec, "run:"+strings.Join(argv, " "))
			return 1
		}
		return inner(argv)
	}
	captureVia(&d, nil)
	if rc := MacosSetup(d); rc != 1 {
		t.Fatalf("rc = %d", rc)
	}
	out := buf.String()
	want := "Remove the rule, then run setup again:\n\n" +
		"  sudo rm -f /etc/sudoers.d/yolo-jail\n" +
		"  # or, if sudo itself fails:\n" +
		"  osascript -e 'do shell script \"/bin/rm -f /etc/sudoers.d/yolo-jail\" with administrator privileges'\n" +
		"  yolo macos-setup\n"
	if !strings.Contains(out, "removing the rule failed") || !strings.Contains(out, want) {
		t.Errorf("refusal lacks\n%s\ngot:\n%s", want, out)
	}
}

// A first run has no rule file: the existing-rule check must not print grep's complaint about it.
func TestSetupAsksAboutTheExistingRuleSilently(t *testing.T) {
	var rec []string
	d := mockDeps(&rec)
	if rc := MacosSetup(d); rc != 0 {
		t.Fatalf("rc = %d", rc)
	}
	if recAt(rec, "/usr/bin/grep -s -qxF") < 0 {
		t.Errorf("the existing-rule grep is not silenced with -s:\n%s", strings.Join(rec, "\n"))
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
