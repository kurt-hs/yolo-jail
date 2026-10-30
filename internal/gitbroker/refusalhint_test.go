package gitbroker

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/brokerscope"
)

// A refused push names, per ref, the grant that would have covered it, and following that hint
// makes the same push land: the hint is the next step, not a description of the rule.

// suggestedGrant is the one grant the refusal output spells as JSON for ref.
func suggestedGrant(t *testing.T, out, ref string) brokerscope.Grant {
	t.Helper()
	for _, line := range strings.Split(out, "\n") {
		if !strings.Contains(line, ref+":") {
			continue
		}
		start := strings.Index(line, `{"kind":`)
		if start < 0 {
			continue
		}
		end := strings.Index(line[start:], "}")
		var g brokerscope.Grant
		if end < 0 || json.Unmarshal([]byte(line[start:start+end+1]), &g) != nil {
			t.Fatalf("an unreadable grant on the hint line %q", line)
		}
		return g
	}
	t.Fatalf("no grant suggested for %s in:\n%s", ref, out)
	return brokerscope.Grant{}
}

// approve adds g to the live scope file, as `yolo config apply` approving the workspace config
// does, and lists its kind in the user config.
func (f *fixture) approve(t *testing.T, g brokerscope.Grant) {
	t.Helper()
	sf, err := brokerscope.ReadFile(f.scopeFile)
	if err != nil {
		t.Fatal(err)
	}
	if err := brokerscope.ReplaceScopeContext(context.Background(), f.scopeFile, f.broker.bound, sf.Repos, append(sf.Grants, g)); err != nil {
		t.Fatal(err)
	}
	allowKinds(t, g.Kind)
}

func TestRefusedUpdateNamesTheBranchGrantThatCoversIt(t *testing.T) {
	f := newFixtureOpts(t, fixtureOpts{})
	f.commit("one")
	out, err := f.git("push", "origin", "HEAD:main")
	if err == nil {
		t.Fatalf("an update of main with no grant landed:\n%s", out)
	}
	g := suggestedGrant(t, out, "refs/heads/main")
	if g != (brokerscope.Grant{Kind: "branch", Repo: fixtureRepo, Branch: "main"}) {
		t.Fatalf("suggested %+v", g)
	}
	for _, want := range []string{"not in this workspace's working set", "yolo-jail.jsonc", "yolo config apply", `"branch"`, "grant_kinds"} {
		if !strings.Contains(out, want) {
			t.Fatalf("the hint lacks %q:\n%s", want, out)
		}
	}
	f.approve(t, g)
	f.mustPush("origin", "HEAD:main")
	if got, want := f.upstreamRef("refs/heads/main"), mustGit(t, f.ws, "rev-parse", "HEAD"); got != want {
		t.Fatalf("after following the hint, upstream main %q, local HEAD %q", got, want)
	}
}

func TestRefusedCreationNamesTheCreateBranchGrant(t *testing.T) {
	f := newFixtureOpts(t, fixtureOpts{})
	f.commit("one")
	out, err := f.git("push", "origin", "HEAD:refs/heads/push/trunk")
	if err == nil {
		t.Fatalf("a creation with no grant landed:\n%s", out)
	}
	g := suggestedGrant(t, out, "refs/heads/push/trunk")
	if g != (brokerscope.Grant{Kind: "create-branch", Repo: fixtureRepo}) {
		t.Fatalf("suggested %+v", g)
	}
	if !strings.Contains(out, "any new branch") {
		t.Fatalf("the hint does not say the grant covers any new branch name:\n%s", out)
	}
	f.approve(t, g)
	f.mustPush("origin", "HEAD:refs/heads/push/trunk")
}

// With the kind already listed in the user config, the hint does not send the user to list it.
func TestRefusedUpdateHintOmitsAKindAlreadyAllowed(t *testing.T) {
	f := newFixtureOpts(t, fixtureOpts{})
	allowKinds(t, "branch")
	f.commit("one")
	out, _ := f.git("push", "origin", "HEAD:main")
	suggestedGrant(t, out, "refs/heads/main")
	if strings.Contains(out, "grant_kinds") {
		t.Fatalf("the hint asks for a kind already listed:\n%s", out)
	}
}

// A tag no grant covers asks for approval, and its hint names the tag grant that would cover it
// (GB-D25) and the host push; following the hint makes the same push land.
func TestRefusedTagNamesTheTagGrant(t *testing.T) {
	f := grantedFixture(t)
	f.commit("one")
	out, err := f.git("push", "origin", "HEAD:refs/tags/v1")
	if err == nil {
		t.Fatalf("a tag landed:\n%s", out)
	}
	g := suggestedGrant(t, out, "refs/tags/v1")
	if g != (brokerscope.Grant{Kind: "tag", Repo: fixtureRepo, Tag: "v1"}) {
		t.Fatalf("suggested %+v", g)
	}
	for _, want := range []string{"no tag grant covers it", "asks for approval", "from the host", "grant_kinds", "yolo config apply"} {
		if !strings.Contains(out, want) {
			t.Fatalf("the tag hint lacks %q:\n%s", want, out)
		}
	}
	f.approve(t, g)
	f.mustPush("origin", "HEAD:refs/tags/v1")
	if got, want := f.upstreamRef("refs/tags/v1"), mustGit(t, f.ws, "rev-parse", "HEAD"); got != want {
		t.Fatalf("after following the hint, upstream v1 %q, local HEAD %q", got, want)
	}
}
