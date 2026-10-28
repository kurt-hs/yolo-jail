package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/config"
	"github.com/mschulkind-oss/yolo-jail/internal/configdoc"
	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
	"github.com/mschulkind-oss/yolo-jail/internal/testconfig"
)

func TestConfigRegistryUnsetScalarRemovesNamedLocalValueInBothFormats(t *testing.T) {
	for _, tc := range []struct {
		name, key, userText, projectText, localText string
		format                                      configdoc.Format
		localName                                   string
		wantLower                                   any
		wantHunk                                    string
	}{
		{
			name: "TOML integer", key: "machine.resources.cpus", format: configdoc.TOML,
			localName: paths.WorkspaceLocalConfigTOMLName,
			userText:  "[machine.resources]\ncpus = 1\n", projectText: "[machine.resources]\ncpus = 2\n", localText: "[machine.resources]\ncpus = 3\n",
			wantLower: int64(2), wantHunk: "-cpus = 3",
		},
		{
			name: "JSONC canonical integer", key: "machine.resources.cpus", format: configdoc.JSONC,
			localName: paths.WorkspaceLocalConfigLegacyJSONCName,
			userText:  `{"machine":{"resources":{"cpus":1}}}`, projectText: `{"machine":{"resources":{"cpus":2}}}`, localText: `{"machine":{"resources":{"cpus":3}}}`,
			wantLower: int64(2), wantHunk: `-"cpus":3`,
		},
		{
			name: "JSONC legacy string", key: "machine.resources.cpus", format: configdoc.JSONC,
			localName: paths.WorkspaceLocalConfigLegacyJSONCName,
			userText:  `{"resources":{"cpus":"1"}}`, projectText: `{"resources":{"cpus":"2"}}`, localText: `{"resources":{"cpus":3}}`,
			wantLower: "2", wantHunk: `-"cpus":3`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			workspace := testconfig.WorkspaceFormat(t, tc.format, tc.projectText)
			user := testconfig.UserFormat(t, tc.format, tc.userText)
			t.Setenv("HOME", user.Root)
			t.Setenv("YOLO_VERSION", "")
			localPath := filepath.Join(workspace.Root, tc.localName)
			if err := os.WriteFile(localPath, []byte(tc.localText), 0o600); err != nil {
				t.Fatal(err)
			}
			withConfigEditCWD(t, workspace.Root)

			beforeUser, _ := os.ReadFile(user.Path)
			beforeProject, _ := os.ReadFile(workspace.Path)
			beforeStat, err := os.Stat(localPath)
			if err != nil {
				t.Fatal(err)
			}
			rc, out, errw := dispatchConfigCapture(t, []string{"unset", tc.key, "--local"})
			if !strings.Contains(out, localPath) || !strings.Contains(out, tc.wantHunk) {
				t.Fatalf("registered unset rc=%d stdout=%q stderr=%q", rc, out, errw)
			}
			assertPublicPostWriteCheck(t, rc, out, errw)
			gotLocal, err := os.ReadFile(localPath)
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Contains(gotLocal, []byte("cpus")) {
				t.Fatalf("named Local scalar remains after unset: %s", gotLocal)
			}
			if after, _ := os.ReadFile(user.Path); !bytes.Equal(after, beforeUser) {
				t.Fatalf("unset changed User bytes: before=%q after=%q", beforeUser, after)
			}
			if after, _ := os.ReadFile(workspace.Path); !bytes.Equal(after, beforeProject) {
				t.Fatalf("Local unset changed Project bytes: before=%q after=%q", beforeProject, after)
			}
			afterStat, err := os.Stat(localPath)
			if err != nil || afterStat.Mode().Perm() != beforeStat.Mode().Perm() {
				t.Fatalf("unset changed Local mode: before=%v after=%v err=%v", beforeStat.Mode(), afterStat.Mode(), err)
			}
			effective, err := config.LoadReadableConfig(workspace.Root, true, nil)
			if err != nil {
				t.Fatal(err)
			}
			got := readableValue(t, effective, "machine", "resources", "cpus")
			if want, ok := tc.wantLower.(string); ok {
				if got != want {
					t.Fatalf("effective lower-layer cpus = %#v (%T), want string %q", got, got, want)
				}
			} else if gotInt, ok := jsonx.AsInt(got); !ok || gotInt != 2 {
				t.Fatalf("effective lower-layer cpus = %#v (%T), want integer 2", got, got)
			}
		})
	}
}

