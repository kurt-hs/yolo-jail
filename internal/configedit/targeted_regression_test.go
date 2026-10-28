package configedit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/config"
	"github.com/mschulkind-oss/yolo-jail/internal/configdoc"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
	"github.com/mschulkind-oss/yolo-jail/internal/pidlock"
)

func assertNoTargetArtifacts(t *testing.T, path string) {
	t.Helper()
	temps, err := filepath.Glob(filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+".edit-*"))
	if err != nil || len(temps) != 0 {
		t.Fatalf("edit left atomic temp files: %v err=%v", temps, err)
	}
	if pidlock.Held(path + ".yolo-edit.lock") {
		t.Fatalf("edit left its target lock held: %s", path+".yolo-edit.lock")
	}
}

func userVariantCapabilities(t *testing.T, home string) []any {
	t.Helper()
	t.Setenv("HOME", home)
	value, err := config.UserScopeConfig(true, func(string) {})
	if err != nil {
		t.Fatalf("read composed User graph: %v", err)
	}
	variant := effectiveMap(t, value, "providers", "zai", "variants", "fast")
	capabilities, ok := variant.Get("capabilities")
	if !ok {
		t.Fatal("composed variant has no capabilities")
	}
	list, ok := capabilities.([]any)
	if !ok {
		t.Fatalf("composed capabilities has type %T", capabilities)
	}
	return list
}

func TestApplyTargetedAcceptsLiteralStarMCPServerNameInBothFormats(t *testing.T) {
	for _, tc := range []struct {
		name, filename, before string
		format                 configdoc.Format
	}{
		{"JSONC", paths.WorkspaceConfigLegacyJSONCName, `{"mcp_servers":{"*":{"command":"echo","args":["old"]}}}`, configdoc.JSONC},
		{"TOML", paths.WorkspaceConfigTOMLName, "[mcp_servers.\"*\"]\ncommand=['echo']\nargs=['old']\n", configdoc.TOML},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home, workspace := t.TempDir(), t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("YOLO_VERSION", "")
			path := filepath.Join(workspace, tc.filename)
			writeEditFixture(t, path, tc.before)
			argsPath := regressionPath("mcp_servers", "*", "args")
			set := Mutation{Action: Set, Path: argsPath, Value: []any{"keep"}}
			target, err := ResolveTarget(workspace, set, config.EditTargetChoice{Kind: config.EditTargetAutomatic})
			if err != nil {
				t.Fatalf("resolve literal-star MCP server Set: %v", err)
			}
			if target.LogicalPath != path {
				t.Fatalf("literal-star target=%s, want %s", target.LogicalPath, path)
			}
			if _, err := ApplyTargeted(workspace, target, set, ApplyOptions{}); err != nil {
				t.Fatalf("apply literal-star MCP server Set: %v", err)
			}
			afterSet, err := os.ReadFile(path)
			if err != nil || !strings.Contains(string(afterSet), "keep") || !strings.Contains(string(afterSet), "echo") {
				t.Fatalf("literal-star Set did not preserve the server: %q err=%v", afterSet, err)
			}
			remove := Mutation{Action: Remove, Path: argsPath}
			target, err = ResolveTarget(workspace, remove, config.EditTargetChoice{Kind: config.EditTargetAutomatic})
			if err != nil {
				t.Fatalf("resolve literal-star MCP server Remove: %v", err)
			}
			if _, err := ApplyTargeted(workspace, target, remove, ApplyOptions{}); err != nil {
				t.Fatalf("apply literal-star MCP server Remove: %v", err)
			}
			afterRemove, err := os.ReadFile(path)
			if err != nil || strings.Contains(string(afterRemove), "args") || !strings.Contains(string(afterRemove), "echo") {
				t.Fatalf("literal-star Remove changed the wrong member: %q err=%v", afterRemove, err)
			}
			assertNoTargetArtifacts(t, path)

			beforeMalformed, _ := os.ReadFile(path)
			for _, malformedPath := range [][]configdoc.Step{
				{{Key: "mcp_servers"}, {Key: "*", Index: 1}, {Key: "args"}},
				{{Key: "mcp_servers"}, {Key: "not-an-index", Index: 0, IsIndex: true}, {Key: "args"}},
				{{Key: "mcp_servers"}, {Index: -1, IsIndex: true}, {Key: "args"}},
			} {
				malformed := Mutation{Action: Set, Path: malformedPath, Value: []any{"bad"}}
				if _, err := ResolveTarget(workspace, malformed, config.EditTargetChoice{Kind: config.EditTargetAutomatic}); err == nil {
					t.Fatalf("malformed structured path was accepted: %#v", malformedPath)
				}
			}
			afterMalformed, _ := os.ReadFile(path)
			if string(afterMalformed) != string(beforeMalformed) {
				t.Fatalf("malformed path changed target: before=%q after=%q", beforeMalformed, afterMalformed)
			}
		})
	}
}

