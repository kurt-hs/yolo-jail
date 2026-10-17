package integration

// workspaceskills_test.go is the workspace skills layer (docs/reference/agent-briefings.md) end to
// end in a real jail: a repo's committed skills reach the agents that do not read them natively,
// at their :ro home-scope dirs; the agent that does read them gets no second copy; a committed
// link to a host file outside the workspace delivers nothing and is named at launch; and the
// launch writes nothing into the repo. The unit halves (internal/jailcontent,
// internal/cli/run) pin the reader and the call site; this pins that the staged dirs are the
// ones the jail binds.

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/jailcontent"
)

// wsSkillsTree writes a repo's skills — `.claude/skills/review/` — and, in an owned
// sibling temp dir, a public marker the repo links to from `.agents/skills/x/SKILL.md`.
func wsSkillsTree(t *testing.T, ws, nonce string) {
	t.Helper()
	outside := resolvedTempDir(t)
	marker := filepath.Join(outside, "public-skill-marker")
	if err := os.WriteFile(marker, []byte("PUBLIC-"+nonce+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if rel, err := filepath.Rel(ws, marker); err != nil {
		t.Fatalf("checking sibling marker is outside workspace: %v", err)
	} else if rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))) {
		t.Fatalf("public marker must be outside workspace: workspace %q, marker %q", ws, marker)
	}

	path := filepath.Join(ws, ".claude", "skills", "review", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("---\nname: review\n---\nREVIEW-"+nonce+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	link := filepath.Join(ws, ".agents", "skills", "x", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	relativeTarget, err := filepath.Rel(filepath.Dir(link), marker)
	if err != nil {
		t.Fatalf("making relative escape to public marker: %v", err)
	}
	if err := os.Symlink(relativeTarget, link); err != nil {
		t.Fatal(err)
	}

	// This target is lexically inside the workspace but does not exist. It must be refused
	// as dangling, independently of the outside-workspace link above. It is spelled through
	// the workspace's REAL path: the harness's explicit Env leaves the launcher no PWD, so its
	// root is getcwd's real path, and the reader (which never lstats outside the workspace)
	// cannot know that macOS's /var/folders/… spelling of t.TempDir() names the same tree.
	realWS, err := filepath.EvalSymlinks(ws)
	if err != nil {
		t.Fatal(err)
	}
	dangling := filepath.Join(ws, ".agents", "skills", "dangling", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(dangling), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(realWS, "missing", "SKILL.md"), dangling); err != nil {
		t.Fatal(err)
	}
}

func wsListing(t *testing.T, root string) string {
	t.Helper()
	var b strings.Builder
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		if rel == ".yolo" || strings.HasPrefix(rel, ".yolo"+string(filepath.Separator)) {
			return filepath.SkipDir // yolo's own state dir, written by every launch by design
		}
		// podman creates the per-side shadow mountpoints in a live bind of the workspace
		// (mounts.go says why they are left there); they are not this layer's.
		if rel == "node_modules" || rel == ".venv" {
			return filepath.SkipDir
		}
		fi, _ := os.Lstat(p)
		b.WriteString(rel + " " + fi.Mode().String() + "\n")
		return nil
	})
	return b.String()
}

