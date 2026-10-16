package loopholes

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// oldjailtree_test.go pins the attach to a jail an older yolo launched. That jail's pack tree is
// frozen at its launch, so its shipped credential loopholes still declare the retired
// `"scope": "host"`, and an attach adopts that tree for every host-side reader (the briefing
// refresh among them). The loopholes must stay in the set, and nothing may tell the user to edit
// the frozen manifest as if the loophole had failed.

// oldShippedManifest copies a shipped loophole's manifest into a module dir of its own, as the
// version before HD-R1 shipped it: `scope: "host"` where `detach` now is.
func oldShippedManifest(t *testing.T, pack, name string) string {
	t.Helper()
	src := filepath.Join("..", "..", "packs", pack, "loopholes", name, "manifest.jsonc")
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	old := strings.Replace(string(data), `"detach": true`, `"scope": "host"`, 1)
	if old == string(data) {
		t.Fatalf("%s declares no detach to turn back into scope host", src)
	}
	dir := filepath.Join(t.TempDir(), "packs", pack, "loopholes", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.jsonc"), []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestAnOlderJailsShippedCredentialLoopholesStayInTheSet(t *testing.T) {
	warnings := captureWarnings(t)
	ResetPackModules()
	ResetPackSupersessions()
	t.Cleanup(func() {
		ResetPackModules()
		ResetPackSupersessions()
	})
	var mods []PackModule
	for _, l := range [][2]string{{"claude", "claude-oauth-broker"}, {"openai-auth", "openai-auth-broker"},
		{"aws-auth", "aws-auth"}} {
		mods = append(mods, PackModule{Dir: oldShippedManifest(t, l[0], l[1]), HostExecApproved: true})
	}
	SetPackModules(mods)
	SetPackSupersessions(nil)

	got := map[string]bool{}
	for _, lp := range NewHostSet(nil).All() {
		got[lp.Name] = true
		if lp.HostDaemon != nil && lp.HostDaemon.Scope != ScopeJail {
			t.Errorf("%s scope = %q, want it read as one per jail", lp.Name, lp.HostDaemon.Scope)
		}
	}
	for _, want := range []string{"claude-oauth-broker", "openai-auth-broker", "aws-auth"} {
		if !got[want] {
			t.Errorf("%s vanished from the set read from an older jail's tree (got %v)\nwarnings: %v",
				want, got, *warnings)
		}
	}
	for _, w := range *warnings {
		if strings.Contains(w, "NOT active") {
			t.Errorf("an older jail's shipped loophole was reported as not active: %s", w)
		}
	}
}
