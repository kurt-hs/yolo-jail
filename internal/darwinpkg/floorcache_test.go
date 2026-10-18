package darwinpkg

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/perf"
)

// recordingNix puts a fake `nix` on PATH that logs each call, answers the skip-list eval with
// evalOut (exiting evalExit), and for a build makes its --out-link point at a profile directory it
// creates, as nix does, and prints that profile. It returns the call log's path and the profile.
func recordingNix(t *testing.T, evalOut, evalExit string) (calls, profile string) {
	t.Helper()
	bin := t.TempDir()
	calls = filepath.Join(t.TempDir(), "calls")
	profile = filepath.Join(t.TempDir(), "store-profile")
	script := `#!/bin/sh
echo "$*" >> '` + calls + `'
case "$*" in
*eval*) echo '` + evalOut + `'; exit ` + evalExit + ` ;;
esac
link=""
prev=""
for a in "$@"; do
  if [ "$prev" = "--out-link" ]; then link="$a"; fi
  prev="$a"
done
mkdir -p '` + profile + `/bin'
if [ -n "$link" ]; then mkdir -p "$(dirname "$link")"; ln -sfn '` + profile + `' "$link"; fi
echo '` + profile + `'
`
	if err := os.WriteFile(filepath.Join(bin, "nix"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("HOME", t.TempDir())
	return calls, profile
}

func nixCalls(t *testing.T, calls string) int {
	t.Helper()
	body, err := os.ReadFile(calls)
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Count(string(body), "\n")
}

func flakeRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for name, body := range map[string]string{"flake.nix": "{ }\n", "flake.lock": "{\"nodes\":{}}\n"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func materializeSpans(t *testing.T, root string, packages []any) (*DarwinPackages, map[string]int) {
	t.Helper()
	ends := map[string]int{}
	log := perf.New(nil, func(e perf.Event) {
		if e.Kind == perf.KindEnd {
			ends[e.Name]++
		}
	})
	var stderr bytes.Buffer
	pkgs, err := Materialize(root, packages, "aarch64-darwin", &stderr, log)
	if err != nil {
		t.Fatalf("Materialize: %v\n%s", err, stderr.String())
	}
	return pkgs, ends
}

// AN UNCHANGED FLOOR RUNS NO NIX, through Materialize itself: the second launch with the same
// inputs takes the recorded profile, and every input the key hashes — flake.lock, flake.nix, the
// declared packages — or a profile nix no longer holds sends the next launch back to nix.
func TestAnUnchangedFloorRunsNoNix(t *testing.T) {
	calls, profile := recordingNix(t, `["gone"]`, "0")
	root := flakeRoot(t)

	first, _ := materializeSpans(t, root, nil)
	if n := nixCalls(t, calls); n != 2 {
		t.Fatalf("the first launch ran nix %d times, want the eval and the build", n)
	}
	second, ends := materializeSpans(t, root, nil)
	if n := nixCalls(t, calls); n != 2 {
		t.Fatalf("an unchanged launch ran nix (%d calls in all)", n)
	}
	if ends["macos_user.materialize.cached"] != 1 || ends["macos_user.materialize.build"] != 0 {
		t.Fatalf("an unchanged launch's spans: %v", ends)
	}
	if second.ProfilePath != first.ProfilePath || second.ProfilePath != profile ||
		strings.Join(second.Skipped, ",") != "gone" || strings.Join(second.PathPrefix, ",") != profile+"/bin" {
		t.Fatalf("the recorded answer %+v differs from the built one %+v", second, first)
	}

	want := 2
	for _, change := range []struct {
		name string
		do   func()
	}{
		{"flake.lock changed", func() { writeFile(t, filepath.Join(root, "flake.lock"), "{\"nodes\":{\"x\":1}}\n") }},
		{"flake.nix changed", func() { writeFile(t, filepath.Join(root, "flake.nix"), "{ x = 1; }\n") }},
		{"the profile was collected", func() {
			if err := os.RemoveAll(profile); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		change.do()
		materializeSpans(t, root, nil)
		want += 2
		if n := nixCalls(t, calls); n != want {
			t.Fatalf("after %s nix ran %d times in all, want %d", change.name, n, want)
		}
		materializeSpans(t, root, nil)
		if n := nixCalls(t, calls); n != want {
			t.Fatalf("the launch after %s's rebuild ran nix again", change.name)
		}
	}
	materializeSpans(t, root, []any{"jq"})
	if n := nixCalls(t, calls); n != want+2 {
		t.Fatal("a changed packages: list did not run nix")
	}
}

// A FAILED SKIP-LIST EVAL IS NEVER RECORDED: its best-effort empty answer must not become every
// later launch's.
func TestAFailedSkipEvalIsNotRecorded(t *testing.T) {
	calls, _ := recordingNix(t, "", "1")
	root := flakeRoot(t)
	materializeSpans(t, root, nil)
	materializeSpans(t, root, nil)
	if n := nixCalls(t, calls); n != 4 {
		t.Fatalf("nix ran %d times over two launches with a failing eval, want 4", n)
	}
}

func writeFile(t *testing.T, p, body string) {
	t.Helper()
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// THE KEY HASHES THE ONLY ENVIRONMENT THE FLAKE READS. The build runs --impure, so a
// `builtins.getEnv` of any other variable would be an input the floor record cannot see, and an
// unchanged key could then name a different floor.
func TestTheFlakeReadsOnlyTheEnvTheFloorKeyHashes(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("..", "..", "flake.nix"))
	if err != nil {
		t.Fatal(err)
	}
	var code strings.Builder
	for _, line := range strings.Split(string(body), "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "#") {
			code.WriteString(line + "\n")
		}
	}
	all := strings.Count(code.String(), "getEnv")
	lit := regexp.MustCompile(`builtins\.getEnv\s+"([A-Za-z0-9_]+)"`).FindAllStringSubmatch(code.String(), -1)
	if len(lit) != all {
		t.Fatalf("flake.nix has %d getEnv uses and %d with a literal name; floorKey cannot know what the others read", all, len(lit))
	}
	for _, m := range lit {
		if m[1] != "YOLO_EXTRA_PACKAGES" {
			t.Errorf("flake.nix reads $%s, which floorKey (floorcache.go) does not hash", m[1])
		}
	}
}