func TestApplyTargetedMapsUserIndexedIntentsToAuthoredSourceIndices(t *testing.T) {
	for _, tc := range []struct {
		name, configName, includePath, main, fragment string
		format                                        configdoc.Format
	}{
		{
			name: "JSONC recursive include", configName: paths.UserConfigLegacyJSONCName,
			includePath: "parts/provider.jsonc", format: configdoc.JSONC,
			main: `// main note
{"packs":["zai"],"include_if_found":["parts/provider.jsonc"],"providers":{"zai":{"variants":{"fast":{"model":"glm-5.3","capabilities":["web_search"]}}}}}`,
			fragment: `// fragment note
{"providers":{"zai":{"variants":{"fast":{"model":"glm-5.3","capabilities":["messages","responses"]}}}}}`,
		},
		{
			name: "TOML config.d", configName: paths.UserConfigTOMLName,
			includePath: "config.d/10-provider.toml", format: configdoc.TOML,
			main:     "# main note\npacks=['zai']\n[providers.zai.variants.fast]\nmodel='glm-5.3'\ncapabilities=['web_search']\n",
			fragment: "# fragment note\n[providers.zai.variants.fast]\nmodel='glm-5.3' # model note\ncapabilities=['messages','responses']\n",
		},
	} {
		for _, action := range []Action{Remove, Set} {
			t.Run(tc.name+map[Action]string{Remove: "/remove", Set: "/set"}[action], func(t *testing.T) {
				home, workspace := t.TempDir(), t.TempDir()
				t.Setenv("HOME", home)
				t.Setenv("YOLO_VERSION", "")
				userDir := filepath.Join(home, ".config", "yolo-jail")
				mainPath := filepath.Join(userDir, tc.configName)
				fragmentPath := filepath.Join(userDir, filepath.FromSlash(tc.includePath))
				writeEditFixture(t, mainPath, tc.main)
				writeEditFixture(t, fragmentPath, tc.fragment)
				mainBefore, _ := os.ReadFile(mainPath)
				fragmentBefore, _ := os.ReadFile(fragmentPath)
				neighborPath := filepath.Join(userDir, "untouched.txt")
				writeEditFixture(t, neighborPath, "neighbor\n")
				neighborBefore, _ := os.ReadFile(neighborPath)
				fragmentInfo, _ := os.Stat(fragmentPath)

				capabilities := append(regressionPath("providers", "zai", "variants", "fast", "capabilities"), configdoc.Step{Index: 1, IsIndex: true})
				mutation := Mutation{Action: Remove, Path: capabilities}
				if action == Set {
					mutation = Mutation{Action: Set, Path: capabilities, Value: "web_search"}
				}
				target, err := ResolveTarget(workspace, mutation, config.EditTargetChoice{Kind: config.EditTargetAutomatic})
				if err != nil {
					t.Fatalf("resolve indexed User intent: %v", err)
				}
				if target.LogicalPath != fragmentPath || target.Role != config.ExplicitEditUser {
					t.Fatalf("indexed User target=%s role=%v, want actual authored source %s/User", target.LogicalPath, target.Role, fragmentPath)
				}
				input, err := config.NewConfigReadInput(target.PhysicalPath, target.ExpectedBytes())
				if err != nil {
					t.Fatalf("bind actual consumed source bytes for baseline validation: %v", err)
				}
				if _, err := config.ValidateExplicitEdit(workspace, target.Target, input); err != nil {
					t.Fatalf("invalid shipped-zai baseline graph: %v", err)
				}
				if !target.MatchesMutation(editMutationIntent(mutation)) {
					t.Fatal("target no longer matches the original structured intent")
				}
				authored := target.AuthoredPath()
				if got := authored[len(authored)-1].Index; got != 0 {
					t.Fatalf("bound authored index=%d, want fragment-local 0; original=%#v", got, mutation.Path)
				}
				authored[len(authored)-1].Index = 2
				if again := target.AuthoredPath(); again[len(again)-1].Index != 0 || mutation.Path[len(mutation.Path)-1].Index != 1 {
					t.Fatalf("mapped path aliased target or original intent: target=%#v intent=%#v", again, mutation.Path)
				}
				wrong := mutation
				wrong.Path = append(regressionPath("providers", "zai", "variants", "fast", "capabilities"), configdoc.Step{Index: 0, IsIndex: true})
				if _, err := ApplyTargeted(workspace, target, wrong, ApplyOptions{}); err == nil || !strings.Contains(err.Error(), "does not match") {
					t.Fatalf("changed caller intent was not refused: %v", err)
				}
				if after, _ := os.ReadFile(fragmentPath); string(after) != string(fragmentBefore) {
					t.Fatalf("mismatched original intent changed the authored source: %q", after)
				}
				staleSuffix := "// stale preimage\n"
				if tc.format == configdoc.TOML {
					staleSuffix = "# stale preimage\n"
				}
				staleBody := append(append([]byte(nil), fragmentBefore...), []byte(staleSuffix)...)
				if err := os.WriteFile(fragmentPath, staleBody, fragmentInfo.Mode().Perm()); err != nil {
					t.Fatal(err)
				}
				if _, err := ApplyTargeted(workspace, target, mutation, ApplyOptions{}); err == nil || !strings.Contains(err.Error(), "changed since it was planned") {
					t.Fatalf("stale mapped target was not refused: %v", err)
				}
				if after, _ := os.ReadFile(fragmentPath); string(after) != string(staleBody) {
					t.Fatalf("stale refusal changed authored bytes: %q", after)
				}
				if err := os.WriteFile(fragmentPath, fragmentBefore, fragmentInfo.Mode().Perm()); err != nil {
					t.Fatal(err)
				}
				if _, err := ApplyTargeted(workspace, target, mutation, ApplyOptions{}); err != nil {
					t.Fatalf("apply mapped User index: %v", err)
				}
				mainAfter, _ := os.ReadFile(mainPath)
				fragmentAfter, _ := os.ReadFile(fragmentPath)
				neighborAfter, _ := os.ReadFile(neighborPath)
				fragmentInfoAfter, _ := os.Stat(fragmentPath)
				if string(mainAfter) != string(mainBefore) || string(neighborAfter) != string(neighborBefore) || fragmentInfoAfter.Mode().Perm() != fragmentInfo.Mode().Perm() {
					t.Fatalf("mapped edit changed main/neighbor/mode: main=%q neighbor=%q mode=%v", mainAfter, neighborAfter, fragmentInfoAfter.Mode().Perm())
				}
				if action == Remove {
					if strings.Contains(string(fragmentAfter), "messages") || !strings.Contains(string(fragmentAfter), "responses") {
						t.Fatalf("effective index 1 must remove fragment-local index 0 (messages), not responses: %q", fragmentAfter)
					}
				} else if !strings.Contains(string(fragmentAfter), "web_search") || !strings.Contains(string(fragmentAfter), "responses") || strings.Contains(string(fragmentAfter), "messages") {
					t.Fatalf("Set did not map effective index 1 to authored index 0: %q", fragmentAfter)
				}
				if !strings.Contains(string(fragmentAfter), "fragment note") || (tc.format == configdoc.TOML && !strings.Contains(string(fragmentAfter), "model note")) {
					t.Fatalf("mapped splice dropped authored comments: %q", fragmentAfter)
				}
				capabilitiesAfter := userVariantCapabilities(t, home)
				if len(capabilitiesAfter) != 2 || capabilitiesAfter[0] != "web_search" || capabilitiesAfter[1] != "responses" {
					t.Fatalf("mapped edit changed effective neighbors: capabilities=%#v", capabilitiesAfter)
				}
				assertNoTargetArtifacts(t, fragmentPath)
			})
		}
	}
}

