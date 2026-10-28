package gitbroker

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/brokerscope"
	"github.com/mschulkind-oss/yolo-jail/internal/config"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

func writeReadableGitUserTOML(t *testing.T, allowTable, legacyKinds string) {
	writeReadableGitUserTOMLWithPacks(t, "['git']", allowTable, legacyKinds)
}

func writeReadableGitUserTOMLWithPacks(t *testing.T, packs, allowTable, legacyKinds string) {
	t.Helper()
	configDir := filepath.Join(os.Getenv("HOME"), ".config", "yolo-jail")
	if err := os.Remove(paths.UserConfigPath()); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	body := ""
	if legacyKinds != "" {
		body += "include_if_found = ['legacy.jsonc']\n"
	}
	body += "packs = " + packs + "\n\n[allow.git]\n" + allowTable
	if err := os.WriteFile(filepath.Join(configDir, paths.UserConfigTOMLName), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if legacyKinds != "" {
		legacy := `{"brokered":{"git":{"grant_kinds":[` + strings.ReplaceAll(legacyKinds, "'", `"`) + `]}}}`
		if err := os.WriteFile(filepath.Join(configDir, "legacy.jsonc"), []byte(legacy), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// writeReadableGitJSONCPackSelection writes a TOML user scope whose pack entries come from a JSONC include.
func writeReadableGitJSONCPackSelection(t *testing.T, packEntry, allowTable string) {
	t.Helper()
	configDir := filepath.Join(os.Getenv("HOME"), ".config", "yolo-jail")
	if err := os.Remove(paths.UserConfigPath()); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	body := "include_if_found = ['selection.jsonc']\n\n[allow.git]\n" + allowTable
	if err := os.WriteFile(filepath.Join(configDir, paths.UserConfigTOMLName), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	selection := `{"packs":[` + packEntry + `]}`
	if err := os.WriteFile(filepath.Join(configDir, "selection.jsonc"), []byte(selection), 0o600); err != nil {
		t.Fatal(err)
	}
}

func readableGitRequestFixture(t *testing.T, capability, request string) *fixture {
	return readableGitRequestFixtureWithConfig(t, capability, request, func() {
		writeReadableGitUserTOML(t, "always = ['fetch']\non_request = ['"+capability+"']\n", "")
	})
}

func readableGitFilteredRequestFixture(t *testing.T, capability, request string) *fixture {
	return readableGitRequestFixtureWithConfig(t, capability, request, func() {
		writeReadableGitJSONCPackSelection(t, `{"source":"git","exclude":["README.md"]}`, "always = ['fetch']\non_request = ['"+capability+"']\n")
	})
}

// readableGitRequestFixtureWithConfig stages the exact selected entries, runs the selected-pack
// declaration reader and scope producer, and gives accepted grants to the real broker fixture.
func readableGitRequestFixtureWithConfig(t *testing.T, capability, request string, writeConfig func()) *fixture {
	t.Helper()
	f := newFixture(t, "")
	writeConfig()
	workspaceConfig := filepath.Join(f.ws, paths.WorkspaceConfigTOMLName)
	if err := os.WriteFile(workspaceConfig, []byte("[requests.git]\n"+request+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	packs := launchCompatibleSelectedPacks(t)
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

func launchCompatibleSelectedPacks(t *testing.T) []*packload.Pack {
	t.Helper()
	entries, problems, err := config.LoadPackEntries()
	if err != nil || len(problems) != 0 {
		t.Fatalf("load selected pack entries: problems=%v err=%v", problems, err)
	}
	stageRoot := t.TempDir()
	resolve := func(entry config.PackEntry) (*packload.Pack, error) {
		resolved, err := config.ResolvePack(entry, config.ResolvePackSpec{
			Dest: filepath.Join(stageRoot, entry.Slug()), ReadOnlyStore: true,
		})
		if err != nil {
			return nil, err
		}
		if resolved.Pack == nil || len(resolved.Problems) != 0 {
			return nil, os.ErrInvalid
		}
		return resolved.Pack, nil
	}
	selected, err := config.SelectPacks(entries, config.PackSelectSpec{
		Resolve: resolve,
		Join: func(pack *packload.Pack) (*packload.Pack, error) {
			return resolve(config.EmbeddedPackEntry(pack.Name))
		},
		FailFast: true, Selection: config.UserScopeSelection(),
	})
	if err != nil || !selected.Complete() {
		t.Fatalf("resolve launch-compatible selected pack declarations: selection=%+v err=%v", selected, err)
	}
	return selected.Packs()
}

func TestReadableFilteredGitSelectionRemainsAvailableToTheBroker(t *testing.T) {
	f := readableGitFilteredRequestFixture(t, "push", "push = ['main']")
	f.commit("filtered canonical branch update")
	f.mustPush("origin", "HEAD:refs/heads/main")
	if got, want := f.upstreamRef("refs/heads/main"), mustGit(t, f.ws, "rev-parse", "HEAD"); got != want {
		t.Fatalf("filtered canonical branch update left upstream at %q, want %q", got, want)
	}
	if p := lastPush(t); p.Set != setWorkingSet || len(p.Refs) != 1 || p.Refs[0].CoveredBy != "branch:main" {
		t.Fatalf("filtered canonical branch audit %+v", p)
	}
}

func TestMalformedSelectedPackEntryWithdrawsApprovedGitPush(t *testing.T) {
	f := readableGitRequestFixture(t, "push", "push = ['main']")
	f.commit("before malformed selection")
	f.mustPush("origin", "HEAD:refs/heads/main")
	before := f.upstreamRef("refs/heads/main")

	writeReadableGitUserTOMLWithPacks(t, "['git', true]", "always = ['fetch']\non_request = ['push']\n", "")
	f.commit("after malformed selection")
	out, err := f.git("push", "origin", "HEAD:refs/heads/main")
	if err == nil || !strings.Contains(out, reasonNeedsApproval) {
		t.Fatalf("push after malformed selected pack entry: err=%v\n%s", err, out)
	}
	if f.upstreamRef("refs/heads/main") != before {
		t.Fatal("push after malformed selected pack entry reached upstream")
	}
}

func TestUnresolvableSelectedPackCannotAuthorizeCanonicalBroker(t *testing.T) {
	newFixture(t, "")
	missing := filepath.Join(os.Getenv("HOME"), ".config", "yolo-jail", "packs", "missing")
	writeReadableGitJSONCPackSelection(t, `{"source":"file://`+filepath.ToSlash(missing)+`","name":"missing"}`, "always = ['fetch']\non_request = ['push']\n")

	kinds, canonical, err := config.BrokeredGrantKindsUsingReadablePolicy(Source)
	if err == nil || !canonical || len(kinds) != 0 {
		t.Fatalf("unresolvable selected pack ceiling = %v, canonical %t, err %v; want fail-closed canonical error", kinds, canonical, err)
	}
}

func TestInvalidProfileClosureCannotAuthorizeCanonicalBroker(t *testing.T) {
	for _, tc := range []struct {
		name, extra string
	}{
		{name: "invalid selection", extra: `,"profile":false`},
		{name: "invalid selected profile", extra: `,"profile":"broken","profiles":{"broken":{}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			newFixture(t, "")
			writeReadableGitJSONCPackSelection(t, `{"source":"git"}`, "always = ['fetch']\non_request = ['push']\n")
			selection := `{"packs":[{"source":"git"}]` + tc.extra + `}`
			selectionPath := filepath.Join(os.Getenv("HOME"), ".config", "yolo-jail", "selection.jsonc")
			if err := os.WriteFile(selectionPath, []byte(selection), 0o600); err != nil {
				t.Fatal(err)
			}
			kinds, canonical, err := config.BrokeredGrantKindsUsingReadablePolicy(Source)
			if err == nil || !canonical || len(kinds) != 0 {
				t.Fatalf("invalid profile closure ceiling = %v, canonical %t, err %v; want fail-closed canonical error", kinds, canonical, err)
			}
		})
	}
}

func TestFilteredOutBrokerModuleCannotAuthorizeApprovedPush(t *testing.T) {
	f := readableGitRequestFixture(t, "push", "push = ['main']")
	f.commit("before filtered broker module")
	f.mustPush("origin", "HEAD:main")
	before := f.upstreamRef("refs/heads/main")

	writeReadableGitJSONCPackSelection(t, `{"source":"git","exclude":["loopholes/git-broker"]}`, "always = ['fetch']\non_request = ['push']\n")
	f.commit("after filtered broker module")
	out, err := f.git("push", "origin", "HEAD:main")
	if err == nil || !strings.Contains(out, reasonNeedsApproval) {
		t.Fatalf("push after filtering out selected broker module: err=%v\n%s", err, out)
	}
	if f.upstreamRef("refs/heads/main") != before {
		t.Fatal("push whose selected filter removed the broker module reached upstream")
	}
}

func TestDuplicateSelectedBrokerOwnerCannotAuthorizePush(t *testing.T) {
	f := readableGitRequestFixture(t, "push", "push = ['main']")
	f.commit("before duplicate broker owner")
	f.mustPush("origin", "HEAD:main")
	before := f.upstreamRef("refs/heads/main")

	duplicate := writeDuplicateGitBrokerPack(t)
	writeReadableGitJSONCPackSelection(t, `{"source":"git"},{"source":"file://`+filepath.ToSlash(duplicate)+`","name":"duplicate"}`, "always = ['fetch']\non_request = ['push']\n")
	f.commit("after duplicate broker owner")
	out, err := f.git("push", "origin", "HEAD:main")
	if err == nil || !strings.Contains(out, reasonNeedsApproval) {
		t.Fatalf("push with duplicate selected broker owner: err=%v\n%s", err, out)
	}
	if f.upstreamRef("refs/heads/main") != before {
		t.Fatal("push with duplicate selected broker owner reached upstream")
	}
}

func writeDuplicateGitBrokerPack(t *testing.T) string {
	t.Helper()
	var gitPack *packload.Pack
	for _, pack := range packload.Embedded() {
		if pack.Name == "git" {
			gitPack = pack
			break
		}
	}
	if gitPack == nil {
		t.Fatal("embedded git pack is unavailable")
	}
	configDir := filepath.Join(os.Getenv("HOME"), ".config", "yolo-jail")
	root := filepath.Join(configDir, "packs", "duplicate")
	module := filepath.Join(root, "loopholes", "git-broker")
	if err := os.MkdirAll(module, 0o700); err != nil {
		t.Fatal(err)
	}
	manifest, err := os.ReadFile(filepath.Join(gitPack.Root, "loopholes", "git-broker", "manifest.jsonc"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(module, "manifest.jsonc"), manifest, 0o600); err != nil {
		t.Fatal(err)
	}
	packManifest := `{"name":"duplicate","contributes":[{"from":"loopholes/git-broker","kind":"loophole"}]}`
	if err := os.WriteFile(filepath.Join(root, "pack.json"), []byte(packManifest), 0o600); err != nil {
		t.Fatal(err)
	}
	return root
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
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := readableGitRequestFixture(t, "push", "push = ['main']")
			f.commit("before withdrawal")
			f.mustPush("origin", "HEAD:main")
			before := f.upstreamRef("refs/heads/main")
			writeReadableGitUserTOML(t, tc.allowTable, "'branch'")
			assertCanonicalEmptyWithLegacyKind(t, "branch")
			f.commit("after withdrawal")
			out, err := f.git("push", "origin", "HEAD:main")
			if err == nil || !strings.Contains(out, reasonNeedsApproval) {
				t.Fatalf("push after canonical withdrawal: err=%v\n%s", err, out)
			}
			if f.upstreamRef("refs/heads/main") != before {
				t.Fatal("push after canonical authority removal reached upstream")
			}
		})
	}

	f := readableGitRequestFixture(t, "push", "push = ['main']")
	f.commit("before malformed authority")
	f.mustPush("origin", "HEAD:main")
	before := f.upstreamRef("refs/heads/main")
	writeReadableGitUserTOML(t, "always = ['fetch']\non_request = 5\n", "'branch'")
	f.commit("after malformed authority")
	out, err := f.git("push", "origin", "HEAD:main")
	if err == nil || !strings.Contains(out, reasonNeedsApproval) {
		t.Fatalf("push after malformed canonical authority: err=%v\n%s", err, out)
	}
	if f.upstreamRef("refs/heads/main") != before {
		t.Fatal("push after malformed canonical authority reached upstream")
	}
}

func assertCanonicalEmptyWithLegacyKind(t *testing.T, legacyKind string) {
	t.Helper()
	user, _, err := config.UserScopeConfigWithSources(true, func(string) {})
	if err != nil || user == nil {
		t.Fatalf("strict user config with canonical withdrawal and legacy include: user=%v err=%v", user, err)
	}
	if !slices.Contains(config.BrokeredGrantKinds(Source), legacyKind) {
		t.Fatalf("legacy JSONC include did not supply the broader %s grant", legacyKind)
	}
	kinds, canonical, err := config.BrokeredGrantKindsUsingReadablePolicy(Source)
	if err != nil || !canonical || len(kinds) != 0 {
		t.Fatalf("empty canonical authority = %v, canonical %t, err %v; want valid canonical denial despite legacy %s", kinds, canonical, err, legacyKind)
	}
}

func TestEmptyLegacyProfileSelectionWithdrawsApprovedGitPush(t *testing.T) {
	f := readableGitRequestFixture(t, "push", "push = ['main']")
	f.commit("before empty profile selection")
	f.mustPush("origin", "HEAD:main")
	before := f.upstreamRef("refs/heads/main")

	writeReadableGitJSONCPackSelection(t, `{"source":"git"}`, "always = ['fetch']\non_request = ['push']\n")
	selectionPath := filepath.Join(os.Getenv("HOME"), ".config", "yolo-jail", "selection.jsonc")
	selection := `{"packs":[{"source":"git"}],"profile":""}`
	if err := os.WriteFile(selectionPath, []byte(selection), 0o600); err != nil {
		t.Fatal(err)
	}
	f.commit("after empty profile selection")
	out, err := f.git("push", "origin", "HEAD:main")
	if err == nil || !strings.Contains(out, reasonNeedsApproval) {
		t.Fatalf("push after invalid empty legacy profile selection: err=%v\n%s", err, out)
	}
	if f.upstreamRef("refs/heads/main") != before {
		t.Fatal("push after invalid empty legacy profile selection reached upstream")
	}
}

func TestNullLegacyProfileSelectionKeepsApprovedGitPush(t *testing.T) {
	f := readableGitRequestFixture(t, "push", "push = ['main']")
	writeReadableGitJSONCPackSelection(t, `{"source":"git"}`, "always = ['fetch']\non_request = ['push']\n")
	selectionPath := filepath.Join(os.Getenv("HOME"), ".config", "yolo-jail", "selection.jsonc")
	selection := `{"packs":[{"source":"git"}],"profile":null}`
	if err := os.WriteFile(selectionPath, []byte(selection), 0o600); err != nil {
		t.Fatal(err)
	}
	f.commit("supported null profile selection")
	f.mustPush("origin", "HEAD:main")
	if got, want := f.upstreamRef("refs/heads/main"), mustGit(t, f.ws, "rev-parse", "HEAD"); got != want {
		t.Fatalf("push with null profile deselection left upstream at %q, want %q", got, want)
	}
}

func TestReadableNewBranchWithdrawalDoesNotResurrectLegacyGrant(t *testing.T) {
	f := readableGitRequestFixture(t, "new_branches", "new_branches = true")
	f.commit("first approved branch")
	f.mustPush("origin", "HEAD:refs/heads/agent/first")
	before := f.upstreamRef("refs/heads/agent/first")

	writeReadableGitUserTOML(t, "always = ['fetch']\non_request = []\n", "'create-branch'")
	assertCanonicalEmptyWithLegacyKind(t, "create-branch")
	f.commit("after new-branch withdrawal")
	out, err := f.git("push", "origin", "HEAD:refs/heads/agent/second")
	if err == nil || !strings.Contains(out, reasonNeedsApproval) {
		t.Fatalf("new branch after canonical withdrawal: err=%v\n%s", err, out)
	}
	if f.upstreamRef("refs/heads/agent/first") != before {
		t.Fatal("withdrawal changed the already-approved branch")
	}
	if got := mustGit(t, f.bare, "for-each-ref", "--format=%(refname)", "refs/heads/agent/second"); got != "" {
		t.Fatalf("withdrawn new branch reached upstream: %s", got)
	}
}
