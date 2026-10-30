package packmigrate

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/packdecl"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
)

// writePack lays out a fixture pack: files maps a pack-relative path to its content; a path
// ending in "*" is written executable (the star is dropped).
func writePack(t *testing.T, manifest string, files map[string]string) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	dir = filepath.Join(dir, "fixture-pack")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, packdecl.ManifestLegacyJSONName), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	for rel, content := range files {
		mode := fs.FileMode(0o644)
		if strings.HasSuffix(rel, "*") {
			rel, mode = strings.TrimSuffix(rel, "*"), 0o755
		}
		full := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), mode); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(full, mode); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// treeSnapshot is every path under dir with its content hash and mode, to prove nothing moved.
func treeSnapshot(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(full string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, full)
		if entry.IsDir() {
			if entry.Name() == ".git" {
				return filepath.SkipDir
			}
			out[rel+"/"] = "dir"
			return nil
		}
		data, err := os.ReadFile(full)
		if err != nil {
			return err
		}
		info, _ := entry.Info()
		sum := sha256.Sum256(data)
		out[rel] = hex.EncodeToString(sum[:]) + " " + info.Mode().String()
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func equalSnapshots(a, b map[string]string) bool {
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

const plainFixture = `{
  // The fixture pack's own name.
  "name": "fixture-pack",
  "description": "A fixture with plain copies.",
  "contributes": [
    // Sources, checked 2026-10-29:
    //   apt: https://packages.debian.org/trixie/fd-find
    { "kind": "program", "bin": "fd", "install_hints": { "apt": "fd-find", "brew": "fd" } },
    /* One script, copied to a path. */
    { "kind": "files", "from": "bin/finder.sh", "into": ".claude/bin/finder.sh" },
    { "kind": "files", "from": "ext", "into": ".pi/agent/extensions" },
    // An addressed pi package stays one tree.
    { "kind": "files", "agents": ["pi"], "from": "pi-package" },
    { "kind": "config-overlay", "surface": "claude/settings",
      "config": { "managed": { "fileSuggestion": { "type": "command", "command": "~/.claude/bin/finder.sh" } } } }
  ]
}
`

func plainFixtureFiles() map[string]string {
	return map[string]string{
		"bin/finder.sh*":                 "#!/bin/sh\nfd \"$@\"\n",
		"ext/turn-bell.ts":               "export default 1\n",
		"ext/board/index.ts":             "export default 2\n",
		"pi-package/extensions/x.ts":     "export default 3\n",
		"pi-package/themes/dark.json":    "{}\n",
		"README.md":                      "A fixture.\n",
		"skills/fixture-skill/SKILL.md":  "---\nname: fixture-skill\ndescription: d\n---\n",
		"unrelated/notes.txt":            "kept\n",
		"ext-sibling-not-moved/file.txt": "kept\n",
	}
}

func TestPlanMovesPlainFilesIntoHome(t *testing.T) {
	dir := writePack(t, plainFixture, plainFixtureFiles())
	before, problems := packload.LoadDir(dir, "")
	if len(problems) != 0 {
		t.Fatalf("fixture does not load: %v", problems)
	}
	beforeDelivery, err := Delivery(before)
	if err != nil {
		t.Fatal(err)
	}

	m, err := Plan(dir, Options{})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	gotMoves := make([]string, len(m.Moves))
	for i, move := range m.Moves {
		gotMoves[i] = move.From + " -> " + move.To
	}
	sort.Strings(gotMoves)
	wantMoves := []string{
		"bin/finder.sh -> home/.claude/bin/finder.sh",
		"ext/board/index.ts -> home/.pi/agent/extensions/board/index.ts",
		"ext/turn-bell.ts -> home/.pi/agent/extensions/turn-bell.ts",
	}
	if strings.Join(gotMoves, "\n") != strings.Join(wantMoves, "\n") {
		t.Fatalf("moves:\n%s\nwant:\n%s", strings.Join(gotMoves, "\n"), strings.Join(wantMoves, "\n"))
	}
	if len(m.Kept) != 1 || m.Kept[0].From != "pi-package" {
		t.Errorf("kept files entries = %#v, want only the addressed pi package", m.Kept)
	}
	text := string(m.Manifest)
	for _, want := range []string{
		"# The fixture pack's own name.\nname = \"fixture-pack\"",
		"# Sources, checked 2026-10-29:\n#   apt: https://packages.debian.org/trixie/fd-find\n[[program]]\nbin = \"fd\"\ninstall = { default = false, brew = \"fd\", apt = \"fd-find\" }",
		"# One script, copied to a path.",
		"# An addressed pi package stays one tree.\n[[files]]\nagents = [\"pi\"]\nfrom = \"pi-package\"",
		"[[overlay]]\nsurface = \"claude/settings\"\nset.fileSuggestion = ",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("manifest lacks %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "finder.sh\"\ninto") || strings.Contains(text, "from = \"ext\"") || strings.Contains(text, "install_hints") || strings.Contains(text, "kind =") {
		t.Errorf("manifest still declares a folded copy or a legacy spelling:\n%s", text)
	}
	if len(m.Comments) != 5 {
		t.Errorf("carried %d comments, want all 5: %q", len(m.Comments), m.Comments)
	}

	if err := Apply(m); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, packdecl.ManifestLegacyJSONName)); !os.IsNotExist(err) {
		t.Errorf("pack.json still present after migration (err=%v)", err)
	}
	for _, gone := range []string{"bin", "ext"} {
		if _, err := os.Stat(filepath.Join(dir, gone)); !os.IsNotExist(err) {
			t.Errorf("emptied source %s/ remains (err=%v)", gone, err)
		}
	}
	for _, kept := range []string{"pi-package/extensions/x.ts", "unrelated/notes.txt", "ext-sibling-not-moved/file.txt", "skills/fixture-skill/SKILL.md"} {
		if _, err := os.Stat(filepath.Join(dir, kept)); err != nil {
			t.Errorf("%s did not stay put: %v", kept, err)
		}
	}
	after, problems := packload.LoadDir(dir, "")
	if len(problems) != 0 {
		t.Fatalf("migrated pack does not load: %v", problems)
	}
	if after.Decl.Origin.Format != packdecl.ManifestFormatTOML {
		t.Fatalf("migrated pack read from %v, want pack.toml", after.Decl.Origin)
	}
	afterDelivery, err := Delivery(after)
	if err != nil {
		t.Fatal(err)
	}
	if diffs := DeliveryDifferences(beforeDelivery, afterDelivery); len(diffs) != 0 || len(afterDelivery) != 3 {
		t.Errorf("delivery changed (%d files): %v", len(afterDelivery), diffs)
	}
}

func TestPlanRefusesCollisionBeforeMutation(t *testing.T) {
	files := plainFixtureFiles()
	files["home/.pi/agent/extensions/turn-bell.ts"] = "someone else's\n"
	dir := writePack(t, plainFixture, files)
	snapshot := treeSnapshot(t, dir)
	_, err := Plan(dir, Options{})
	var collision *CollisionError
	if !errors.As(err, &collision) {
		t.Fatalf("Plan with a taken destination = %v, want a CollisionError", err)
	}
	if len(collision.Collisions) != 1 || collision.Collisions[0].Path != "home/.pi/agent/extensions/turn-bell.ts" ||
		!strings.Contains(err.Error(), "yolo pack migrate "+dir) {
		t.Errorf("collision report = %v", err)
	}
	if !equalSnapshots(snapshot, treeSnapshot(t, dir)) {
		t.Fatal("a refused plan changed the pack")
	}

	// A destination taken between Plan and Apply is refused by Apply, with nothing written.
	clean := writePack(t, plainFixture, plainFixtureFiles())
	m, err := Plan(clean, Options{})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	taken := filepath.Join(clean, "home", ".pi", "agent", "extensions", "turn-bell.ts")
	if err := os.MkdirAll(filepath.Dir(taken), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(taken, []byte("late\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	snapshot = treeSnapshot(t, clean)
	if err := Apply(m); !errors.As(err, &collision) {
		t.Fatalf("Apply onto a destination taken after planning = %v, want a CollisionError", err)
	}
	if !equalSnapshots(snapshot, treeSnapshot(t, clean)) {
		t.Fatal("a refused apply changed the pack")
	}
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	full := append([]string{"-C", dir, "-c", "user.name=fixture", "-c", "user.email=fixture@example.test", "-c", "commit.gpgsign=false"}, args...)
	out, err := exec.Command("git", full...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func TestApplyUsesGitMvInACheckout(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	files := plainFixtureFiles()
	dir := writePack(t, plainFixture, files)
	git(t, dir, "init", "-q")
	// Every fixture file is tracked except one copied file, which must still move.
	if err := os.Remove(filepath.Join(dir, "ext", "board", "index.ts")); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-q", "-m", "fixture")
	if err := os.WriteFile(filepath.Join(dir, "ext", "board", "index.ts"), []byte("untracked\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	m, err := Plan(dir, Options{})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if err := Apply(m); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	status := git(t, dir, "status", "--porcelain=v1", "--untracked-files=all")
	for _, want := range []string{
		"R  bin/finder.sh -> home/.claude/bin/finder.sh",
		"R  ext/turn-bell.ts -> home/.pi/agent/extensions/turn-bell.ts",
		"?? home/.pi/agent/extensions/board/index.ts",
	} {
		if !strings.Contains(status, want+"\n") {
			t.Errorf("git status lacks %q:\n%s", want, status)
		}
	}
	// pack.json left the index by git mv and pack.toml entered it; its new bytes are an
	// unstaged modification of the moved file.
	index := git(t, dir, "ls-files", "--", packdecl.ManifestLegacyJSONName, packdecl.ManifestTOMLName)
	if strings.TrimSpace(index) != packdecl.ManifestTOMLName {
		t.Errorf("index after migration holds %q, want only pack.toml", index)
	}
	if strings.Contains(status, "?? "+packdecl.ManifestTOMLName) {
		t.Errorf("pack.toml was written beside the checkout rather than moved by git mv:\n%s", status)
	}
}

const postureFixture = `{
  "name": "fixture-pack",
  "contributes": [
    { "kind": "files", "from": "bin/run-fixture", "into": ".local/share/fixture/run-fixture" },
    { "kind": "autonomy",
      "autonomous": { "launch": [ { "bin": "fixture-tool", "flags": ["--yolo"] } ] },
      "guarded": { "launch": [ { "bin": "fixture-tool", "flags": ["--ask"] } ] } },
    { "kind": "config-overlay", "surface": "fixture-tool/settings",
      "config": { "managed": { "permissions": { "allow": ["Read"] } } } },
    { "kind": "config-overlay", "surface": "fixture-tool/settings",
      "config": { "managed": { "theme": "dark" } } }
  ]
}
`

func TestParityIncludesModesAndPosture(t *testing.T) {
	dir := writePack(t, postureFixture, map[string]string{"bin/run-fixture*": "#!/bin/sh\nexit 0\n"})
	m, err := Plan(dir, Options{})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	text := string(m.Manifest)
	// The legacy launch rows replace earlier packs' rows and the legacy overlay's array
	// replaces; the grouped defaults would append, so the migration says replace where it
	// matters and nowhere else.
	if strings.Count(text, "replace = true") != 2 || !strings.Contains(text, "[[autonomy]]\nautonomous = { launch = ") ||
		strings.Contains(text, "theme = \"dark\"\nreplace") {
		t.Fatalf("replace placement in:\n%s", text)
	}
	old, _ := packdecl.Decode([]byte(postureFixture))
	old.Contributes = old.Contributes[1:]
	without, problems := packdecl.DecodeTOML([]byte(strings.ReplaceAll(text, "replace = true\n", "")))
	if len(problems) != 0 {
		t.Fatal(problems)
	}
	diffs := strings.Join(ManifestDifferences(old, without), "\n")
	if !strings.Contains(diffs, "launch row 0 for \"fixture-tool\"") || !strings.Contains(diffs, "overlay fixture-tool/settings row 0") || strings.Contains(diffs, "row 1:") {
		t.Errorf("dropping replace = true was not caught as a posture/overlay difference:\n%s", diffs)
	}
	with, _ := packdecl.DecodeTOML(m.Manifest)
	if diffs := ManifestDifferences(old, with); len(diffs) != 0 {
		t.Errorf("planned manifest differs: %v", diffs)
	}
	for _, enabled := range []bool{true, false} {
		posture := with.PostureFor(enabled)
		if posture == nil || len(posture.Launch) != 1 || !posture.Launch[0].Replace {
			t.Errorf("PostureFor(%t) = %#v, want one replacing launch row", enabled, posture)
		}
	}

	if err := Apply(m); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	info, err := os.Stat(filepath.Join(dir, "home", ".local", "share", "fixture", "run-fixture"))
	if err != nil || info.Mode().Perm() != 0o755 {
		t.Fatalf("moved script mode = %v (err %v), want 0755", info, err)
	}
	pack, problems := packload.LoadDir(dir, "")
	if len(problems) != 0 {
		t.Fatal(problems)
	}
	delivered, err := Delivery(pack)
	if err != nil {
		t.Fatal(err)
	}
	if diffs := DeliveryDifferences(m.delivery, delivered); len(diffs) != 0 {
		t.Errorf("delivery after migration differs: %v", diffs)
	}
	// The comparison sees a dropped executable bit.
	if err := os.Chmod(filepath.Join(dir, "home", ".local", "share", "fixture", "run-fixture"), 0o644); err != nil {
		t.Fatal(err)
	}
	chmodded, err := Delivery(pack)
	if err != nil {
		t.Fatal(err)
	}
	if diffs := DeliveryDifferences(m.delivery, chmodded); len(diffs) != 1 || !strings.Contains(diffs[0], "changes mode") {
		t.Errorf("a dropped exec bit is not a delivery difference: %v", diffs)
	}
}