func TestApplyTargetedRemovesSingletonAuthoredUserListElement(t *testing.T) {
	home, workspace := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("YOLO_VERSION", "")
	userDir := filepath.Join(home, ".config", "yolo-jail")
	mainPath := filepath.Join(userDir, paths.UserConfigTOMLName)
	fragmentPath := filepath.Join(userDir, "config.d", "10-provider.toml")
	writeEditFixture(t, mainPath, "packs=['zai']\n[providers.zai.variants.fast]\nmodel='glm-5.3'\ncapabilities=['web_search']\n")
	fragment := "# keep fragment note\n[providers.zai.variants.fast]\nmodel='glm-5.3' # keep model note\ncapabilities=['messages']\n"
	writeEditFixture(t, fragmentPath, fragment)
	neighborPath := filepath.Join(userDir, "config.d", "20-neighbor.toml")
	writeEditFixture(t, neighborPath, "[providers.zai.variants.fast]\nmodel='glm-5.3'\n")
	neighborBefore, _ := os.ReadFile(neighborPath)
	mainBefore, _ := os.ReadFile(mainPath)
	modeBefore, _ := os.Stat(fragmentPath)
	path := append(regressionPath("providers", "zai", "variants", "fast", "capabilities"), configdoc.Step{Index: 1, IsIndex: true})
	mutation := Mutation{Action: Remove, Path: path}
	target, err := ResolveTarget(workspace, mutation, config.EditTargetChoice{Kind: config.EditTargetAutomatic})
	if err != nil {
		t.Fatalf("resolve singleton mapped User index: %v", err)
	}
	if target.LogicalPath != fragmentPath || target.AuthoredPath()[len(path)-1].Index != 0 {
		t.Fatalf("singleton target=%s authored=%#v", target.LogicalPath, target.AuthoredPath())
	}
	if _, err := ApplyTargeted(workspace, target, mutation, ApplyOptions{}); err != nil {
		t.Fatalf("remove singleton authored element: %v", err)
	}
	fragmentAfter, _ := os.ReadFile(fragmentPath)
	mainAfter, _ := os.ReadFile(mainPath)
	neighborAfter, _ := os.ReadFile(neighborPath)
	modeAfter, _ := os.Stat(fragmentPath)
	if strings.Contains(string(fragmentAfter), "messages") || !strings.Contains(string(fragmentAfter), "# keep fragment note") || !strings.Contains(string(fragmentAfter), "# keep model note") || string(mainAfter) != string(mainBefore) || string(neighborAfter) != string(neighborBefore) || modeAfter.Mode().Perm() != modeBefore.Mode().Perm() {
		t.Fatalf("singleton removal changed wrong source or neighbor: fragment=%q main=%q neighbor=%q", fragmentAfter, mainAfter, neighborAfter)
	}
	// TOML [] is an explicit reset of inherited capabilities, unlike legacy JSONC [] union.
	if got := userVariantCapabilities(t, home); len(got) != 0 {
		t.Fatalf("singleton removal must retain the authored TOML empty-list reset: effective capabilities=%#v", got)
	}
	assertNoTargetArtifacts(t, fragmentPath)
}

