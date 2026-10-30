package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/testconfig"
)

// activeProviderFixture is a host workspace whose user config selects the shipped claude and zai packs and a zai variant
// and holds [active_provider] in a config.d fragment, so a test can tell "the file that already
// holds it" from the default user file.
func activeProviderFixture(t *testing.T) (userConfig, fragment string) {
	t.Helper()
	ws := testconfig.Workspace(t, "")
	user := testconfig.User(t, "packs = [\"claude\", \"zai\"]\n[providers.zai.variants.fast]\nmodel = \"glm-5.3\"\n")
	fragment = filepath.Join(filepath.Dir(user.Path), "config.d", "work.toml")
	if err := os.MkdirAll(filepath.Dir(fragment), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fragment, []byte("[active_provider]\ndefault = \"native\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", user.Root)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("YOLO_VERSION", "")
	t.Setenv("YOLO_WORKSPACE", "")
	t.Setenv("YOLO_HOST_DIR", "")
	withConfigEditCWD(t, ws.Root)
	return user.Path, fragment
}

func TestActiveProviderVerbRefusesUndeclaredNameListingDeclared(t *testing.T) {
	user, fragment := activeProviderFixture(t)
	before := readFile(t, fragment)
	rc, out, errs := captureStd(t, func() int {
		return registry["active-provider"]([]string{"active-provider", "claude", "nonesuch"})
	})
	if rc != 1 {
		t.Fatalf("rc=%d, want 1; stdout=%q stderr=%q", rc, out, errs)
	}
	for _, want := range []string{"nonesuch", "zai", "native", "yolo pack add"} {
		if !strings.Contains(errs, want) {
			t.Fatalf("refusal does not name %q: %q", want, errs)
		}
	}
	if readFile(t, fragment) != before || strings.Contains(readFile(t, user), "active_provider") {
		t.Fatal("a refused verb wrote a config file")
	}
}

func TestActiveProviderVerbWritesNativeForOneAgent(t *testing.T) {
	user, fragment := activeProviderFixture(t)
	userBefore := readFile(t, user)
	rc, out, errs := captureStd(t, func() int {
		return registry["active-provider"]([]string{"active-provider", "claude", "native"})
	})
	if rc != 0 {
		t.Fatalf("rc=%d stdout=%q stderr=%q", rc, out, errs)
	}
	got := readFile(t, fragment)
	if !strings.HasPrefix(got, "[active_provider]\ndefault = \"native\"\n") || !strings.Contains(got, "claude = \"native\"") {
		t.Fatalf("the file holding [active_provider] was not spliced:\n%s", got)
	}
	if readFile(t, user) != userBefore {
		t.Fatal("config.toml changed although it does not hold [active_provider]")
	}
	if !strings.Contains(out, "takes effect at the next launch") || !strings.Contains(out, fragment) {
		t.Fatalf("stdout lacks the file or the next-launch line: %q", out)
	}
}

func TestActiveProviderVerbWritesProviderAndVariant(t *testing.T) {
	_, fragment := activeProviderFixture(t)

	for _, name := range []string{"zai", "zai/fast"} {
		rc, out, errs := captureStd(t, func() int {
			return registry["active-provider"]([]string{"active-provider", "default", name})
		})
		if rc != 0 {
			t.Fatalf("%s: rc=%d stdout=%q stderr=%q", name, rc, out, errs)
		}
		if got := readFile(t, fragment); !strings.Contains(got, "default = \""+name+"\"") || strings.Contains(got, "native") {
			t.Fatalf("%s: not written:\n%s", name, got)
		}
	}
}

func TestActiveProviderVerbInJailPrintsHostRun(t *testing.T) {
	_, fragment := activeProviderFixture(t)
	before := readFile(t, fragment)
	t.Setenv("YOLO_VERSION", "test")
	t.Setenv("YOLO_HOST_DIR", "/home/someone/code/proj")
	rc, out, errs := captureStd(t, func() int {
		return registry["active-provider"]([]string{"active-provider", "claude", "native"})
	})
	if rc != 1 || !strings.Contains(errs, "yolo host-run") || !strings.Contains(errs, "yolo active-provider claude native") {
		t.Fatalf("rc=%d stdout=%q stderr=%q", rc, out, errs)
	}
	if readFile(t, fragment) != before {
		t.Fatal("the in-jail refusal wrote a file")
	}
}