func TestConfigRegistryUnsetScalarExposesLowerLayerAndRemovesProjectBytesUnderLocalWinner(t *testing.T) {
	for _, tc := range []struct {
		name, project, user, local, localName, key      string
		format                                          configdoc.Format
		oldPath                                         []configdoc.Step
		wantProjectComment, wantValueComment, wantOther string
		wantRemoved                                     string
	}{
		{
			name: "TOML", format: configdoc.TOML,
			project: "# project-only comment survives\n[machine.resources]\ncpus = 2 # value comment stays unattached\n\n[env]\nset = { PAGER = 'project' }\n",
			user:    "[machine.resources]\ncpus = 1\n", local: "[machine.resources]\ncpus = 3\n",
			localName: paths.WorkspaceLocalConfigTOMLName, key: "machine.resources.cpus",
			oldPath:            []configdoc.Step{{Key: "machine"}, {Key: "resources"}, {Key: "cpus"}},
			wantProjectComment: "# project-only comment survives", wantValueComment: "# value comment stays unattached",
			wantOther: "set = { PAGER = 'project' }", wantRemoved: "cpus = 2",
		},
		{
			name: "legacy JSONC", format: configdoc.JSONC,
			project: "{\n  // project-only comment survives\n  \"resources\": {\n    \"cpus\": 2 // value comment stays unattached\n  },\n  \"env\": {\"set\": {\"PAGER\": \"project\"}}\n}\n",
			user:    `{"resources":{"cpus":1}}`, local: `{"resources":{"cpus":3}}`,
			localName: paths.WorkspaceLocalConfigLegacyJSONCName, key: "machine.resources.cpus",
			oldPath:            []configdoc.Step{{Key: "resources"}, {Key: "cpus"}},
			wantProjectComment: "// project-only comment survives", wantValueComment: "// value comment stays unattached",
			wantOther: `"PAGER": "project"`, wantRemoved: `"cpus": 2`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			workspace := testconfig.WorkspaceFormat(t, tc.format, tc.project)
			user := testconfig.UserFormat(t, tc.format, tc.user)
			t.Setenv("HOME", user.Root)
			t.Setenv("YOLO_VERSION", "")
			localPath := filepath.Join(workspace.Root, tc.localName)
			if err := os.WriteFile(localPath, []byte(tc.local), 0o600); err != nil {
				t.Fatal(err)
			}
			withConfigEditCWD(t, workspace.Root)
			beforeProject, _ := os.ReadFile(workspace.Path)
			beforeUser, _ := os.ReadFile(user.Path)
			beforeLocal, _ := os.ReadFile(localPath)

			rc, out, errw := dispatchConfigCapture(t, []string{"unset", tc.key})
			if !strings.Contains(out, workspace.Path) {
				t.Fatalf("registered scalar unset rc=%d stdout=%q stderr=%q", rc, out, errw)
			}
			assertPublicPostWriteCheck(t, rc, out, errw)
			afterProject := mustReadScalarUnset(t, workspace.Path)
			if bytes.Contains(afterProject, []byte(tc.wantRemoved)) || !bytes.Contains(afterProject, []byte(tc.wantProjectComment)) ||
				!bytes.Contains(afterProject, []byte(tc.wantValueComment)) || !bytes.Contains(afterProject, []byte(tc.wantOther)) {
				t.Fatalf("unset did not remove only the Project scalar bytes: %s", afterProject)
			}
			if bytes.Equal(beforeProject, afterProject) || !bytes.Equal(mustReadScalarUnset(t, user.Path), beforeUser) ||
				!bytes.Equal(mustReadScalarUnset(t, localPath), beforeLocal) {
				t.Fatalf("unset changed wrong source: project=%s user=%s local=%s", afterProject, mustReadScalarUnset(t, user.Path), mustReadScalarUnset(t, localPath))
			}
			doc, err := configdoc.Parse(afterProject, tc.format)
			if err != nil {
				t.Fatal(err)
			}
			if _, found, err := doc.Index.Value(tc.oldPath...); err != nil || found {
				t.Fatalf("independent document parser found removed Project scalar: found=%v err=%v", found, err)
			}
			effective, err := config.LoadReadableConfig(workspace.Root, true, nil)
			if err != nil {
				t.Fatal(err)
			}
			got := readableValue(t, effective, "machine", "resources", "cpus")
			if n, ok := jsonx.AsInt(got); !ok || n != 3 {
				t.Fatalf("Local winner after Project unset = %#v (%T), want integer 3", got, got)
			}
		})
	}
}