func TestResolveTargetRefusesOutOfRangeUserIndexWithoutWriting(t *testing.T) {
	home, workspace := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("YOLO_VERSION", "")
	userDir := filepath.Join(home, ".config", "yolo-jail")
	mainPath := filepath.Join(userDir, paths.UserConfigTOMLName)
	fragmentPath := filepath.Join(userDir, "config.d", "10-provider.toml")
	writeEditFixture(t, mainPath, "packs=['zai']\n[providers.zai.variants.fast]\nmodel='glm-5.3'\ncapabilities=['web_search']\n")
	fragment := "[providers.zai.variants.fast]\nmodel='glm-5.3'\ncapabilities=['messages','responses']\n"
	writeEditFixture(t, fragmentPath, fragment)
	path := append(regressionPath("providers", "zai", "variants", "fast", "capabilities"), configdoc.Step{Index: 3, IsIndex: true})
	if _, err := ResolveTarget(workspace, Mutation{Action: Remove, Path: path}, config.EditTargetChoice{Kind: config.EditTargetAutomatic}); err == nil {
		t.Fatal("out-of-range effective User index was accepted without an authored mapping")
	}
	after, _ := os.ReadFile(fragmentPath)
	if string(after) != fragment {
		t.Fatalf("out-of-range target changed authored source: %q", after)
	}
	assertNoTargetArtifacts(t, fragmentPath)
}

