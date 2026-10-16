package version

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// stampVars returns the sorted set of internal/version variables a file stamps with
// `-X github.com/mschulkind-oss/yolo-jail/internal/version.<Var>=`.
func stampVars(t *testing.T, rel string) []string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", rel))
	if err != nil {
		t.Fatalf("reading %s: %v", rel, err)
	}
	re := regexp.MustCompile(`-X github\.com/mschulkind-oss/yolo-jail/internal/version\.(\w+)=`)
	seen := map[string]bool{}
	for _, m := range re.FindAllStringSubmatch(string(b), -1) {
		seen[m[1]] = true
	}
	out := make([]string, 0, len(seen))
	for v := range seen {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

// TestTheFlakeStampsWhatBuildGoStamps pins the owner's 2026-10-16 ruling (every build's
// version string carries the git hash, nix builds included; docs/plans/downstream-asks.md
// OQ-DA2) at the one place it can rot silently in a short test: flake.nix's
// goStampLdflags must set the same version variables scripts/build-go.sh sets, and both
// compiling derivations (goBinaries, guestPrefix) must pass it. A variable renamed in
// this package and in build-go.sh but not in the flake would leave every nix-built binary
// unstamped with the build green. The end-to-end check is the integration suite's
// TestTheJailsYoloCarriesTheCheckoutsCommit.
func TestTheFlakeStampsWhatBuildGoStamps(t *testing.T) {
	flakeVars := stampVars(t, "flake.nix")
	goVars := stampVars(t, "scripts/build-go.sh")
	if len(goVars) == 0 {
		t.Fatal("parsed no -X stamp from scripts/build-go.sh; fix the pattern rather than the test")
	}
	if strings.Join(flakeVars, ",") != strings.Join(goVars, ",") {
		t.Errorf("flake.nix stamps %v, scripts/build-go.sh stamps %v: a nix build and a Go build "+
			"must carry the same version and commit", flakeVars, goVars)
	}
	for _, v := range goVars {
		switch v {
		case "buildVersion", "GitCommit":
		default:
			t.Errorf("build-go.sh stamps %s, which this test does not know to exist", v)
		}
	}

	flake, err := os.ReadFile(filepath.Join("..", "..", "flake.nix"))
	if err != nil {
		t.Fatal(err)
	}
	builds := regexp.MustCompile(`(?m)^\s*go build .*$`).FindAllString(string(flake), -1)
	if len(builds) != 2 {
		t.Fatalf("flake.nix has %d `go build` lines, want 2 (goBinaries, guestPrefix): %q",
			len(builds), builds)
	}
	for _, line := range builds {
		if !strings.Contains(line, "-ldflags ${goStampLdflags}") {
			t.Errorf("a flake.nix `go build` does not stamp the version and commit: %s",
				strings.TrimSpace(line))
		}
	}
}
