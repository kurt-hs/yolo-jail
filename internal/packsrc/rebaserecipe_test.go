package packsrc

// rebaserecipe_test.go runs the REBASE RECIPE (rebaserecipe.go) as printed, in a shell, against the
// replay fixture's upstream: the commands stop at the conflict, the export pasted mid-rebase changes
// nothing, and once the conflict is resolved the export replaces the series with one based on the
// target, which the replay then takes.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// recipeShell runs lines (the recipe's commands, prose lines left out) in one bash in dir, and
// returns the output and whether the last command succeeded.
func recipeShell(t *testing.T, dir string, lines ...string) (string, bool) {
	t.Helper()
	cmd := exec.Command("bash", "-c", strings.Join(lines, "\n"))
	cmd.Dir = dir
	cmd.Env = append(gitTestEnv(), "GIT_EDITOR=true")
	out, err := cmd.CombinedOutput()
	return string(out), err == nil
}

// commands is the recipe's command lines, trimmed, its prose left out.
func commands(lines []string) []string {
	var out []string
	for _, l := range lines {
		if strings.HasPrefix(l, "  ") {
			out = append(out, strings.TrimSpace(l))
		}
	}
	return out
}

func TestTheRebaseRecipeRunsAsPrintedAndItsExportWaitsForTheRebase(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}
	u := newPatchedUpstream(t)
	v11 := u.release(t, "v1.1.0", map[int]string{10: "TEN"})
	patches := filepath.Join(u.pack, "patches")
	r := RebaseRecipe{Repo: u.repo, Base: u.base, Patches: patches, Dir: "fork-rebase"}
	target := ListEntry{Commit: v11, Tag: "v1.1.0"}
	lines := r.Lines(target)
	cmds := commands(lines)
	if len(cmds) != 4 {
		t.Fatalf("want four command lines, got:\n%s", strings.Join(lines, "\n"))
	}
	for _, w := range []string{"git rebase " + v11, "--base=" + v11, "format.noprefix=false", "--no-signature",
		"--suffix=.patch", "mkdir " + patches + ".new", "rm -r " + patches + ".old"} {
		if !strings.Contains(strings.Join(lines, "\n"), w) {
			t.Errorf("the recipe lacks %q:\n%s", w, strings.Join(lines, "\n"))
		}
	}
	work := t.TempDir()
	before, _ := os.ReadDir(patches)

	// THE STEPS, which stop at the first member's conflict, then THE EXPORT pasted mid-rebase.
	if out, ok := recipeShell(t, work, cmds[0], cmds[1], cmds[2]); ok {
		t.Fatalf("the rebase did not stop at the conflict:\n%s", out)
	}
	clone := filepath.Join(work, "fork-rebase")
	if out, ok := recipeShell(t, clone, cmds[3]); ok {
		t.Fatalf("the export ran mid-rebase:\n%s", out)
	}
	if after, _ := os.ReadDir(patches); len(after) != len(before) {
		t.Fatalf("the export mid-rebase changed the series: %d files, was %d", len(after), len(before))
	}
	mustNotExist(t, patches+".new", "the export mid-rebase made it")

	// RESOLVED, the export replaces the series with one based on the target.
	if err := os.WriteFile(filepath.Join(clone, "f.txt"), []byte(thirtyLines(map[int]string{10: "ten"})), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, ok := recipeShell(t, clone, "git add f.txt && git rebase --continue", cmds[3]); !ok {
		t.Fatalf("the export after the rebase failed:\n%s", out)
	}
	mustNotExist(t, patches+".new", "the export left it")
	mustNotExist(t, patches+".old", "the export left it")
	series := u.series(t)
	if series.Base != v11 || series.Len() != 2 {
		t.Fatalf("the exported series has base %s and %d members, want v1.1.0 and 2", series.Base, series.Len())
	}
	_, list := u.checked(t)
	w := u.store.WalkSeries(mustAddr(t, u.source("main")).Repo, "", series, list[:1], WalkOptions{})
	if w.Fit != 0 {
		t.Errorf("the replay does not take the exported series at v1.1.0: %+v", w)
	}
}

// A SERIES NOT THE USER'S TO WRITE (a fetched pack's) prints a placeholder and says where it is.
func TestTheRebaseRecipeNamesAFetchedPacksSeries(t *testing.T) {
	r := RebaseRecipe{Repo: "https://example.com/up.git", Base: "b", Pack: "forkpack", Rel: "patches", Dir: "d"}
	got := strings.Join(r.Lines(ListEntry{Commit: "c"}), "\n")
	for _, w := range []string{"pack forkpack is not yours to write here, so <patches> below is its series directory (patches)",
		"git am <patches>/*.patch", "mkdir <patches>.new", "commit and push it after"} {
		if !strings.Contains(got, w) {
			t.Errorf("the recipe lacks %q:\n%s", w, got)
		}
	}
}
