package entrypoint

import (
	"os"
	"path/filepath"
	"testing"
)

// TestConfigureAgyPrismFirstMigration proves agy's settings.json port: agy is
// born directly on the prism (no bespoke writer, no gate). The prism renders the
// force-managed permissionMode:"allow", seeds the sidecars with an empty overlay
// (§3.2 first migration), and the dynamic mcp_config.json sibling is written
// (it stays a pure-overwrite bespoke sibling — the prism owns only settings.json).
func TestConfigureAgyPrismFirstMigration(t *testing.T) {
	home := t.TempDir()
	ws := t.TempDir()
	e := &Env{Home: home, Workspace: ws, Vars: map[string]string{}}

	if err := ConfigurePackByName(e, "agy"); err != nil {
		t.Fatalf("ConfigureAgyPrism: %v", err)
	}

	// settings.json rendered by the prism: the managed permissionMode lands.
	settings := decodeJSONFile(t, filepath.Join(e.AgyDir(), "settings.json"))
	if settings["permissionMode"] != "allow" {
		t.Errorf("settings.json permissionMode = %v, want \"allow\" (managed)", settings["permissionMode"])
	}

	// Sidecars seeded (proves the surface went through the stateful harness).
	if _, err := os.Stat(prismLastRenderPath(e, "agy", "settings")); err != nil {
		t.Errorf("last_render sidecar missing: %v", err)
	}
	overlay := decodeJSONFile(t, prismOverlayPath(e, "agy", "settings"))
	if len(overlay) != 0 {
		t.Errorf("overlay = %v, want {} on first migration", overlay)
	}

	// The dynamic mcp_config.json sibling is written (bespoke pure-overwrite).
	mcp := decodeJSONFile(t, filepath.Join(e.AgyDir(), "mcp_config.json"))
	if _, ok := mcp["mcpServers"]; !ok {
		t.Errorf("mcp_config.json missing mcpServers key: %v", mcp)
	}
}

