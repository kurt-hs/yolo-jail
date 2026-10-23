package entrypoint

// hostfilesretire_test.go pins that the host `files` render CONVERGES: a path a pack's render
// wrote on an earlier apply, and which the pack no longer ships (removed, renamed, or a
// directory contribution whose `into` moved), leaves the home on the next apply — while a file
// yolo did not write, and a file yolo wrote that the user has since changed, stays.
//
// The field case (dotfiles §25): a pack shipped two library modules into pi's load-all
// `~/.pi/agent/extensions/`, then moved them under `extensions/board/`. The render wrote the new
// set and left the old two, and pi then refused every launch over a file no pack shipped.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/packdecl"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
	"github.com/mschulkind-oss/yolo-jail/internal/shquote"
)

// filesPackOf builds a pack with the given source tree (pack-root-relative path -> body) and
// `files` contributions (from -> into). The same root can be re-declared by calling again with
// a different contribution list, which is how a test models the pack's next version.
func filesPackOf(t *testing.T, root, name string, tree map[string]string, contribs [][2]string) *packload.Pack {
	t.Helper()
	for rel, body := range tree {
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var cs []packdecl.Contribution
	for _, c := range contribs {
		cs = append(cs, packdecl.Contribution{Kind: packdecl.KindFiles, From: c[0], Into: c[1]})
	}
	return &packload.Pack{Name: name, Root: root, Decl: &packdecl.Manifest{Contributes: cs}}
}

const extDir = ".pi/agent/extensions"

// v1 ships one file at the top of the load-all dir and one directory beside it.
func retireV1(t *testing.T, root string) *packload.Pack {
	return filesPackOf(t, root, "matt", map[string]string{
		"files/a.ts":            "export const a = 1\n",
		"files/board/client.ts": "client\n",
		"files/board/pile.ts":   "pile\n",
	}, [][2]string{
		{"files/a.ts", extDir + "/a.ts"},
		{"files/board", extDir + "/old"},
	})
}

// v2 renames the file and moves the directory contribution's `into`.
func retireV2(t *testing.T, root string) *packload.Pack {
	return filesPackOf(t, root, "matt", map[string]string{
		"files/b.ts": "export const b = 1\n",
	}, [][2]string{
		{"files/b.ts", extDir + "/b.ts"},
		{"files/board", extDir + "/new"},
	})
}

func mustRender(t *testing.T, p *packload.Pack, home string, req HostFilesRequest, observe bool) []HostRenderResult {
	t.Helper()
	results, err := RenderHostFiles(p, home, req, observe)
	if err != nil {
		t.Fatal(err)
	}
	return results
}

func filesResultFor(results []HostRenderResult, path string) (HostRenderResult, bool) {
	for _, r := range results {
		if r.Path == path {
			return r, true
		}
	}
	return HostRenderResult{}, false
}

func assertGone(t *testing.T, path, why string) {
	t.Helper()
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Errorf("%s is still in the home (%v): %s", path, err, why)
	}
}

func assertBody(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	if string(got) != want {
		t.Errorf("%s = %q, want %q", path, got, want)
	}
}

