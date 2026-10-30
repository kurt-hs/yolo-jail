package cli

import (
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/configdoc"
	"github.com/mschulkind-oss/yolo-jail/internal/testconfig"
)

// genericFixture points HOME at a user config holding userText and the cwd at a workspace
// holding project (in format), the fresh-config state every advice line naming the generic
// verbs meets (OQ-RC10).
func genericFixture(t *testing.T, userText string, format configdoc.Format, project string) (user, ws testconfig.Fixture) {
	t.Helper()
	ws = testconfig.WorkspaceFormat(t, format, project)
	user = testconfig.User(t, userText)
	t.Setenv("HOME", user.Root)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("YOLO_VERSION", "")
	t.Setenv("YOLO_WORKSPACE", "")
	t.Setenv("YOLO_HOST_DIR", "")
	withConfigEditCWD(t, ws.Root)
	return user, ws
}

func runConfigVerb(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	return captureStd(t, func() int { return registry["config"](append([]string{"config"}, args...)) })
}

func assertValidFile(t *testing.T, path string, format configdoc.Format, comment, ws string) string {
	t.Helper()
	got := readFile(t, path)
	if _, err := configdoc.Parse([]byte(got), format); err != nil {
		t.Fatalf("%s no longer parses: %v\n%s", path, err, got)
	}
	if !strings.Contains(got, comment) {
		t.Fatalf("%s lost its comment %q:\n%s", path, comment, got)
	}
	if errs := launchErrors(t, ws); len(errs) != 0 {
		t.Fatalf("launch validation rejects the written file: %v\n%s", errs, got)
	}
	return got
}

func TestConfigSetAddsAMissingScalarToTheUserFile(t *testing.T) {
	user, ws := genericFixture(t, "# keep user\n", configdoc.TOML, "# keep project\n")
	rc, out, errs := runConfigVerb(t, "set", "update_check", "false", "--user")
	assertPublicPostWriteCheck(t, rc, out, errs)
	if !strings.Contains(out, "Written") {
		t.Fatalf("nothing written: rc=%d stdout=%q stderr=%q", rc, out, errs)
	}
	got := assertValidFile(t, user.Path, configdoc.TOML, "# keep user", ws.Root)
	if !strings.Contains(got, "update_check = false") {
		t.Fatalf("value not written:\n%s", got)
	}
}

func TestConfigSetAddsAMissingNestedKeyInAProjectTOML(t *testing.T) {
	_, ws := genericFixture(t, "", configdoc.TOML, "# keep project\n")
	rc, out, errs := runConfigVerb(t, "set", "network.mode", "bridge")
	assertPublicPostWriteCheck(t, rc, out, errs)
	if !strings.Contains(out, "Written") {
		t.Fatalf("nothing written: rc=%d stdout=%q stderr=%q", rc, out, errs)
	}
	got := assertValidFile(t, ws.Path, configdoc.TOML, "# keep project", ws.Root)
	if !strings.Contains(got, "[network]") || !strings.Contains(got, `mode = "bridge"`) {
		t.Fatalf("nested key not written:\n%s", got)
	}
}

func TestConfigSetAddsAMissingNestedKeyInAProjectJSONC(t *testing.T) {
	_, ws := genericFixture(t, "", configdoc.JSONC, "{\n  // keep project\n}\n")
	rc, out, errs := runConfigVerb(t, "set", "network.mode", "bridge")
	assertPublicPostWriteCheck(t, rc, out, errs)
	if !strings.Contains(out, "Written") {
		t.Fatalf("nothing written: rc=%d stdout=%q stderr=%q", rc, out, errs)
	}
	got := assertValidFile(t, ws.Path, configdoc.JSONC, "// keep project", ws.Root)
	if !strings.Contains(got, `"mode": "bridge"`) {
		t.Fatalf("nested key not written:\n%s", got)
	}
}

func TestConfigAddCreatesAMissingList(t *testing.T) {
	_, ws := genericFixture(t, "", configdoc.TOML, "# keep project\n")
	rc, out, errs := runConfigVerb(t, "add", "install.nix", "ripgrep")
	assertPublicPostWriteCheck(t, rc, out, errs)
	if !strings.Contains(out, "Written") {
		t.Fatalf("nothing written: rc=%d stdout=%q stderr=%q", rc, out, errs)
	}
	got := assertValidFile(t, ws.Path, configdoc.TOML, "# keep project", ws.Root)
	if !strings.Contains(got, "ripgrep") {
		t.Fatalf("item not written:\n%s", got)
	}
	// A second add appends to the list the first one created.
	rc, out, errs = runConfigVerb(t, "add", "install.nix", "fd")
	assertPublicPostWriteCheck(t, rc, out, errs)
	got = assertValidFile(t, ws.Path, configdoc.TOML, "# keep project", ws.Root)
	if !strings.Contains(got, "ripgrep") || !strings.Contains(got, "fd") {
		t.Fatalf("list lost an item:\n%s", got)
	}
}

func TestConfigAddOnTheRenamedPackagesKeyNamesTheReplacement(t *testing.T) {
	_, ws := genericFixture(t, "", configdoc.TOML, "# keep project\n")
	rc, _, errs := runConfigVerb(t, "add", "packages", "ripgrep")
	if rc == 0 || !strings.Contains(errs, "install.nix") {
		t.Fatalf("want a refusal naming install.nix, got rc=%d stderr=%q", rc, errs)
	}
	if got := readFile(t, ws.Path); got != "# keep project\n" {
		t.Fatalf("a refusal wrote the file:\n%s", got)
	}
}

func TestConfigSetUnderANonTableParentRefusesAndNamesTheNextStep(t *testing.T) {
	_, ws := genericFixture(t, "", configdoc.TOML, "# keep project\nnetwork = \"bridge\"\n")
	rc, _, errs := runConfigVerb(t, "set", "network.mode", "bridge")
	if rc == 0 {
		t.Fatalf("set under a scalar parent succeeded")
	}
	if !strings.Contains(errs, "network") || !strings.Contains(errs, "yolo check") {
		t.Fatalf("refusal does not name the key and a next step: %q", errs)
	}
	if got := readFile(t, ws.Path); !strings.Contains(got, `network = "bridge"`) {
		t.Fatalf("a refusal changed the file:\n%s", got)
	}
}

func TestConfigSetAnUnknownKeyRefuses(t *testing.T) {
	_, ws := genericFixture(t, "", configdoc.TOML, "# keep project\n")
	rc, _, errs := runConfigVerb(t, "set", "bogus.key", "1")
	if rc == 0 || !strings.Contains(errs, "bogus.key") {
		t.Fatalf("want a refusal naming the key, got rc=%d stderr=%q", rc, errs)
	}
	if got := readFile(t, ws.Path); got != "# keep project\n" {
		t.Fatalf("a refusal wrote the file:\n%s", got)
	}
}

// The command a writable-mount refusal names must work on a user config with no [allow.mounts].
func TestConfigAddCreatesTheAllowMountsCeilingInTheUserFile(t *testing.T) {
	user, ws := genericFixture(t, "# keep user\n", configdoc.TOML, "# keep project\n")
	rc, out, errs := runConfigVerb(t, "add", "allow.mounts.on_request", "write", "--user")
	assertPublicPostWriteCheck(t, rc, out, errs)
	got := assertValidFile(t, user.Path, configdoc.TOML, "# keep user", ws.Root)
	if !strings.Contains(got, "[allow.mounts]") || !strings.Contains(got, `on_request = ["write"]`) {
		t.Fatalf("ceiling not written:\n%s", got)
	}
}
