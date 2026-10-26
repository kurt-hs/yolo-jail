package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/jailcontent"
)

func setupHostMechanicsTest(t *testing.T) string {
	t.Helper()
	return userSkillsFixture(t)
}

func TestHostApplyComposesGeneratedHostSkillsThroughOwnership(t *testing.T) {
	home := setupHostMechanicsTest(t)
	rc, report := applyWith(t, true, strings.NewReader("y\ny\n"))
	if rc != 0 {
		t.Fatalf("host apply rc=%d\n%s", rc, report)
	}
	workspace, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	claude := filepath.Join(home, ".claude", "skills")
	for _, name := range []string{"configuring-the-jail", "diagnosing-the-jail", "jail-mechanics"} {
		body, err := os.ReadFile(filepath.Join(claude, name, "SKILL.md"))
		if err != nil {
			t.Fatalf("host built-in %s missing: %v\n%s", name, err, report)
		}
		if !strings.Contains(string(body), workspace) {
			t.Errorf("%s does not name the actual host workspace %q", name, workspace)
		}
		if strings.Contains(string(body), "restart the jail") || strings.Contains(string(body), "loopback") {
			t.Errorf("%s contains jail-only host advice", name)
		}
	}
	configPath := filepath.Join(home, ".config", "yolo-jail", "config.jsonc")
	configuring, _ := os.ReadFile(filepath.Join(claude, "configuring-the-jail", "SKILL.md"))
	if !strings.Contains(string(configuring), configPath) {
		t.Errorf("host skill does not name the actual user config path %q", configPath)
	}

	before := linkAwareHashes(t, home)
	rc, report = applyWith(t, true, nil)
	if rc != 0 {
		t.Fatalf("second host apply rc=%d\n%s", rc, report)
	}
	if strings.Contains(report, "[y/N]") || strings.Contains(report, "no longer in your config") {
		t.Errorf("generated content was treated as user content or a dropped pack:\n%s", report)
	}
	if after := linkAwareHashes(t, home); !equalStringMaps(before, after) {
		t.Errorf("reapplying changed generated host skills\n%s", report)
	}
}

func TestHostApplyStillRetiresARealDroppedPack(t *testing.T) {
	home := setupHostMechanicsTest(t)
	rc, report := applyWith(t, true, strings.NewReader("y\ny\n"))
	if rc != 0 {
		t.Fatalf("initial host apply rc=%d\n%s", rc, report)
	}
	selectPacks(t, home, `"claude","codex","pi"`)
	rc, report = applyWith(t, true, nil)
	if rc != 0 {
		t.Fatalf("host apply rc=%d\n%s", rc, report)
	}
	if !strings.Contains(report, "shared") || !strings.Contains(report, "no longer in your config") {
		t.Errorf("real dropped pack was not reported for retirement:\n%s", report)
	}
	if strings.Contains(report, "yolo:generated-skills") {
		t.Errorf("generated ownership leaked into dropped-pack retirement:\n%s", report)
	}
}

func TestHostApplyObserveDoesNotWriteGeneratedSkills(t *testing.T) {
	home := setupHostMechanicsTest(t)
	before := linkAwareHashes(t, home)
	rc, report := applyWith(t, false, nil)
	if rc != 0 {
		t.Fatalf("host apply observe rc=%d\n%s", rc, report)
	}
	if after := linkAwareHashes(t, home); !equalStringMaps(before, after) {
		t.Errorf("observe changed the home")
	}
	for _, name := range []string{"configuring-the-jail", "diagnosing-the-jail", "jail-mechanics"} {
		if _, err := os.Stat(filepath.Join(home, ".claude", "skills", name)); !os.IsNotExist(err) {
			t.Errorf("observe created host built-in %s (err=%v)", name, err)
		}
	}
}

func TestHostApplyBuiltinRenderFailureKeepsPreviousSuite(t *testing.T) {
	home := setupHostMechanicsTest(t)
	rc, report := applyWith(t, true, strings.NewReader("y\ny\n"))
	if rc != 0 {
		t.Fatalf("initial host apply rc=%d\n%s", rc, report)
	}
	before := linkAwareHashes(t, home)
	oldRenderer := renderHostBuiltinSkills
	renderHostBuiltinSkills = func(_ jailcontent.FactSheet) (map[string][]byte, error) {
		return nil, fmt.Errorf("injected render failure")
	}
	t.Cleanup(func() { renderHostBuiltinSkills = oldRenderer })
	rc, report = applyWith(t, true, nil)
	if rc != 0 {
		t.Fatalf("renderer failure must not fail host apply (rc=%d)\n%s", rc, report)
	}
	if !strings.Contains(report, "keeping the previous suite") || !strings.Contains(report, "rerun `yolo host apply`") {
		t.Errorf("render failure did not explain recovery:\n%s", report)
	}
	if after := linkAwareHashes(t, home); !equalStringMaps(before, after) {
		t.Errorf("render failure changed the previous suite\n%s", report)
	}
}

func equalStringMaps(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}
