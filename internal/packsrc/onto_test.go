package packsrc

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/shquote"
)

// conflictFixture is the patched upstream with a v1.1.0 that edits line 11, between the series'
// two members' lines (§5.2's conflict row): the walk stops at the first member there.
func conflictFixture(t *testing.T) (u *patchedUpstream, v11 string, series *Series) {
	t.Helper()
	u = newPatchedUpstream(t)
	v11 = u.release(t, "v1.1.0", map[int]string{11: "eleven"})
	series, _ = u.checked(t)
	return u, v11, series
}

// wrappedGit is a git that runs body (shell) before it execs the real git with the same arguments.
// In body, $last is the run's last argument.
func wrappedGit(t *testing.T, body string) string {
	t.Helper()
	real, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not available")
	}
	bin := filepath.Join(t.TempDir(), "git")
	writeTestFile(t, bin, "#!/bin/sh\nfor last; do :; done\n"+body+"\nexec "+shquote.Quote(real)+" \"$@\"\n")
	if err := os.Chmod(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	return bin
}

// --onto RESOLVES IN THE MIRROR, with no network: a tag (named as one), a branch, an abbreviated
// commit, and refusals for what names nothing and for a flag.
func TestResolveOnto(t *testing.T) {
	u, v11, _ := conflictFixture(t)
	repo := mustAddr(t, u.source("main")).Repo
	if e, err := u.store.ResolveOnto(repo, "v1.1.0", nil); err != nil || e.Commit != v11 || e.Tag != "v1.1.0" {
		t.Errorf("v1.1.0 → %+v (%v)", e, err)
	}
	if e, err := u.store.ResolveOnto(repo, "main", nil); err != nil || e.Commit != v11 || e.Tag != "" {
		t.Errorf("main → %+v (%v)", e, err)
	}
	if e, err := u.store.ResolveOnto(repo, v11[:10], nil); err != nil || e.Commit != v11 {
		t.Errorf("an abbreviated commit → %+v (%v)", e, err)
	}
	if _, err := u.store.ResolveOnto(repo, "nope", nil); err == nil || !strings.Contains(err.Error(), "names no") {
		t.Errorf("a ref that names nothing: %v", err)
	}
	if _, err := u.store.ResolveOnto(repo, "--upload-pack=x", nil); err == nil {
		t.Error("a flag passed for a revision")
	}
}
