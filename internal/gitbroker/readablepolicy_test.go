package gitbroker

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/brokerscope"
	"github.com/mschulkind-oss/yolo-jail/internal/config"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

func writeReadableGitUserTOML(t *testing.T, allowTable, legacyKinds string) {
	t.Helper()
	configDir := filepath.Join(os.Getenv("HOME"), ".config", "yolo-jail")
	if err := os.Remove(paths.UserConfigPath()); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	body := "packs = ['git']\n\n[allow.git]\n" + allowTable
	if legacyKinds != "" {
		body += "\n[brokered.git]\ngrant_kinds = [" + legacyKinds + "]\n"
	}
	if err := os.WriteFile(filepath.Join(configDir, paths.UserConfigTOMLName), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// readableGitRequestFixture runs the real selected-pack declaration reader and scope producer,
// then gives the resulting approved grants to the actual git broker fixture.
func readableGitRequestFixture(t *testing.T, capability, request string) *fixture {
	t.Helper()
	f := newFixture(t, "")
	writeReadableGitUserTOML(t, "always = ['fetch']\non_request = ['"+capability+"']\n", "")
	workspaceConfig := filepath.Join(f.ws, paths.WorkspaceConfigTOMLName)
	if err := os.WriteFile(workspaceConfig, []byte("[requests.git]\n"+request+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	packs, complete := config.UserScopeSelectedPacks()
	if !complete {
		t.Fatal("selected user packs did not resolve completely")
	}
	t.Cleanup(packload.ReleaseEmbedded)
	declarations, settings, err := config.ReadableDeclarationsForPacks(packs)
	if err != nil {
		t.Fatalf("read selected pack declarations: %v", err)
	}
	selected := make([]config.ReadableSelectedBroker, 0, len(declarations))
	brokers := make([]config.ScopeBroker, 0, len(declarations))
	for _, declaration := range declarations {
		selected = append(selected, config.ReadableSelectedBroker{Pack: declaration.Pack, Label: declaration.Label, Brokered: declaration.Brokered})
		brokers = append(brokers, config.ScopeBroker{Source: declaration.Brokered.Source, Label: declaration.Label,
			RemoteHost: declaration.Brokered.RemoteHost, Pack: declaration.Pack, Brokered: declaration.Brokered,
			GrantKinds: declaration.Brokered.Grants})
	}
	read, err := config.ReadWorkspaceForGate(f.ws)
	if err != nil {
		t.Fatalf("read workspace for gate: %v", err)
	}
	check := config.NewSelectedScopeCheck(f.ws, brokers, read, settings, selected)
	if check == nil || check.Fatal != nil {
		t.Fatalf("selected readable scope check = %+v", check)
	}
	var gitScope *config.ScopeSource
	for i := range check.Sources {
		if check.Sources[i].Source == Source {
			gitScope = &check.Sources[i]
			break
		}
	}
	if gitScope == nil || len(gitScope.Grants) < 2 {
		t.Fatalf("actual selected scope did not include read plus requested write grant: %+v", check.Sources)
	}
	ok, err := config.CheckConfigAndScopeChanges(f.ws, read.Config, check, false, true, nil)
	if err != nil || !ok {
		t.Fatalf("approve actual selected scope: ok=%t err=%v", ok, err)
	}
	approved := config.ApprovedGrants(f.ws, Source)
	if len(approved) < 2 {
		t.Fatalf("approval record lost the requested write grant: %+v", approved)
	}
	sf, err := brokerscope.ReadFile(f.scopeFile)
	if err != nil {
		t.Fatal(err)
	}
	sf.Grants = approved
	if _, err := brokerscope.Write(sf); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestReadableRequestedBranchUpdateUsesCanonicalCeiling(t *testing.T) {
	f := readableGitRequestFixture(t, "push", "push = ['main']")
	f.commit("canonical branch update")
	f.mustPush("origin", "HEAD:refs/heads/main")
	if got, want := f.upstreamRef("refs/heads/main"), mustGit(t, f.ws, "rev-parse", "HEAD"); got != want {
		t.Fatalf("canonical requested update left upstream at %q, want %q", got, want)
	}
	if p := lastPush(t); p.Set != setWorkingSet || len(p.Refs) != 1 || p.Refs[0].CoveredBy != "branch:main" {
		t.Fatalf("canonical branch audit %+v", p)
	}
}

func TestReadableRequestedNewBranchUsesCanonicalCeiling(t *testing.T) {
	f := readableGitRequestFixture(t, "new_branches", "new_branches = true")
	f.commit("canonical new branch")
	f.mustPush("origin", "HEAD:refs/heads/agent/new")
	if got, want := f.upstreamRef("refs/heads/agent/new"), mustGit(t, f.ws, "rev-parse", "HEAD"); got != want {
		t.Fatalf("canonical new branch left upstream at %q, want %q", got, want)
	}
	entries := liveEntries(t)
	if len(entries) != 1 || entries[0].By != setCreateBranch || entries[0].Resource != "refs/heads/agent/new" {
		t.Fatalf("canonical create-branch working set = %+v", entries)
	}
}

func TestReadableCanonicalCeilingWithdrawalAndMalformedAuthorityFailClosed(t *testing.T) {
	for _, tc := range []struct {
		name, allowTable string
	}{
		{name: "empty capability list", allowTable: "always = ['fetch']\non_request = []\n"},
		{name: "empty authority table", allowTable: ""},
		{name: "malformed capability type", allowTable: "always = ['fetch']\non_request = 5\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := readableGitRequestFixture(t, "push", "push = ['main']")
			f.commit("before withdrawal")
			f.mustPush("origin", "HEAD:main")
			before := f.upstreamRef("refs/heads/main")
			// Keep the branch grant in the already-approved live scope and add the broader
			// legacy ceiling. Canonical authority, including malformed/empty authority,
			// must suppress fallback at the next push.
			writeReadableGitUserTOML(t, tc.allowTable, "'branch'")
			f.commit("after withdrawal")
			out, err := f.git("push", "origin", "HEAD:main")
			if err == nil || !strings.Contains(out, reasonNeedsApproval) {
				t.Fatalf("push after canonical withdrawal/malformed authority: err=%v\n%s", err, out)
			}
			if f.upstreamRef("refs/heads/main") != before {
				t.Fatal("push after canonical authority removal reached upstream")
			}
		})
	}
}