// THE HEADLINE: rename a file and move a directory contribution, and the old copies leave the
// home, the new ones land, and a neighbouring file the user put there is untouched.
func TestHostFilesRetiresWhatThePackStoppedShipping(t *testing.T) {
	home, root := t.TempDir(), t.TempDir()
	req := filesReq(t)
	ext := filepath.Join(home, filepath.FromSlash(extDir))
	user := filepath.Join(ext, "user.ts")
	if err := os.MkdirAll(ext, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(user, []byte("mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	mustRender(t, retireV1(t, root), home, req, false)
	assertBody(t, filepath.Join(ext, "a.ts"), "export const a = 1\n")
	assertBody(t, filepath.Join(ext, "old", "client.ts"), "client\n")

	if err := os.Remove(filepath.Join(root, "files", "a.ts")); err != nil {
		t.Fatal(err)
	}
	results := mustRender(t, retireV2(t, root), home, req, false)

	assertGone(t, filepath.Join(ext, "a.ts"), "the pack renamed it to b.ts")
	assertGone(t, filepath.Join(ext, "old", "client.ts"), "the directory contribution moved to new/")
	assertGone(t, filepath.Join(ext, "old", "pile.ts"), "the directory contribution moved to new/")
	assertGone(t, filepath.Join(ext, "old"), "an emptied old directory location is not left behind")
	assertBody(t, filepath.Join(ext, "b.ts"), "export const b = 1\n")
	assertBody(t, filepath.Join(ext, "new", "client.ts"), "client\n")
	assertBody(t, user, "mine\n")
	if fi, err := os.Stat(ext); err != nil || !fi.IsDir() {
		t.Errorf("the extensions directory itself must stay: %v", err)
	}

	r, ok := filesResultFor(results, filepath.Join(ext, "a.ts"))
	if !ok || !strings.HasPrefix(r.Action, "removed") || !r.WouldChange {
		t.Errorf("the removal must be disclosed as one line, got %+v (found=%v)", r, ok)
	}
	if req.Manifest.OwnedBy(filepath.Join(ext, "a.ts"), "matt") {
		t.Error("a removed path must be forgotten by the ownership record")
	}

	// And it converges: a third apply has nothing to retire.
	for _, r := range mustRender(t, retireV2(t, root), home, req, false) {
		if r.Action != "unchanged" {
			t.Errorf("a repeated apply should be all-unchanged, got %+v", r)
		}
	}
}

// A file yolo wrote that the user has since changed is user state: it is left where it is, and
// the line names it, the pack that used to ship it, and the command that removes it.
func TestHostFilesRetireLeavesAChangedOrphan(t *testing.T) {
	home, root := t.TempDir(), t.TempDir()
	req := filesReq(t)
	a := filepath.Join(home, filepath.FromSlash(extDir), "a.ts")

	mustRender(t, retireV1(t, root), home, req, false)
	if err := os.Chmod(a, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(a, []byte("my edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	results := mustRender(t, retireV2(t, root), home, req, false)
	assertBody(t, a, "my edit\n")
	r, ok := filesResultFor(results, a)
	if !ok {
		t.Fatalf("the changed orphan must be reported, got %+v", results)
	}
	for _, want := range []string{"left in place", `"matt"`, "rm " + shquote.Quote(a)} {
		if !strings.Contains(r.Action, want) {
			t.Errorf("the line must contain %q, got %q", want, r.Action)
		}
	}
	if r.WouldChange {
		t.Error("leaving a file alone is not a pending change")
	}
}

// THE UPGRADE CASE: a path recorded by a yolo that kept no content digest. The record proves
// yolo wrote it; whether the user changed it since cannot be proven, so it is MOVED to the
// archive (one `mv` back) while it still carries the read-only mode the render gave it, and left
// in place once that mode has been changed.
func TestHostFilesRetireArchivesAnOrphanRecordedBeforeDigests(t *testing.T) {
	home, root := t.TempDir(), t.TempDir()
	req := filesReq(t)
	ext := filepath.Join(home, filepath.FromSlash(extDir))
	a := filepath.Join(ext, "a.ts")
	client := filepath.Join(ext, "old", "client.ts")

	mustRender(t, retireV1(t, root), home, req, false)
	req.Manifest.Digests = nil // the record an older yolo wrote
	if err := os.Chmod(client, 0o644); err != nil {
		t.Fatal(err)
	}

	results := mustRender(t, retireV2(t, root), home, req, false)
	assertGone(t, a, "a recorded, still read-only orphan is archived")
	r, _ := filesResultFor(results, a)
	if !strings.Contains(r.Action, "archived to") {
		t.Errorf("the line must say where it went, got %q", r.Action)
	}
	archived := filepath.Join(string(req.ArchiveRoot), req.Stamp, "matt", "a.ts")
	assertBody(t, archived, "export const a = 1\n")

	assertBody(t, client, "client\n")
	r, _ = filesResultFor(results, client)
	if !strings.Contains(r.Action, "left in place") || !strings.Contains(r.Action, "rm "+shquote.Quote(client)) {
		t.Errorf("an unprovable orphan whose mode changed must be left and named, got %q", r.Action)
	}
}

// A dry run predicts the retirement and moves nothing.
func TestHostFilesRetireObserveWritesNothing(t *testing.T) {
	home, root := t.TempDir(), t.TempDir()
	req := filesReq(t)
	a := filepath.Join(home, filepath.FromSlash(extDir), "a.ts")

	mustRender(t, retireV1(t, root), home, req, false)
	results := mustRender(t, retireV2(t, root), home, req, true)
	assertBody(t, a, "export const a = 1\n")
	r, ok := filesResultFor(results, a)
	if !ok || !strings.HasPrefix(r.Action, "would remove") || !r.WouldChange {
		t.Errorf("observe must predict the removal as a change, got %+v", r)
	}
	if !req.Manifest.OwnedBy(a, "matt") {
		t.Error("observe must not touch the ownership record")
	}
}

// What the pack still DECLARES is never retired, even when its source is missing this run (a
// filter or a typo — reported by the render as a pack problem), and a user's file inside an old
// directory location keeps that directory.
func TestHostFilesRetireSparesDeclaredAndUserPaths(t *testing.T) {
	home, root := t.TempDir(), t.TempDir()
	req := filesReq(t)
	ext := filepath.Join(home, filepath.FromSlash(extDir))

	mustRender(t, retireV1(t, root), home, req, false)
	mine := filepath.Join(ext, "old", "mine.ts")
	if err := os.WriteFile(mine, []byte("mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// The directory's source vanished, but the contribution is still declared.
	if err := os.RemoveAll(filepath.Join(root, "files", "board")); err != nil {
		t.Fatal(err)
	}
	mustRender(t, retireV1(t, root), home, req, false)
	assertBody(t, filepath.Join(ext, "old", "client.ts"), "client\n")

	// Now it is no longer declared: yolo's two files go, the user's stays, and so does old/.
	p := filesPackOf(t, root, "matt", nil, [][2]string{{"files/a.ts", extDir + "/a.ts"}})
	mustRender(t, p, home, req, false)
	assertGone(t, filepath.Join(ext, "old", "client.ts"), "no longer declared")
	assertBody(t, mine, "mine\n")
}

// A pack that stops declaring ANY `files` contribution still has its old output retired.
func TestHostFilesRetireCoversAPackWithNoFilesLeft(t *testing.T) {
	home, root := t.TempDir(), t.TempDir()
	req := filesReq(t)
	mustRender(t, retireV1(t, root), home, req, false)

	p := filesPackOf(t, root, "matt", nil, nil)
	mustRender(t, p, home, req, false)
	assertGone(t, filepath.Join(home, filepath.FromSlash(extDir), "a.ts"), "the pack ships no files")
	assertGone(t, filepath.Join(home, filepath.FromSlash(extDir), "old"), "the pack ships no files")
	// The sweep stops at the directory contribution's old root: the directories above it were
	// there before yolo (or are the agent's), however empty the retire left them.
	for _, dir := range []string{extDir, ".pi/agent", ".pi"} {
		if fi, err := os.Lstat(filepath.Join(home, filepath.FromSlash(dir))); err != nil || !fi.IsDir() {
			t.Errorf("~/%s was swept, but it is above every retired directory root: %v", dir, err)
		}
	}
}

// A single-file contribution's parent is never swept, even when the retire empties it: yolo
// cannot prove it created that directory (a user's ~/.config/userdir, here).
func TestHostFilesRetireNeverSweepsASingleFilesParent(t *testing.T) {
	home, root := t.TempDir(), t.TempDir()
	req := filesReq(t)
	userDir := filepath.Join(home, ".config", "userdir")
	if err := os.MkdirAll(userDir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filesPackOf(t, root, "matt", map[string]string{"files/x.conf": "x\n"},
		[][2]string{{"files/x.conf", ".config/userdir/x.conf"}})
	mustRender(t, p, home, req, false)

	mustRender(t, filesPackOf(t, root, "matt", nil, nil), home, req, false)
	assertGone(t, filepath.Join(userDir, "x.conf"), "the pack stopped shipping it")
	if fi, err := os.Lstat(userDir); err != nil || !fi.IsDir() {
		t.Errorf("the user's empty directory was swept: %v", err)
	}
}

// A directory contribution that becomes a FILE at the same `into` converges: the old tree and
// its now-empty directory go, and the file renders in their place.
func TestHostFilesRetireDirectoryBecomingAFile(t *testing.T) {
	home, root := t.TempDir(), t.TempDir()
	req := filesReq(t)
	into := ".x/board"
	mustRender(t, filesPackOf(t, root, "matt", map[string]string{"files/board/client.ts": "c\n"},
		[][2]string{{"files/board", into}}), home, req, false)

	p := filesPackOf(t, root, "matt", map[string]string{"files/board.ts": "file\n"},
		[][2]string{{"files/board.ts", into}})
	results := mustRender(t, p, home, req, false)
	assertBody(t, filepath.Join(home, filepath.FromSlash(into)), "file\n")
	if r, ok := filesResultFor(results, filepath.Join(home, filepath.FromSlash(into))); !ok || r.Action != "rendered" {
		t.Errorf("the file must render in the directory's place, got %+v", r)
	}
}

// The sweep never follows a link: a directory contribution whose old root the user made a
// symbolic link (into a dotfiles checkout) keeps the link after the retire empties its target.
func TestHostFilesRetireSweepNeverFollowsALink(t *testing.T) {
	home, root := t.TempDir(), t.TempDir()
	req := filesReq(t)
	target := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".x"), 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(home, ".x", "old")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	mustRender(t, filesPackOf(t, root, "matt", map[string]string{"files/board/client.ts": "c\n"},
		[][2]string{{"files/board", ".x/old"}}), home, req, false)
	assertBody(t, filepath.Join(target, "client.ts"), "c\n")

	mustRender(t, filesPackOf(t, root, "matt", nil, [][2]string{{"files/board", ".x/new"}}), home, req, false)
	assertGone(t, filepath.Join(target, "client.ts"), "yolo's unchanged file is retired through the link")
	if fi, err := os.Lstat(link); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Errorf("the user's link was swept: %v", err)
	}
}

// The sweep never removes a directory directly under the home, even when it is the retired
// directory contribution's own root.
func TestHostFilesRetireSweepStopsAtTheHomesChildren(t *testing.T) {
	home, root := t.TempDir(), t.TempDir()
	req := filesReq(t)
	mustRender(t, filesPackOf(t, root, "matt", map[string]string{"files/board/client.ts": "c\n"},
		[][2]string{{"files/board", ".tooldir"}}), home, req, false)

	mustRender(t, filesPackOf(t, root, "matt", nil, nil), home, req, false)
	assertGone(t, filepath.Join(home, ".tooldir", "client.ts"), "retired")
	if fi, err := os.Lstat(filepath.Join(home, ".tooldir")); err != nil || !fi.IsDir() {
		t.Errorf("a directory directly under the home was swept: %v", err)
	}
}

// An entry of the pack's under its SKILLS destination is not this kind's: a pre-composition
// skill shares the record, and the retire leaves it alone.
func TestHostFilesRetireSparesTheSkillsDestination(t *testing.T) {
	home, root := t.TempDir(), t.TempDir()
	req := filesReq(t)
	skill := filepath.Join(home, ".pi", "agent", "skills", "legacy.md")
	if err := os.MkdirAll(filepath.Dir(skill), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(skill, []byte("skill\n"), 0o444); err != nil {
		t.Fatal(err)
	}
	req.Manifest.Record(skill, "matt")
	p := filesPackOf(t, root, "matt", nil, nil)
	p.Decl.Contributes = append(p.Decl.Contributes,
		packdecl.Contribution{Kind: packdecl.KindSkills, From: "skills", Into: ".pi/agent/skills"})

	mustRender(t, p, home, req, false)
	assertBody(t, skill, "skill\n")
}

// An unchanged path an older yolo recorded without a digest gets one on the next apply, so a
// later retire REMOVES it as provably unchanged rather than archiving it as unprovable.
func TestHostFilesBackfillsTheDigestOfAnUnchangedPath(t *testing.T) {
	home, root := t.TempDir(), t.TempDir()
	req := filesReq(t)
	a := filepath.Join(home, filepath.FromSlash(extDir), "a.ts")
	client := filepath.Join(home, filepath.FromSlash(extDir), "old", "client.ts")
	mustRender(t, retireV1(t, root), home, req, false)
	req.Manifest.Digests, req.Manifest.Roots = nil, nil // the record an older yolo wrote

	mustRender(t, retireV1(t, root), home, req, false)
	if _, ok := req.Manifest.Digest(a); !ok {
		t.Fatal("an unchanged recorded path did not get its digest backfilled")
	}
	if _, ok := req.Manifest.Root(client); !ok {
		t.Fatal("an unchanged directory-contribution path did not get its root backfilled")
	}
	results := mustRender(t, retireV2(t, root), home, req, false)
	if r, _ := filesResultFor(results, a); r.Action != "removed (the pack no longer ships it)" {
		t.Errorf("with a backfilled digest the orphan is removed, not archived; got %q", r.Action)
	}
	assertGone(t, filepath.Join(home, filepath.FromSlash(extDir), "old"), "the backfilled root bounds the sweep")
}

// A recorded path the user replaced with something other than a regular file (a link, a
// directory) is theirs: left in place, and named with its rm command.
func TestHostFilesRetireLeavesAPathThatIsNoLongerAFile(t *testing.T) {
	home, root := t.TempDir(), t.TempDir()
	req := filesReq(t)
	a := filepath.Join(home, filepath.FromSlash(extDir), "a.ts")
	mustRender(t, retireV1(t, root), home, req, false)
	if err := os.Remove(a); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/dev/null", a); err != nil {
		t.Fatal(err)
	}

	results := mustRender(t, retireV2(t, root), home, req, false)
	if fi, err := os.Lstat(a); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Errorf("the user's link was touched: %v", err)
	}
	r, ok := filesResultFor(results, a)
	if !ok || !strings.HasPrefix(r.Action, HostFilesLeftAction) || !strings.Contains(r.Action, "rm "+shquote.Quote(a)) ||
		!r.Retire || r.WouldChange {
		t.Errorf("a replaced path must be left and named, got %+v", r)
	}
}

// Another pack's recorded path is not this pack's to retire.
func TestHostFilesRetireTouchesOnlyItsOwnPacksRecord(t *testing.T) {
	home, root := t.TempDir(), t.TempDir()
	req := filesReq(t)
	other := filepath.Join(home, filepath.FromSlash(extDir), "other.ts")
	if err := os.MkdirAll(filepath.Dir(other), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(other, []byte("other\n"), 0o444); err != nil {
		t.Fatal(err)
	}
	req.Manifest.Record(other, "someone-else")

	mustRender(t, retireV2(t, root), home, req, false)
	assertBody(t, other, "other\n")
}