func TestConfigRegistryUnsetScalarDryRunInvalidInputAbsentAndNonScalarPreserveFiles(t *testing.T) {
	workspace := testconfig.Workspace(t, "[machine.resources]\ncpus = 2\nmemory = '8g'\n\n[machine.gpu]\nenabled = true\n")
	user := testconfig.User(t, "[machine.resources]\ncpus = 1\nmemory = '16g'\n")
	t.Setenv("HOME", user.Root)
	t.Setenv("YOLO_VERSION", "")
	localPath := filepath.Join(workspace.Root, paths.WorkspaceLocalConfigTOMLName)
	if err := os.WriteFile(localPath, []byte("[machine.resources]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	withConfigEditCWD(t, workspace.Root)
	before, err := os.ReadFile(workspace.Path)
	if err != nil {
		t.Fatal(err)
	}
	{
		args := []string{"unset", "machine.resources.cpus", "--project", "--dry-run"}
		var out, errw bytes.Buffer
		rc, output, errOutput := dispatchConfigCapture(t, args)
		out.WriteString(output)
		errw.WriteString(errOutput)
		if rc != 0 || strings.Contains(errw.String(), "config edit refused") || strings.Contains(out.String(), "Summary") ||
			!strings.Contains(out.String(), "Dry run; nothing was written.") ||
			!strings.Contains(out.String(), "cpus = 2") {
			t.Fatalf("scalar dry-run rc=%d stdout=%q stderr=%q", rc, out.String(), errw.String())
		}
		if got := mustReadScalarUnset(t, workspace.Path); !bytes.Equal(got, before) {
			t.Fatalf("dry-run changed bytes: before=%q after=%q", before, got)
		}
	}

	for _, args := range [][]string{
		{"unset", "unknown.key", "--project"},
		{"unset", "machine.gpu", "--project"},
	} {
		var out, errw bytes.Buffer
		rc, output, errOutput := dispatchConfigCapture(t, args)
		out.WriteString(output)
		errw.WriteString(errOutput)
		if rc == 0 || out.Len() != 0 || errw.Len() == 0 {
			t.Errorf("invalid/non-scalar unset %q rc=%d stdout=%q stderr=%q", args, rc, out.String(), errw.String())
		}
		if got := mustReadScalarUnset(t, workspace.Path); !bytes.Equal(got, before) {
			t.Fatalf("invalid/non-scalar unset %q changed bytes: %q", args, got)
		}
	}

	// An independently absent valid scalar key remains a successful no-op, even with a lower value.
	var out, errw bytes.Buffer
	rc, output, errOutput := dispatchConfigCapture(t, []string{"unset", "machine.resources.memory", "--local"})
	out.WriteString(output)
	errw.WriteString(errOutput)
	if rc != 0 || strings.Contains(errw.String(), "config edit refused") || strings.Contains(out.String(), "Summary") ||
		!strings.Contains(out.String(), "No change; no write.") {
		t.Fatalf("absent scalar unset rc=%d stdout=%q stderr=%q", rc, out.String(), errw.String())
	}
	if got := mustReadScalarUnset(t, workspace.Path); !bytes.Equal(got, before) {
		t.Fatalf("absent local noop changed project bytes: %q", got)
	}
}

func TestConfigRegistryUnsetScalarDoesNotAcceptLegacyPathAsPublicAlias(t *testing.T) {
	workspace := testconfig.WorkspaceFormat(t, configdoc.JSONC, `{"resources":{"cpus":3}}`)
	user := testconfig.UserFormat(t, configdoc.JSONC, "{}")
	t.Setenv("HOME", user.Root)
	t.Setenv("YOLO_VERSION", "")
	withConfigEditCWD(t, workspace.Root)
	before := mustReadScalarUnset(t, workspace.Path)
	rc, out, errw := dispatchConfigCapture(t, []string{"unset", "resources.cpus", "--project"})
	if rc == 0 || out != "" || !strings.Contains(errw, "has no readable schema target") {
		t.Fatalf("legacy path alias rc=%d stdout=%q stderr=%q", rc, out, errw)
	}
	if after := mustReadScalarUnset(t, workspace.Path); !bytes.Equal(after, before) {
		t.Fatalf("unsupported legacy CLI alias changed JSONC bytes: before=%q after=%q", before, after)
	}
}

func TestConfigRegistryUnsetScalarHelpDescribesPhysicalRemoval(t *testing.T) {
	rc, out, errw := dispatchConfigCapture(t, []string{"unset", "--help"})
	if rc != 0 || !strings.Contains(out, "physically removes an ordinary recognized scalar") ||
		!strings.Contains(out, "lower layer or default may reappear") || !strings.Contains(out, "Table, list, native, and null removals") ||
		!strings.Contains(out, "unsupported removals refuse without writing") {
		t.Fatalf("public unset help rc=%d stdout=%q stderr=%q", rc, out, errw)
	}
}

func TestConfigRegistryUnsetScalarPrevalidatesInvalidRawResourceUnderLocalMask(t *testing.T) {
	for _, tc := range []struct {
		name, path, projectText string
		format                  configdoc.Format
	}{
		{name: "TOML", path: "machine.resources.cpus", format: configdoc.TOML, projectText: "[machine.resources]\ncpus = 'not-a-number'\n"},
		{name: "JSONC", path: "machine.resources.cpus", format: configdoc.JSONC, projectText: `{"resources":{"cpus":"not-a-number"}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			workspace := testconfig.WorkspaceFormat(t, tc.format, tc.projectText)
			userText := ""
			localText := "[machine.resources]\ncpus = 3\n"
			if tc.format == configdoc.JSONC {
				userText = "{}"
				localText = `{"resources":{"cpus":3}}`
			}
			user := testconfig.UserFormat(t, tc.format, userText)
			t.Setenv("HOME", user.Root)
			t.Setenv("YOLO_VERSION", "")
			localPath := filepath.Join(workspace.Root, paths.WorkspaceLocalConfigTOMLName)
			if tc.format == configdoc.JSONC {
				localPath = filepath.Join(workspace.Root, paths.WorkspaceLocalConfigLegacyJSONCName)
			}
			if err := os.WriteFile(localPath, []byte(localText), 0o600); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(workspace.Path)
			if err != nil {
				t.Fatal(err)
			}
			beforeLocal, err := os.ReadFile(localPath)
			if err != nil {
				t.Fatal(err)
			}
			withConfigEditCWD(t, workspace.Root)
			for _, args := range [][]string{
				{"unset", tc.path, "--project", "--dry-run"},
				{"unset", tc.path, "--project"},
				{"unset", "machine.resources.memory", "--local"}, // absent target still validates the original graph
			} {
				rc, out, errw := dispatchConfigCapture(t, args)
				if rc == 0 || out != "" || !strings.Contains(errw, "validate raw workspace resource config") || strings.Contains(out, "Summary") {
					t.Fatalf("unset masked invalid authored scalar %q rc=%d stdout=%q stderr=%q", args, rc, out, errw)
				}
				if after, _ := os.ReadFile(workspace.Path); !bytes.Equal(after, before) {
					t.Fatalf("invalid masked source was removed: before=%q after=%q", before, after)
				}
				if after, _ := os.ReadFile(localPath); !bytes.Equal(after, beforeLocal) {
					t.Fatalf("invalid-source refusal changed Local bytes: before=%q after=%q", beforeLocal, after)
				}
			}
		})
	}
}

func TestConfigRegistryUnsetScalarPreservesSelectedSymlink(t *testing.T) {
	workspace := testconfig.Workspace(t, "[machine.resources]\ncpus = 2\n")
	user := testconfig.User(t, "")
	t.Setenv("HOME", user.Root)
	t.Setenv("YOLO_VERSION", "")
	physical := filepath.Join(workspace.Root, "physical-project.toml")
	if err := os.Rename(workspace.Path, physical); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Base(physical), workspace.Path); err != nil {
		t.Fatal(err)
	}
	linkBefore, err := os.Readlink(workspace.Path)
	if err != nil {
		t.Fatal(err)
	}
	linkInfo, err := os.Lstat(workspace.Path)
	if err != nil {
		t.Fatal(err)
	}
	withConfigEditCWD(t, workspace.Root)
	rc, out, errw := dispatchConfigCapture(t, []string{"unset", "machine.resources.cpus", "--project"})
	if !strings.Contains(out, workspace.Path) || strings.Contains(string(mustReadScalarUnset(t, physical)), "cpus") {
		t.Fatalf("symlink-target unset rc=%d stdout=%q stderr=%q bytes=%q", rc, out, errw, mustReadScalarUnset(t, physical))
	}
	assertPublicPostWriteCheck(t, rc, out, errw)
	linkAfter, err := os.Readlink(workspace.Path)
	if err != nil {
		t.Fatalf("config symlink disappeared: %v", err)
	}
	linkInfoAfter, err := os.Lstat(workspace.Path)
	if err != nil {
		t.Fatalf("inspect config symlink after unset: %v", err)
	}
	if linkAfter != linkBefore || linkInfoAfter.Mode()&os.ModeSymlink == 0 || linkInfoAfter.Mode().Perm() != linkInfo.Mode().Perm() {
		t.Fatalf("config symlink identity changed: before=%q/%v after=%q/%v", linkBefore, linkInfo.Mode(), linkAfter, linkInfoAfter.Mode())
	}
}

func readableValue(t *testing.T, root *jsonx.OrderedMap, keys ...string) any {
	t.Helper()
	var value any = root
	for _, key := range keys {
		object, ok := value.(*jsonx.OrderedMap)
		if !ok {
			t.Fatalf("%s is not an object: %#v", key, value)
		}
		value, ok = object.Get(key)
		if !ok {
			t.Fatalf("%s is absent", key)
		}
	}
	return value
}

func mustReadScalarUnset(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
