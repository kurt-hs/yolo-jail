package macosuser

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// bsdChmodFileOperands parses one chmod argv (argv[0] the chmod binary) the way macOS's BSD
// chmod does: options are read only up to the first non-option word or a `--`, that next word
// is the MODE, and every word after it is a FILE. GNU chmod permutes and accepts `--` anywhere,
// so an argv like `chmod -R u+w -- dir` passes on Linux and fails on macOS with
// `chmod: --: No such file or directory`.
func bsdChmodFileOperands(argv []string) (mode string, files []string) {
	i := 1
	for ; i < len(argv); i++ {
		a := argv[i]
		if a == "--" {
			i++
			break
		}
		if !strings.HasPrefix(a, "-") || a == "-" {
			break
		}
	}
	if i >= len(argv) {
		return "", nil
	}
	return argv[i], argv[i+1:]
}

// chmodArgvsInScript returns each chmod invocation in a shell script, split on whitespace up to
// the next shell operator. The scripts checked here quote their operands, so that is enough.
func chmodArgvsInScript(script string) [][]string {
	var out [][]string
	words := strings.Fields(script)
	for i, w := range words {
		if w != chmodBin {
			continue
		}
		argv := []string{w}
		for _, a := range words[i+1:] {
			if a == ";" || a == "&&" || a == "||" {
				break
			}
			if strings.HasSuffix(a, ";") {
				argv = append(argv, strings.TrimSuffix(a, ";"))
				break
			}
			argv = append(argv, a)
		}
		out = append(out, argv)
	}
	return out
}

// TestChmodSpellingsParseOnBSDChmod pins every chmod the macos-user backend composes to an
// argument order BSD chmod reads: no `--` after the mode operand. It runs on Linux, where GNU
// chmod would accept the broken order, so the CI job that catches it is not only check-macos.
func TestChmodSpellingsParseOnBSDChmod(t *testing.T) {
	var argvs [][]string
	for _, script := range []string{
		unsealScript,
		openDirScript,
		unsealInScript(`"$tmp"`),
		stageCaptureScript,
		pruneCapturesScript,
	} {
		found := chmodArgvsInScript(script)
		if len(found) == 0 && script != pruneCapturesScript {
			t.Errorf("no chmod found in %q; the test no longer reads the spelling it pins", script)
		}
		argvs = append(argvs, found...)
	}
	for _, cmd := range unsealCommands("/Users/Shared/yolo-staged/x") {
		if len(cmd) > 2 && cmd[0] == shBin {
			argvs = append(argvs, chmodArgvsInScript(cmd[2])...)
		}
	}
	guest := CaptureScratchGuestCommands(RunAs{}, "/Users/Shared/yolo-staged/capture")
	sawGuestChmod := false
	for _, cmd := range guest {
		for i, a := range cmd {
			if a == chmodBin {
				argvs = append(argvs, cmd[i:])
				sawGuestChmod = true
			}
		}
	}
	if !sawGuestChmod {
		t.Errorf("CaptureScratchGuestCommands has no chmod: %v", guest)
	}
	for _, argv := range argvs {
		mode, files := bsdChmodFileOperands(argv)
		if mode == "" || len(files) == 0 {
			t.Errorf("BSD chmod reads no mode and file from %q", argv)
			continue
		}
		for _, f := range files {
			if f == "--" {
				t.Errorf("BSD chmod takes `--` as a FILE in %q (mode %q): put `--` before the mode", argv, mode)
			}
		}
	}
}

func TestBSDChmodParserModel(t *testing.T) {
	_, files := bsdChmodFileOperands([]string{chmodBin, "-R", "u+w", "--", "/x"})
	if len(files) != 2 || files[0] != "--" {
		t.Errorf("the model should read `--` after the mode as a file: %q", files)
	}
	mode, files := bsdChmodFileOperands([]string{chmodBin, "-R", "--", "u+w", "/x"})
	if mode != "u+w" || len(files) != 1 || files[0] != "/x" {
		t.Errorf("mode=%q files=%q", mode, files)
	}
}

// modeThenDashDash matches a chmod spelling in Go source whose `--` follows a symbolic or octal
// mode, in either a shell string (`u+w -- "$1"`) or an argv literal (`"u+w", "--"`).
var modeThenDashDash = regexp.MustCompile(`chmod[^\n]*?(?:[ugoa]*[-+=][rwxXst]+|\b0?[0-7]{3,4})["\s,]+"?--(?:[\s"]|$)`)

// TestNoSourceSpellsDashDashAfterAChmodMode sweeps this package's production source, so a chmod
// added later in the broken order fails here too, not only the ones listed above.
func TestNoSourceSpellsDashDashAfterAChmodMode(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	if !modeThenDashDash.MatchString("exec ` + chmodBin + ` -R u+w -- \"$1\"") ||
		!modeThenDashDash.MatchString(`RunAsSandboxArgv(nil, chmodBin, "-R", "u+w", "--", root)`) ||
		modeThenDashDash.MatchString(`chmodBin + " -R -- u+w " + path`) {
		t.Fatal("the sweep's pattern no longer recognizes the spelling it guards")
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for n, line := range strings.Split(string(b), "\n") {
			if modeThenDashDash.MatchString(line) {
				t.Errorf("%s:%d: chmod spelled with `--` after its mode, which BSD chmod reads as a file: %s", f, n+1, strings.TrimSpace(line))
			}
		}
	}
}
