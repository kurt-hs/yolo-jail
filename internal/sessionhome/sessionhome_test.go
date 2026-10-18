package sessionhome

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// fixture is a shared dir with a file, a directory, a lock dir and a rendered surface, and a
// preferred target outside it.
func fixture(t *testing.T) (root string, s Spec) {
	t.Helper()
	root = t.TempDir()
	shared := filepath.Join(root, ".tool", "agent")
	for _, d := range []string{"extensions", "auth.json.lock"} {
		if err := os.MkdirAll(filepath.Join(shared, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for name, body := range map[string]string{"auth.json": "{}", "settings.json": `{"shared":true}`, "AGENTS.md": "x"} {
		if err := os.WriteFile(filepath.Join(shared, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	home, err := Home(shared, "0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	return root, Spec{Shared: shared, Home: home,
		Names:    []string{"auth.json", "models-store.json", "sessions/"},
		Prefer:   map[string]string{"agents/": filepath.Join(root, ".agents")},
		Rendered: []string{"settings.json"}}
}

func TestHomeIsBesideTheSharedDir(t *testing.T) {
	got, err := Home("/h/.tool/agent", "abc12345")
	if err != nil || got != "/h/.tool/.yolo-sessions/abc12345/agent" {
		t.Fatalf("Home = %q, %v", got, err)
	}
	if SessionsRoot("/h/.tool/agent") != "/h/.tool/.yolo-sessions" {
		t.Errorf("SessionsRoot = %q", SessionsRoot("/h/.tool/agent"))
	}
	for _, bad := range []string{"", ".", "..", "a/b"} {
		if _, err := Home("/h/.tool/agent", bad); err == nil {
			t.Errorf("Home accepted session id %q", bad)
		}
	}
}

func TestBuildLinksEverythingButTheRenderedNames(t *testing.T) {
	root, s := fixture(t)
	if err := Build(s); err != nil {
		t.Fatal(err)
	}
	link := func(name string) string {
		t.Helper()
		target, err := os.Readlink(filepath.Join(s.Home, name))
		if err != nil {
			t.Fatalf("%s is not a link: %v", name, err)
		}
		return target
	}
	for _, name := range []string{"auth.json", "extensions", "AGENTS.md", "models-store.json", "sessions"} {
		if got, want := link(name), filepath.Join(s.Shared, name); got != want {
			t.Errorf("%s -> %s, want %s", name, got, want)
		}
	}
	// The declared directory was created in the shared dir; the declared file was linked dangling.
	if fi, err := os.Stat(filepath.Join(s.Shared, "sessions")); err != nil || !fi.IsDir() {
		t.Errorf("shared sessions/ not created: %v", err)
	}
	if _, err := os.Stat(filepath.Join(s.Shared, "models-store.json")); !os.IsNotExist(err) {
		t.Errorf("a declared file was created in the shared dir: %v", err)
	}
	// Its first write through the link creates the shared file.
	if err := os.WriteFile(filepath.Join(s.Home, "models-store.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(s.Shared, "models-store.json")); err != nil {
		t.Errorf("a write through the dangling link did not create the shared file: %v", err)
	}
	// Never a lock, never a rendered name.
	for _, name := range []string{"auth.json.lock", "settings.json"} {
		if _, err := os.Lstat(filepath.Join(s.Home, name)); !os.IsNotExist(err) {
			t.Errorf("%s is in the session home: %v", name, err)
		}
	}
	// agents/: ~/.agents is absent, so the shared agents/ is made and linked.
	if got := link("agents"); got != filepath.Join(s.Shared, "agents") {
		t.Errorf("agents -> %s, want the shared agents/", got)
	}
	_ = root
}

func TestBuildPrefersAnExistingPreferredTarget(t *testing.T) {
	_, s := fixture(t)
	if err := os.MkdirAll(s.Prefer["agents/"], 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Build(s); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.Readlink(filepath.Join(s.Home, "agents")); got != s.Prefer["agents/"] {
		t.Errorf("agents -> %q, want %q", got, s.Prefer["agents/"])
	}
	if _, err := os.Lstat(filepath.Join(s.Shared, "agents")); !os.IsNotExist(err) {
		t.Errorf("the shared agents/ was made though the preferred target exists: %v", err)
	}
}

// SP-D2: a rendered name occupied by a link (an older pass, a hand edit) is unlinked, so the
// render that follows writes the session's own file instead of writing through to the shared one.
func TestBuildUnlinksARenderedName(t *testing.T) {
	_, s := fixture(t)
	if err := os.MkdirAll(s.Home, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(s.Shared, "settings.json"), filepath.Join(s.Home, "settings.json")); err != nil {
		t.Fatal(err)
	}
	if err := Build(s); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(s.Home, "settings.json")); !os.IsNotExist(err) {
		t.Errorf("the rendered name's link survived: %v", err)
	}
}

func TestBuildIsIdempotentAndStraysAreNamed(t *testing.T) {
	_, s := fixture(t)
	if err := Build(s); err != nil {
		t.Fatal(err)
	}
	if err := Build(s); err != nil {
		t.Fatalf("a second build: %v", err)
	}
	if strays, err := Strays(s); err != nil || len(strays) != 0 {
		t.Fatalf("a fresh home has strays %v, %v", strays, err)
	}
	// The render's own file is no stray; a file the program made, and a link it replaced with a
	// regular file, are.
	for _, name := range []string{"settings.json", "new-state.json"} {
		if err := os.WriteFile(filepath.Join(s.Home, name), []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	_ = os.Remove(filepath.Join(s.Home, "auth.json"))
	if err := os.WriteFile(filepath.Join(s.Home, "auth.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	strays, err := Strays(s)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"auth.json", "new-state.json"}; !reflect.DeepEqual(strays, want) {
		t.Errorf("strays = %v, want %v", strays, want)
	}
	// A rebuild keeps a stray where it is.
	if err := Build(s); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Lstat(filepath.Join(s.Home, "auth.json")); err != nil || fi.Mode()&os.ModeSymlink != 0 {
		t.Errorf("a rebuild replaced the stray auth.json: %v", err)
	}
}

// A home that cannot be made is an error naming the path (SP-D6's input).
func TestBuildFailsNamingThePath(t *testing.T) {
	_, s := fixture(t)
	root := SessionsRoot(s.Shared)
	if err := os.WriteFile(root, []byte("in the way"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := Build(s)
	if err == nil || !strings.Contains(err.Error(), root) {
		t.Fatalf("Build = %v, want an error naming %s", err, root)
	}
}

func TestSpecRefusesALockName(t *testing.T) {
	_, s := fixture(t)
	s.Names = append(s.Names, "x.lock")
	if err := Build(s); err == nil || !strings.Contains(err.Error(), "x.lock") {
		t.Fatalf("Build = %v, want a refusal naming x.lock", err)
	}
}

// A RENDERED FILE IN A SUBDIRECTORY makes only the directories on its path real: every other
// entry of each of them is still linked to its shared counterpart. Measured in a nested jail on
// 2026-10-18: a pack rendering `extensions/subagent/config.json` turned the whole `extensions/`
// into a directory holding that one file, so the session's pi loaded none of its extensions.
func TestBuildMergesASubdirectoryHoldingARenderedFile(t *testing.T) {
	_, s := fixture(t)
	for name, body := range map[string]string{
		"extensions/policy.js":            "js",
		"extensions/subagent/other.txt":   "o",
		"extensions/subagent/config.json": `{"shared":true}`,
	} {
		p := filepath.Join(s.Shared, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	s.Rendered = append(s.Rendered, "extensions/subagent/config.json")
	if err := Build(s); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{"extensions", "extensions/subagent"} {
		fi, err := os.Lstat(filepath.Join(s.Home, filepath.FromSlash(dir)))
		if err != nil || !fi.IsDir() || fi.Mode()&os.ModeSymlink != 0 {
			t.Errorf("%s must be a real directory in the session home (%v, %v)", dir, fi, err)
		}
	}
	for _, name := range []string{"extensions/policy.js", "extensions/subagent/other.txt"} {
		got, err := os.Readlink(filepath.Join(s.Home, filepath.FromSlash(name)))
		if want := filepath.Join(s.Shared, filepath.FromSlash(name)); err != nil || got != want {
			t.Errorf("%s = link %q (%v), want a link to %s", name, got, err, want)
		}
	}
	if _, err := os.Lstat(filepath.Join(s.Home, "extensions", "subagent", "config.json")); !os.IsNotExist(err) {
		t.Errorf("the rendered file must be left for the render to write, not linked (%v)", err)
	}
	if err := os.WriteFile(filepath.Join(s.Home, "extensions", "subagent", "config.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if strays, err := Strays(s); err != nil || len(strays) != 0 {
		t.Errorf("a merged directory and its rendered file are not strays: %v, %v", strays, err)
	}
	if err := os.WriteFile(filepath.Join(s.Home, "extensions", "new.js"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if strays, _ := Strays(s); !reflect.DeepEqual(strays, []string{"extensions/new.js"}) {
		t.Errorf("a file the program created inside a merged directory is a stray named by its path; got %v", strays)
	}
}

// A lock the program takes in its session home (`<file>.lock`, made by mkdir, SP-D10) is neither
// linked nor a stray, so a pass during a write names nothing and a leftover lock keeps no home.
func TestALockIsNeverAStray(t *testing.T) {
	_, s := fixture(t)
	if err := Build(s); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(s.Home, "settings.json.lock"), 0o755); err != nil {
		t.Fatal(err)
	}
	if strays, err := Strays(s); err != nil || len(strays) != 0 {
		t.Errorf("Strays = %v, %v; a lock is the program's, never a stray", strays, err)
	}
}