func TestResolveTargetRefusesAmbiguousUserIndexedOriginsWithoutWriting(t *testing.T) {
	home, workspace := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("YOLO_VERSION", "")
	userDir := filepath.Join(home, ".config", "yolo-jail")
	mainPath := filepath.Join(userDir, paths.UserConfigTOMLName)
	fragmentOne := filepath.Join(userDir, "config.d", "10-provider.toml")
	fragmentTwo := filepath.Join(userDir, "config.d", "20-provider.toml")
	writeEditFixture(t, mainPath, "packs=['zai']\n[providers.zai.variants.fast]\nmodel='glm-5.3'\ncapabilities=['web_search']\n")
	body := "[providers.zai.variants.fast]\nmodel='glm-5.3'\ncapabilities=['messages']\n"
	writeEditFixture(t, fragmentOne, body)
	writeEditFixture(t, fragmentTwo, body)
	before := map[string][]byte{}
	for _, path := range []string{mainPath, fragmentOne, fragmentTwo} {
		before[path], _ = os.ReadFile(path)
	}
	path := append(regressionPath("providers", "zai", "variants", "fast", "capabilities"), configdoc.Step{Index: 1, IsIndex: true})
	if _, err := ResolveTarget(workspace, Mutation{Action: Remove, Path: path}, config.EditTargetChoice{Kind: config.EditTargetAutomatic}); err == nil || !strings.Contains(err.Error(), "ambiguous authored sources") {
		t.Fatalf("ambiguous effective list origin was not refused: %v", err)
	}
	for _, path := range []string{mainPath, fragmentOne, fragmentTwo} {
		if after, err := os.ReadFile(path); err != nil || string(after) != string(before[path]) {
			t.Fatalf("ambiguous mapping changed %s: bytes=%q err=%v", path, after, err)
		}
		assertNoTargetArtifacts(t, path)
	}
}

func regressionPath(keys ...string) []configdoc.Step {
	path := make([]configdoc.Step, len(keys))
	for i, key := range keys {
		path[i] = configdoc.Step{Key: key}
	}
	return path
}

func TestApplyTargetedResolvesAndAppliesIndexedEditsInBothFormats(t *testing.T) {
	for _, tc := range []struct {
		name, filename, before string
		format                 configdoc.Format
	}{
		{"JSONC", paths.WorkspaceConfigLegacyJSONCName, `{"machine":{"ports":[{"host":8080,"jail":80},{"host":8081,"jail":81}]}}`, configdoc.JSONC},
		{"TOML", paths.WorkspaceConfigTOMLName, "[[machine.ports]]\nhost=8080\njail=80\n[[machine.ports]]\nhost=8081\njail=81\n", configdoc.TOML},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, action := range []Action{Set, Remove} {
				t.Run(map[Action]string{Set: "indexed-set", Remove: "indexed-remove"}[action], func(t *testing.T) {
					home, workspace := t.TempDir(), t.TempDir()
					t.Setenv("HOME", home)
					t.Setenv("YOLO_VERSION", "")
					path := filepath.Join(workspace, tc.filename)
					writeEditFixture(t, path, tc.before)
					neighbor := filepath.Join(workspace, "neighbor.txt")
					writeEditFixture(t, neighbor, "untouched\n")
					beforeNeighbor, _ := os.ReadFile(neighbor)
					modeBefore, _ := os.Stat(path)
					item := append(regressionPath("machine", "ports"), configdoc.Step{Index: 0, IsIndex: true})
					mutation := Mutation{Action: Remove, Path: item}
					if action == Set {
						mutation = Mutation{Action: Set, Path: append(append([]configdoc.Step(nil), item...), configdoc.Step{Key: "host"}), Value: 8180}
					}
					target, err := ResolveTarget(workspace, mutation, config.EditTargetChoice{Kind: config.EditTargetAutomatic})
					if err != nil {
						t.Fatalf("resolve supported indexed mutation: %v", err)
					}
					if target.LogicalPath != path {
						t.Fatalf("indexed target=%s, want %s", target.LogicalPath, path)
					}
					if _, err := ApplyTargeted(workspace, target, mutation, ApplyOptions{}); err != nil {
						t.Fatalf("apply supported indexed mutation: %v", err)
					}
					after, err := os.ReadFile(path)
					if err != nil || string(after) == tc.before {
						t.Fatalf("indexed edit did not change target: %q err=%v", after, err)
					}
					if action == Set && !strings.Contains(string(after), "8180") {
						t.Fatalf("indexed Set missed selected item: %q", after)
					}
					if action == Remove && strings.Contains(string(after), "8080") {
						t.Fatalf("indexed Remove kept selected item: %q", after)
					}
					afterNeighbor, _ := os.ReadFile(neighbor)
					modeAfter, _ := os.Stat(path)
					if string(afterNeighbor) != string(beforeNeighbor) || modeAfter.Mode().Perm() != modeBefore.Mode().Perm() {
						t.Fatalf("indexed edit touched neighbor or mode: neighbor=%q mode=%v", afterNeighbor, modeAfter.Mode().Perm())
					}
					assertNoTargetArtifacts(t, path)
				})
			}
		})
	}
}

