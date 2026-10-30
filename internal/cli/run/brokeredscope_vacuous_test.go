package run

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/config"
)

// TestLaunchAsksNothingForAVacuousScope drives the launch's own gate (checkConfigChanges, which
// reads the scope through brokeredScopeCheck) with the legacy user read grant for every
// workspace, for a broker on any host (git's shape) and one on a named host (github's): a
// workspace with no remote the broker could serve asks nothing, records nothing for the source,
// and hands the broker an approved empty scope with no grant. The same launch with a real remote
// still refuses without a terminal, which is what fails if the production call is removed.
func TestLaunchAsksNothingForAVacuousScope(t *testing.T) {
	for _, host := range []string{"*", "github.com"} {
		for _, ws := range []string{"non-Git", "no remotes", "one remote"} {
			t.Run(host+"/"+ws, func(t *testing.T) {
				o, buf, _ := brokeredFixtureOn(t, true, host)
				switch ws {
				case "non-Git":
					if err := os.RemoveAll(filepath.Join(o.Workspace, ".git")); err != nil {
						t.Fatal(err)
					}
				case "no remotes":
					if err := os.WriteFile(filepath.Join(o.Workspace, ".git", "config"), []byte("[core]\n\tbare = false\n"), 0o644); err != nil {
						t.Fatal(err)
					}
				}
				ok := o.checkConfigChanges(gbOn(), "podman")
				rec := map[string][]string{}
				if data, err := os.ReadFile(config.ApprovalScopePath(o.Workspace)); err == nil {
					if err := json.Unmarshal(data, &rec); err != nil {
						t.Fatal(err)
					}
				}
				if ws == "one remote" {
					// The positive control: a concrete scope reaches the gate, which a launch with
					// no terminal refuses.
					if ok || !strings.Contains(buf.String(), "gb repository scope") {
						t.Fatalf("a real remote did not reach the gate:\n%s", buf.String())
					}
					return
				}
				if !ok {
					t.Fatalf("a vacuous scope stopped the launch:\n%s", buf.String())
				}
				if strings.Contains(buf.String(), "repository scope") || strings.Contains(buf.String(), "grant read") {
					t.Fatalf("a vacuous scope was put to the reader:\n%s", buf.String())
				}
				if v, has := rec["gbsrc"]; has {
					t.Fatalf("a vacuous scope recorded its scope: %v", v)
				}
				if v, has := rec["gbsrc.grants"]; has {
					t.Fatalf("a vacuous scope recorded grants: %v", v)
				}
				approved, has := o.approvedScopes["gb"]
				if !has || len(approved) != 0 {
					t.Fatalf("the launch did not approve an empty scope for the broker: %v (present=%v)", approved, has)
				}
				if g := o.approvedGrants["gb"]; len(g) != 0 {
					t.Fatalf("a vacuous scope handed its broker grants: %v", g)
				}
			})
		}
	}
}
