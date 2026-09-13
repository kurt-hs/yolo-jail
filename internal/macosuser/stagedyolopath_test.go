package macosuser

import (
	"path/filepath"
	"strings"
	"testing"
)

// stagedyolopath_test.go is docs/design/declaration-parity.md DP-B8 / DP-L4.
//
// Every generated launcher carries `_refresh_servers` and `_try_materialize`, and both
// open `command -v yolo || return`. On this backend the only yolo the sandbox can reach is
// the root-owned copy StageBinaryCommands puts at StagedYoloPath — which was on no PATH,
// so both functions returned at their first line, silently, on every invocation. The fix
// is a PATH entry (SandboxPath), not a macOS-only absolute path inside a generator shared
// with the container backend.

// TestTheSandboxCanResolveYoloByName asserts the property those two functions actually
// need: some entry on the sandbox PATH, joined with "yolo", is the staged binary.
//
// Written as a lookup rather than as "PATH contains /var/yolo-jail", because
// `command -v yolo` does a lookup and a test that pins the spelling would keep passing if
// the staged binary moved out from under it.
func TestTheSandboxCanResolveYoloByName(t *testing.T) {
	staged := StagedYoloPath("")
	path := SandboxPath("/Users/_yolojail", []string{"/nix/store/abc-env/bin"})

	var resolved bool
	for _, dir := range strings.Split(path, ":") {
		if filepath.Join(dir, "yolo") == staged {
			resolved = true
		}
	}
	if !resolved {
		t.Errorf("`command -v yolo` finds nothing on the sandbox PATH, so the launchers' "+
			"_refresh_servers and _try_materialize both return at their first line.\n"+
			"staged binary: %s\nPATH: %s", staged, path)
	}
}

// TestTheLaunchCarriesThePathThatResolvesYolo is the CALL-SITE half. SandboxPath is a pure
// function; the thing that matters is that the argv a sandbox is launched with exports a
// PATH the lookup succeeds on — and the launch exports it twice, as PATH and as
// $YOLO_DARWIN_LOGIN_PATH (which the generated login rc files re-prepend, and which
// entrypoint's agentPath/imageProbePath read as "the PATH the agent will have").
func TestTheLaunchCarriesThePathThatResolvesYolo(t *testing.T) {
	argv := LaunchArgv([]string{"claude"}, "/var/yolo-jail/p.sb", "",
		"/Users/Shared/proj", "", "", []string{"/nix/store/abc-env/bin"})
	staged := StagedYoloPath("")
	stagedDir := filepath.Dir(staged)

	var sawPath bool
	for _, a := range argv {
		v, ok := strings.CutPrefix(a, "PATH=")
		if !ok {
			continue
		}
		sawPath = true
		found := false
		for _, dir := range strings.Split(v, ":") {
			if dir == stagedDir {
				found = true
			}
		}
		if !found {
			t.Errorf("the launch argv's PATH does not carry the staged yolo's directory "+
				"(%s):\n%s", stagedDir, v)
		}
	}
	if !sawPath {
		t.Fatalf("the launch argv sets no PATH at all:\n%v", argv)
	}
}

// TestStagedYoloDoesNotOutrankThePackagesTheUserAsked pins WHERE the entry sits. The
// container spells the same relation with `/bin` last (AGENTS.md, PATH order): yolo is
// reachable by name and loses every collision to what the launch delivers. Putting the
// staged dir ahead of the store prefix would make `packages: ["yolo"]`-shaped collisions
// resolve to yolo's own copy, and would move an entry in a PATH order that has three
// independently-written copies.
func TestStagedYoloDoesNotOutrankThePackagesTheUserAsked(t *testing.T) {
	store := "/nix/store/abc-env/bin"
	home := "/Users/_yolojail"
	dirs := strings.Split(SandboxPath(home, []string{store}), ":")
	idx := func(want string) int {
		for i, d := range dirs {
			if d == want {
				return i
			}
		}
		return -1
	}

	stagedDir := filepath.Dir(StagedYoloPath(""))
	for _, pair := range [][2]string{
		{store, stagedDir},
		{home + "/.yolo/bin/block", stagedDir},
		{home + "/.yolo/bin/launch", stagedDir},
		{home + "/.local/bin", stagedDir},
		{stagedDir, "/usr/bin"},
		{stagedDir, "/bin"},
	} {
		before, after := idx(pair[0]), idx(pair[1])
		if before < 0 || after < 0 {
			t.Fatalf("PATH is missing %q or %q:\n%v", pair[0], pair[1], dirs)
		}
		if before > after {
			t.Errorf("%q must precede %q on the sandbox PATH:\n%v", pair[0], pair[1], dirs)
		}
	}
}