func TestApplyTargetedResolvesOpaqueProviderDescendantsWithDeclaredOptions(t *testing.T) {
	for _, tc := range []struct {
		name, configName, fragmentName string
		format                         configdoc.Format
	}{
		{"JSONC", paths.UserConfigLegacyJSONCName, "", configdoc.JSONC},
		{"TOML", paths.UserConfigTOMLName, "10-provider.toml", configdoc.TOML},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home, workspace := t.TempDir(), t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("YOLO_VERSION", "")
			userDir := filepath.Join(home, ".config", "yolo-jail")
			userPath := filepath.Join(userDir, tc.configName)
			if tc.format == configdoc.TOML {
				writeEditFixture(t, userPath, "packs = ['zai']\n")
			} else {
				writeEditFixture(t, userPath, `{"packs":["zai"],"providers":{"zai":{"capabilities":["web_search","messages"],"variants":{"fast":{"model":"glm-5.3","options":{"model":"glm-5.3"}}}}}}`)
			}
			fragment := userPath
			if tc.fragmentName != "" {
				fragment = filepath.Join(userDir, "config.d", tc.fragmentName)
				writeEditFixture(t, fragment, "[providers.zai.variants.fast]\nmodel='glm-5.3'\noptions={ model='glm-5.3' }\ncapabilities=['web_search','messages']\n")
			}
			neighbor := filepath.Join(userDir, "config.d", "neighbor.txt")
			writeEditFixture(t, neighbor, "keep\n")
			beforeNeighbor, _ := os.ReadFile(neighbor)
			modeBefore, _ := os.Stat(fragment)
			mutation := Mutation{Action: Set, Path: regressionPath("providers", "zai", "variants", "fast", "options", "model"), Value: "glm-5.3-flash"}
			target, err := ResolveTarget(workspace, mutation, config.EditTargetChoice{Kind: config.EditTargetAutomatic})
			if err != nil {
				t.Fatalf("resolve declared opaque descendant: %v", err)
			}
			if target.LogicalPath != fragment || target.Role != config.ExplicitEditUser {
				t.Fatalf("opaque User target=%s role=%v, want %s/User", target.LogicalPath, target.Role, fragment)
			}
			result, err := ApplyTargeted(workspace, target, mutation, ApplyOptions{})
			if err != nil || result.Plan == nil {
				t.Fatalf("apply declared opaque descendant: result=%#v err=%v", result, err)
			}
			if updated, readErr := os.ReadFile(fragment); readErr != nil || !strings.Contains(string(updated), "glm-5.3-flash") {
				t.Fatalf("opaque Set did not update its resolved source: %q err=%v", updated, readErr)
			}
			neighborAfter, _ := os.ReadFile(neighbor)
			modeAfter, _ := os.Stat(fragment)
			if string(neighborAfter) != string(beforeNeighbor) || modeAfter.Mode().Perm() != modeBefore.Mode().Perm() {
				t.Fatal("opaque edit changed neighbor or mode")
			}
			if tc.format == configdoc.TOML {
				indexed := Mutation{Action: Remove, Path: append(regressionPath("providers", "zai", "variants", "fast", "capabilities"), configdoc.Step{Index: 0, IsIndex: true})}
				target, err = ResolveTarget(workspace, indexed, config.EditTargetChoice{Kind: config.EditTargetAutomatic})
				if err != nil {
					t.Fatalf("resolve indexed User descendant: %v", err)
				}
				if target.LogicalPath != fragment || target.Role != config.ExplicitEditUser {
					t.Fatalf("indexed User target=%s role=%v, want config.d fragment %s/User", target.LogicalPath, target.Role, fragment)
				}
				if _, err := ApplyTargeted(workspace, target, indexed, ApplyOptions{}); err != nil {
					t.Fatalf("apply indexed User descendant: %v", err)
				}
			}
			if _, err := ResolveTarget(workspace, mutation, config.EditTargetChoice{Kind: config.EditTargetProject}); err == nil || !strings.Contains(err.Error(), "user-scope only") {
				t.Fatalf("User-only opaque descendant accepted Project role: %v", err)
			}
		})
	}
}