func TestWorkspaceSkillsReachAContainerJail(t *testing.T) {
	requireJail(t)
	nonce := acParityNonce()
	dir := writeProject(t, `{}`)
	packHome(t, `{"packs": ["claude", "codex"]}`)
	wsSkillsTree(t, dir, nonce)
	aliasTarget := filepath.Join(dir, "docs", "alias.md")
	if err := os.MkdirAll(filepath.Dir(aliasTarget), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(aliasTarget, []byte("ALIAS-"+nonce+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	aliasLink := filepath.Join(dir, ".agents", "skills", "alias", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(aliasLink), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/workspace/docs/alias.md", aliasLink); err != nil {
		t.Fatal(err)
	}
	before := wsListing(t, dir)

	r := runYolo(t, dir, strings.Join([]string{
		`grep -q REVIEW-` + nonce + ` /home/agent/.codex/skills/review/SKILL.md`,
		`grep -q ALIAS-` + nonce + ` /home/agent/.claude/skills/alias/SKILL.md`,
		`! test -e /home/agent/.claude/skills/review`,
		`! test -e /home/agent/.codex/skills/alias`,
		`! test -e /home/agent/.codex/skills/x`,
		`! test -e /home/agent/.claude/skills/x`,
		`! test -e /home/agent/.codex/skills/dangling`,
		`! test -e /home/agent/.claude/skills/dangling`,
		`! grep -rq PUBLIC-` + nonce + ` /home/agent/.codex/skills /home/agent/.claude/skills`,
		`echo WS_SKILLS_OK`,
	}, " && "))
	if r.rc != 0 || !strings.Contains(r.stdout, "WS_SKILLS_OK") {
		t.Fatalf("the workspace layer did not reach the jail as ruled: codex should hold review, "+
			"claude (which reads .claude/skills natively) should not, and no skill named x — whose "+
			"only file is a link out of the workspace — should exist anywhere.\nrc %d\nstdout: %s\nstderr: %s", r.rc, r.stdout, r.stderr)
	}
	for _, want := range []string{
		"Workspace skills: refused .agents/skills/x/SKILL.md — a symlink that resolves outside the workspace; nothing it points at was read",
		"Workspace skills: refused .agents/skills/dangling/SKILL.md — a dangling symlink",
		"Workspace skills from .claude/skills mirrored into codex: review",
		"Workspace skills from .agents/skills mirrored into claude: alias",
	} {
		if !strings.Contains(r.combined(), want) {
			t.Errorf("the launch must say %q:\n%s", want, r.combined())
		}
	}
	if after := wsListing(t, dir); after != before {
		t.Errorf("the launch wrote into the workspace:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

// macos-user receives the layer through the same composed tree its sidecar already copies.
func TestMacosUserWorkspaceSkillsArriveThroughTheComposedTree(t *testing.T) {
	requireMacosUser(t)
	nonce := acParityNonce()
	packHome(t, `{"packs": ["pi"]}`)
	ws := macosUserWorkspace(t, `{}`)
	wsSkillsTree(t, ws, nonce)

	r := macosUserRunProbe(t, "workspace-skills", ws, strings.Join([]string{
		`echo "=== REVIEW ==="; cat ~/.pi/agent/skills/review/SKILL.md 2>&1`,
		`echo "=== X ==="; cat ~/.pi/agent/skills/x/SKILL.md 2>&1`,
		`echo "=== DANGLING ==="; cat ~/.pi/agent/skills/dangling/SKILL.md 2>&1`,
		`echo "=== END ==="`,
	}, "\n"))
	if got := section(r.stdout, "=== REVIEW ===", "=== X ==="); !strings.Contains(got, "REVIEW-"+nonce) {
		t.Errorf("the repo's .claude/skills/review never reached pi's home on macos-user:\n%s", got)
	}
	if got := section(r.stdout, "=== X ===", "=== DANGLING ==="); strings.Contains(got, "PUBLIC-") {
		t.Errorf("a public file outside the workspace reached the sandbox as a skill:\n%s", got)
	}
	if got := section(r.stdout, "=== DANGLING ===", "=== END ==="); strings.Contains(got, "PUBLIC-") {
		t.Errorf("a dangling in-workspace link delivered the outside marker:\n%s", got)
	}
	for _, want := range []string{
		"Workspace skills: refused .agents/skills/x/SKILL.md — a symlink that resolves outside the workspace; nothing it points at was read",
		"Workspace skills: refused .agents/skills/dangling/SKILL.md — a dangling symlink",
	} {
		if !strings.Contains(r.combined(), want) {
			t.Errorf("the launch must name %q:\n%s", want, r.combined())
		}
	}
}

// THE DARWIN PATH-RESOLUTION CLASS, for wsSkillsTree's dangling link, reproduced on Linux with
// no container, so it runs under -short. The harness launches yolo with an explicit Env, which
// os/exec takes as license NOT to set PWD to cmd.Dir, so the launcher's os.Getwd falls back to
// getcwd and its workspace root is the REAL path. On macOS t.TempDir() sits under /var, a link
// to /private/var, and an absolute link spelled through /var names a path under no spelling of
// that root: the reader refuses it, by design without an lstat outside the workspace, as one
// that resolves outside — and the launch never says "a dangling symlink" (the macOS nightly's
// TestWorkspaceSkillsReachAContainerJail failure).
func TestWsSkillsTreeDanglingLinkIsDanglingUnderASymlinkedTempDir(t *testing.T) {
	base := resolvedTempDir(t)
	real := filepath.Join(base, "real")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("cannot create a symlink here (%v); the class is unreproducible on this filesystem", err)
	}
	t.Setenv("TMPDIR", link)

	// A subtest, because t.TempDir() reuses the parent dir this test already made under the
	// old TMPDIR.
	t.Run("fixture", func(t *testing.T) {
		ws := t.TempDir()
		root, err := filepath.EvalSymlinks(ws)
		if err != nil {
			t.Fatal(err)
		}
		if root == ws {
			t.Skipf("t.TempDir() came back already resolved (%s), so the class cannot show here", ws)
		}
		wsSkillsTree(t, ws, acParityNonce())

		check, err := jailcontent.CheckWorkspaceSkillSource(root, filepath.Join(".agents", "skills"))
		if err != nil {
			t.Fatal(err)
		}
		want := jailcontent.WorkspaceSkillRefusal{Path: ".agents/skills/dangling/SKILL.md", Reason: "a dangling symlink"}
		for _, r := range check.Refused {
			if r == want {
				return
			}
		}
		t.Errorf("with the workspace root at its real path %s, the fixture's dangling link must be "+
			"refused as %q; the reader refused %+v", root, want.Reason, check.Refused)
	})
}