// TestConfigureAgyPrismManagedReverts proves the managed posture: a user who
// edits permissionMode to something permissive-off has it forced back to "allow"
// on the next boot (managed layer wins — the container is the trust boundary).
func TestConfigureAgyPrismManagedReverts(t *testing.T) {
	home := t.TempDir()
	ws := t.TempDir()
	e := &Env{Home: home, Workspace: ws, Vars: map[string]string{}}
	if err := os.MkdirAll(e.AgyDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	// First boot to seed the sidecars.
	if err := ConfigurePackByName(e, "agy"); err != nil {
		t.Fatalf("ConfigureAgyPrism (boot 1): %v", err)
	}
	// User flips permissionMode off between boots.
	if err := os.WriteFile(filepath.Join(e.AgyDir(), "settings.json"),
		[]byte("{\"permissionMode\": \"ask\"}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Second boot: managed reasserts "allow".
	if err := ConfigurePackByName(e, "agy"); err != nil {
		t.Fatalf("ConfigureAgyPrism (boot 2): %v", err)
	}
	settings := decodeJSONFile(t, filepath.Join(e.AgyDir(), "settings.json"))
	if settings["permissionMode"] != "allow" {
		t.Errorf("settings.json permissionMode = %v, want \"allow\" (managed reverts user edit)", settings["permissionMode"])
	}
}

// agyMCPLoop lays the cycle measured on 2026-10-29: agy's DECLARED mcp_config.json links into
// ~/.gemini/config, and the file at that end links to ITS OWN in-jail path, so the declared path
// answers ELOOP. LoopingLink's comment carries what that cost (the whole boot refused on
// configure_agy_mcp).
func agyMCPLoop(t *testing.T, e *Env) (declared, other string) {
	t.Helper()
	declared = filepath.Join(e.AgyDir(), "mcp_config.json")
	other = filepath.Join(e.GeminiDir(), "config", "mcp_config.json")
	if err := os.MkdirAll(filepath.Dir(other), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(e.AgyDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(other, declared); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(other, other); err != nil {
		t.Fatal(err)
	}
	return declared, other
}

// TestConfigureAgyMCPReplacesALinkLoop: a symlink CYCLE on the way to agy's declared
// mcp_config.json is repaired by the render instead of failing the boot. Before the fix the write
// followed the chain, open(2) answered ELOOP, and configure_agy_mcp refused the whole launch —
// every other agent in the jail with it. A cycle holds no bytes, so replacing it loses nothing.
//
// THE AGENT'S LINK IS KEPT: the loop is removed at the far end, where the repeat is, so the
// declared path still links into ~/.gemini/config and the render lands in the file agy reads.
func TestConfigureAgyMCPReplacesALinkLoop(t *testing.T) {
	e := siblingEnv(t, nil)
	declared, other := agyMCPLoop(t, e)

	if err := ConfigurePackByName(e, "agy"); err != nil {
		t.Fatalf("ConfigureAgyPrism with a link loop at mcp_config.json: %v", err)
	}

	if fi, err := os.Lstat(declared); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Errorf("the agent's own link was replaced: %v, %v (want it kept, with the render in its target)", fi, err)
	}
	if fi, err := os.Lstat(other); err != nil || !fi.Mode().IsRegular() {
		t.Errorf("the looping link was not replaced by a regular file: %v, %v", fi, err)
	}
	mcp := decodeJSONFile(t, declared)
	if _, ok := mcp["mcpServers"]; !ok {
		t.Errorf("mcp_config.json missing mcpServers key after the repair: %v", mcp)
	}
}

// TestConfigureAgyMCPReplacesALoopAboveTheFile: the same rule one directory up, where the cycle
// is the surface's own parent. That shape reaches this code as ELOOP from an Lstat of a deeper
// component rather than as a link to be examined, which is the step up firstLinkOnPath adds.
func TestConfigureAgyMCPReplacesALoopAboveTheFile(t *testing.T) {
	e := siblingEnv(t, nil)
	gemini := e.GeminiDir()
	if err := os.MkdirAll(e.Home, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(gemini, gemini); err != nil {
		t.Fatal(err)
	}

	if err := ConfigurePackByName(e, "agy"); err != nil {
		t.Fatalf("ConfigureAgyPrism with a looping ~/.gemini: %v", err)
	}
	if fi, err := os.Lstat(gemini); err != nil || !fi.IsDir() {
		t.Fatalf("~/.gemini after the render: %v, %v (want a real directory)", fi, err)
	}
	mcp := decodeJSONFile(t, filepath.Join(e.AgyDir(), "mcp_config.json"))
	if _, ok := mcp["mcpServers"]; !ok {
		t.Errorf("mcp_config.json missing mcpServers key after the repair: %v", mcp)
	}
}

// TestConfigureAgyMCPWritesThroughATerminatingLink pins the half that must NOT change: agy's
// real layout is a link that resolves (declared path -> ~/.gemini/config/mcp_config.json), and
// the render writes through it, filling its target. Only a chain that never ends is repaired.
func TestConfigureAgyMCPWritesThroughATerminatingLink(t *testing.T) {
	e := siblingEnv(t, nil)
	declared := filepath.Join(e.AgyDir(), "mcp_config.json")
	other := filepath.Join(e.GeminiDir(), "config", "mcp_config.json")
	if err := os.MkdirAll(filepath.Dir(other), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(e.AgyDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(other, declared); err != nil {
		t.Fatal(err)
	}

	if err := ConfigurePackByName(e, "agy"); err != nil {
		t.Fatalf("ConfigureAgyPrism through a terminating link: %v", err)
	}
	fi, err := os.Lstat(declared)
	if err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Errorf("the agent's own link was replaced: %v, %v (want it kept, with the render in its target)", fi, err)
	}
	mcp := decodeJSONFile(t, other)
	if _, ok := mcp["mcpServers"]; !ok {
		t.Errorf("the link's target did not receive the render: %v", mcp)
	}
}