func TestApplyTargetedKeepsProviderOpaqueDescendantsSemanticallyTyped(t *testing.T) {
	for _, tc := range []struct {
		name, configName, fragmentName string
		format                         configdoc.Format
	}{
		{"JSONC", paths.UserConfigLegacyJSONCName, "", configdoc.JSONC},
		{"TOML", paths.UserConfigTOMLName, "10-provider.toml", configdoc.TOML},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home, workspace := t.TempDir(), t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("YOLO_VERSION", "")
			userDir := filepath.Join(home, ".config", "yolo-jail")
			userPath := filepath.Join(userDir, tc.configName)
			if tc.format == configdoc.TOML {
				writeEditFixture(t, userPath, "packs = ['zai']\n")
			} else {
				writeEditFixture(t, userPath, `{"packs":["zai"],"providers":{"zai":{"variants":{"fast":{"model":"glm-5.3","options":{"model":"glm-5.3"}}}}}}`)
			}
			fragment := userPath
			if tc.fragmentName != "" {
				fragment = filepath.Join(userDir, "config.d", tc.fragmentName)
				writeEditFixture(t, fragment, "[providers.zai.variants.fast]\nmodel='glm-5.3'\noptions={ model='glm-5.3' }\ncapabilities=['web_search','messages']\n")
			}
			neighbor := filepath.Join(workspace, "neighbor.txt")
			writeEditFixture(t, neighbor, "unchanged\n")
			before, _ := os.ReadFile(fragment)
			modeBefore, _ := os.Stat(fragment)
			path := append(regressionPath("providers", "zai", "variants", "fast", "options"), configdoc.Step{Key: "invented"})
			mutation := Mutation{Action: Add, Path: path, Value: "must-not-apply"}
			target, err := ResolveTarget(workspace, mutation, config.EditTargetChoice{Kind: config.EditTargetAutomatic})
			if err != nil {
				t.Fatalf("resolve opaque semantic-negative target: %v", err)
			}
			_, err = ApplyTargeted(workspace, target, mutation, ApplyOptions{})
			if err == nil || !strings.Contains(err.Error(), "invented") {
				t.Fatalf("undeclared provider option was not semantically refused: %v", err)
			}
			after, _ := os.ReadFile(fragment)
			neighborAfter, _ := os.ReadFile(neighbor)
			modeAfter, _ := os.Stat(fragment)
			if string(after) != string(before) || string(neighborAfter) != "unchanged\n" || modeAfter.Mode().Perm() != modeBefore.Mode().Perm() {
				t.Fatalf("semantic refusal changed files/mode: target=%q neighbor=%q", after, neighborAfter)
			}
			assertNoTargetArtifacts(t, fragment)
		})
	}
}

