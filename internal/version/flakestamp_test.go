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

// TestTheFlakeAndBuildGoShareReleaseMetadata pins the version and commit stamps
// shared by source flake builds and the local bundle compiler. Matched snapshot
// metadata belongs to the local bundle pipeline, not the nix image build.
func TestTheFlakeAndBuildGoShareReleaseMetadata(t *testing.T) {
	flakeVars := stampVars(t, "flake.nix")
	goVars := stampVars(t, "scripts/build-go.sh")
	if len(goVars) == 0 {
		t.Fatal("parsed no -X stamp from scripts/build-go.sh; fix the pattern rather than the test")
	}
	for _, v := range []string{"buildVersion", "GitCommit"} {
		if !containsStampVar(flakeVars, v) || !containsStampVar(goVars, v) {
			t.Errorf("flake.nix stamps %v and scripts/build-go.sh stamps %v; both must stamp %s", flakeVars, goVars, v)
		}
	}
	for _, v := range goVars {
		switch v {
		case "buildVersion", "GitCommit", "SnapshotIdentity", "BuildKind":
		default:
			t.Errorf("build-go.sh stamps %s, which this test does not know to exist", v)
		}
	}
	for _, v := range flakeVars {
		if v != "buildVersion" && v != "GitCommit" {
			t.Errorf("flake.nix stamps %s, which is not part of the source-flake stamp contract", v)
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

func containsStampVar(vars []string, want string) bool {
	for _, got := range vars {
		if got == want {
			return true
		}
	}
	return false
}
