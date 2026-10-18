package run

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/entrypoint"
	"github.com/mschulkind-oss/yolo-jail/internal/jailcontent"
	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
	"github.com/mschulkind-oss/yolo-jail/internal/macosuser"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
)

// overlayLaunch runs one macos-user launch through Run and returns the home overlay it handed the
// backend.
func overlayLaunch(t *testing.T, ws string) macosuser.HomeOverlay {
	t.Helper()
	var stdout, stderr bytes.Buffer
	o := dispatchOptions(t, ws, "macos-user", &stdout, &stderr, nil)
	var got macosuser.HomeOverlay
	o.MacosUserRun = func(_ *jsonx.OrderedMap, _ string, _, _ []string, _, _ string, homeOverlay macosuser.HomeOverlay, _ macosuser.HostContext, _ bool, _ *jsonx.OrderedMap, _ []packload.BlockedTool, _ macosuser.JailDaemons) int {
		got = homeOverlay
		return 0
	}
	if rc := Run(*o); rc != 0 || got.Tree == "" {
		t.Fatalf("Run() = %d, overlay %q\nstdout:\n%s\nstderr:\n%s", rc, got.Tree, stdout.String(), stderr.String())
	}
	return got
}

func overlayFileInfo(t *testing.T, p string) os.FileInfo {
	t.Helper()
	fi, err := os.Lstat(p)
	if err != nil {
		t.Fatal(err)
	}
	return fi
}

// AN UNCHANGED RELAUNCH REWRITES NO OVERLAY FILE, through Run's own call: the second launch hands
// the backend the overlay the first built, every file the same inode, the destination list
// included. A changed input — here a workspace skill's bytes — rebuilds it, and the new content is
// what is delivered.
func TestAnUnchangedMacosUserRelaunchReusesTheHomeOverlay(t *testing.T) {
	home := packHome(t)
	writeUserPacks(t, home, `["pi"]`)
	ws, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	wsWrite(t, ws, ".claude/skills/review/SKILL.md", "---\nname: review\n---\nv1\n")

	first := overlayLaunch(t, ws)
	skill := filepath.Join(first.Tree, ".pi", "agent", "skills", "review", "SKILL.md")
	list := filepath.Join(first.Tree, entrypoint.HomeOverlayManifestName)
	skillBefore, listBefore := overlayFileInfo(t, skill), overlayFileInfo(t, list)
	before, _, ok := entrypoint.HomeOverlayDigest(first.Tree)
	if !ok {
		t.Fatal("the built overlay carries no digest")
	}

	second := overlayLaunch(t, ws)
	if second.Tree != first.Tree {
		t.Fatalf("the relaunch's overlay is %q, the first's %q", second.Tree, first.Tree)
	}
	if !os.SameFile(skillBefore, overlayFileInfo(t, skill)) || !os.SameFile(listBefore, overlayFileInfo(t, list)) {
		t.Fatal("an unchanged relaunch rewrote the home overlay")
	}
	if len(second.Dests) == 0 || len(second.Dests) != len(first.Dests) {
		t.Fatalf("the reused overlay's destinations %v differ from the built one's %v", second.Dests, first.Dests)
	}
	for i := range first.Dests {
		if first.Dests[i] != second.Dests[i] {
			t.Fatalf("the reused overlay's destinations %v differ from the built one's %v", second.Dests, first.Dests)
		}
	}

	wsWrite(t, ws, ".claude/skills/review/SKILL.md", "---\nname: review\n---\nv2\n")
	third := overlayLaunch(t, ws)
	body, err := os.ReadFile(filepath.Join(third.Tree, ".pi", "agent", "skills", "review", "SKILL.md"))
	if err != nil || string(body) != "---\nname: review\n---\nv2\n" {
		t.Fatalf("a changed skill was not re-delivered: %q, %v", body, err)
	}
	if after, _, ok := entrypoint.HomeOverlayDigest(third.Tree); !ok || after == before {
		t.Fatalf("a changed input left the overlay's digest at %q (now %q, ok=%v)", before, after, ok)
	}
}

// THE DIGEST FOLLOWS EVERY INPUT the builder writes from, and an overlay is rebuilt when any one
// moves: a destination's bytes, a file's mode, a link's text, a destination added or dropped. And a
// patched extension's tree stands in by its identity, so its bytes are not what decides.
func TestTheHomeOverlayIsRebuiltWhenAnyInputChanges(t *testing.T) {
	staging := t.TempDir()
	skillsDir := filepath.Join(staging, "skills-a")
	write := func(rel, body string, mode os.FileMode) {
		t.Helper()
		p := filepath.Join(skillsDir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), mode); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(p, mode); err != nil {
			t.Fatal(err)
		}
	}
	write("x/SKILL.md", "one", 0o644)
	tree := filepath.Join(t.TempDir(), "patched")
	if err := os.MkdirAll(filepath.Join(tree, "node_modules", "dep"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tree, "node_modules", "dep", "index.js"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	skills := []jailcontent.SkillTarget{{Staging: "skills-a", Dest: ".pi/agent/skills"}}
	files := []packFilesTarget{{Pack: "pi", Src: tree, Dest: ".pi/agent/extensions/ext", Tree: "pi/ext"}}
	ids := map[string]string{"pi/ext": "capture-entry:k1"}
	build := func() string {
		t.Helper()
		overlay, _, err := buildMacosHomeOverlayTreeIDs(staging, skills, nil, files, nil, ids, nil)
		if err != nil {
			t.Fatal(err)
		}
		d, _, ok := entrypoint.HomeOverlayDigest(overlay)
		if !ok {
			t.Fatal("the built overlay carries no digest")
		}
		return d
	}
	seen := map[string]string{}
	step := func(what string) {
		t.Helper()
		d := build()
		for prev, pd := range seen {
			if pd == d {
				t.Fatalf("%s left the digest where %s had it", what, prev)
			}
		}
		seen[what] = d
		if again := build(); again != d {
			t.Fatalf("after %s, an unchanged rebuild moved the digest", what)
		}
	}
	step("the first build")
	write("x/SKILL.md", "two", 0o644)
	step("a skill's bytes")
	write("x/SKILL.md", "two", 0o755)
	step("a skill's exec bit")
	if err := os.Symlink("SKILL.md", filepath.Join(skillsDir, "x", "alias")); err != nil {
		t.Fatal(err)
	}
	step("a link added")
	if err := os.Remove(filepath.Join(skillsDir, "x", "alias")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("other", filepath.Join(skillsDir, "x", "alias")); err != nil {
		t.Fatal(err)
	}
	step("a link's text")
	ids["pi/ext"] = "capture-entry:k2"
	step("a patched tree's entry")
	// THE IDENTITY STANDS FOR THE BYTES: changing the copy's content under the same entry does not
	// move the digest — the entry is content-addressed, so that cannot happen to a real copy.
	if err := os.WriteFile(filepath.Join(tree, "node_modules", "dep", "index.js"), []byte("b"), 0o644); err != nil {
		t.Fatal(err)
	}
	if d := build(); d != seen["a patched tree's entry"] {
		t.Fatal("a patched tree's bytes were read although its identity was given")
	}
	files = nil
	step("a destination dropped")
}