func TestApplyTargetedPreservesEmptyTOMLPreimageForStaleChecks(t *testing.T) {
	for _, mode := range []string{"edit", "dry-run", "noop"} {
		t.Run(mode, func(t *testing.T) {
			home, workspace := t.TempDir(), t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("YOLO_VERSION", "")
			path := filepath.Join(workspace, paths.WorkspaceConfigTOMLName)
			writeEditFixture(t, path, "")
			mutation := Mutation{Action: Add, Path: regressionPath("machine", "resources", "cpus"), Value: 4}
			target, err := ResolveTarget(workspace, mutation, config.EditTargetChoice{Kind: config.EditTargetAutomatic})
			if err != nil {
				t.Fatal(err)
			}
			expected := target.ExpectedBytes()
			if expected == nil || len(expected) != 0 {
				t.Fatalf("resolved empty preimage must be detached non-nil empty: %#v", expected)
			}
			expected = append(expected, 'x')
			if len(expected) != 1 || expected[0] != 'x' {
				t.Fatal("could not mutate detached expected bytes")
			}
			if again := target.ExpectedBytes(); again == nil || len(again) != 0 {
				t.Fatalf("expected-byte accessor leaked mutable storage: %#v", again)
			}
			if mode == "noop" {
				writeEditFixture(t, path, "[machine.resources]\ncpus=4\n")
			} else {
				writeEditFixture(t, path, "[machine.resources]\ncpus=2\n")
			}
			modeBefore, _ := os.Stat(path)
			neighbor := filepath.Join(workspace, "neighbor.txt")
			writeEditFixture(t, neighbor, "stay\n")
			before, _ := os.ReadFile(path)
			neighborBefore, _ := os.ReadFile(neighbor)
			_, err = ApplyTargeted(workspace, target, mutation, ApplyOptions{DryRun: mode == "dry-run"})
			if err == nil || !strings.Contains(err.Error(), "changed since it was planned") {
				t.Fatalf("stale empty expectation was not refused in %s mode: %v", mode, err)
			}
			after, _ := os.ReadFile(path)
			neighborAfter, _ := os.ReadFile(neighbor)
			modeAfter, _ := os.Stat(path)
			if string(after) != string(before) || string(neighborAfter) != string(neighborBefore) || modeAfter.Mode().Perm() != modeBefore.Mode().Perm() {
				t.Fatalf("stale empty preimage changed target/neighbor/mode")
			}
			assertNoTargetArtifacts(t, path)
		})
	}
}

func TestApplyTargetedAllowsAnUnchangedEmptyTOMLPreimage(t *testing.T) {
	for _, dryRun := range []bool{false, true} {
		t.Run(map[bool]string{false: "edit", true: "dry-run"}[dryRun], func(t *testing.T) {
			home, workspace := t.TempDir(), t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("YOLO_VERSION", "")
			path := filepath.Join(workspace, paths.WorkspaceConfigTOMLName)
			writeEditFixture(t, path, "")
			mutation := Mutation{Action: Add, Path: regressionPath("machine", "resources", "cpus"), Value: 4}
			target, err := ResolveTarget(workspace, mutation, config.EditTargetChoice{Kind: config.EditTargetAutomatic})
			if err != nil {
				t.Fatal(err)
			}
			result, err := ApplyTargeted(workspace, target, mutation, ApplyOptions{DryRun: dryRun})
			if err != nil || result.Plan == nil {
				t.Fatalf("unchanged empty target result=%#v err=%v", result, err)
			}
			got, _ := os.ReadFile(path)
			if dryRun && len(got) != 0 {
				t.Fatalf("dry-run wrote empty target: %q", got)
			}
			if !dryRun && len(got) == 0 {
				t.Fatal("edit did not write empty target")
			}
		})
	}
}

func TestApplyTargetedNoopsOnUnchangedExistingAdd(t *testing.T) {
	home, workspace := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("YOLO_VERSION", "")
	path := filepath.Join(workspace, paths.WorkspaceConfigTOMLName)
	before := "[machine.resources]\ncpus=4\n"
	writeEditFixture(t, path, before)
	mutation := Mutation{Action: Add, Path: regressionPath("machine", "resources", "cpus"), Value: 4}
	target, err := ResolveTarget(workspace, mutation, config.EditTargetChoice{Kind: config.EditTargetAutomatic})
	if err != nil {
		t.Fatal(err)
	}
	result, err := ApplyTargeted(workspace, target, mutation, ApplyOptions{})
	got, readErr := os.ReadFile(path)
	if err != nil || result.Plan == nil || !result.Plan.Noop || readErr != nil || string(got) != before {
		t.Fatalf("unchanged Add no-op result=%#v err=%v bytes=%q readErr=%v", result, err, got, readErr)
	}
}
