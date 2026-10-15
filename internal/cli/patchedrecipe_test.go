package cli

// patchedrecipe_test.go pins the REBASE RECIPE where a conflict is said (packsrc.RebaseRecipe;
// docs/design/patched-forks.md §8.4, PF-D85, which retired `yolo pack rebase`): the error block of
// `yolo pack update` prints the git commands with the commits filled in, and run as printed — the
// conflict resolved by hand where they stop — they replace the series with one the next fresh
// launch builds and moves the good build to (§14's done condition).

import (
	"crypto/sha256"
	"encoding/hex"
	"github.com/mschulkind-oss/yolo-jail/internal/shquote"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/packsrc"
	"github.com/mschulkind-oss/yolo-jail/internal/testsupport"
)

// treeDigest is a digest of every file under dir, by path and bytes, so a test can say a tree was
// not written.
func treeDigest(t *testing.T, dir string) string {
	t.Helper()
	h := sha256.New()
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		h.Write([]byte(rel + "\x00"))
		if d.Type().IsRegular() {
			data, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			h.Write(data)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// recipeCommands are the recipe's command lines in printed text: under a Repair line, the lines
// indented six spaces that start a git or test command.
func recipeCommands(printed string) []string {
	var cmds []string
	for _, line := range strings.Split(printed, "\n") {
		if c, ok := strings.CutPrefix(line, "      "); ok && (strings.HasPrefix(c, "git ") || strings.HasPrefix(c, "test ")) {
			cmds = append(cmds, c)
		}
	}
	return cmds
}

// pasted runs one printed command line in bash in dir, as a person pasting it would, with a hermetic
// git and no editor to wait on, and says whether it succeeded.
func pasted(t *testing.T, dir, line string) (string, bool) {
	t.Helper()
	cmd := exec.Command("bash", "-c", line)
	cmd.Dir = dir
	cmd.Env = append(testsupport.HermeticGitEnv(packsrc.CleanGitEnv(os.Environ())), "GIT_EDITOR=true",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@e", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@e")
	out, err := cmd.CombinedOutput()
	return string(out), err == nil
}

// midRebase reports whether git records a rebase in progress in the clone at dir.
func midRebase(dir string) bool {
	for _, d := range []string{"rebase-merge", "rebase-apply"} {
		if fi, err := os.Stat(filepath.Join(dir, ".git", d)); err == nil && fi.IsDir() {
			return true
		}
	}
	return false
}

// §14'S DONE CONDITION, BY THE RECIPE: the conflict a launch holds at is printed with the git that
// rebases the series; run as printed, the export pasted mid-rebase changes nothing, and once each
// stop is resolved it replaces the series with one the next fresh launch builds at v1.2.0.
func TestTheConflictsRecipeRunAsPrintedGivesASeriesTheNextLaunchBuilds(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}
	t.Setenv("YOLO_ALLOW_PATCH_FAILURES", "")
	fx, v11, _, first, out, _ := firstAdvance(t)
	v12 := fx.commit(t, "v1.2.0", map[int]string{14: "fourteen", 11: "eleven"})
	if first.delivery.Key == "" || fx.record(t).Good.Commit != v11 {
		t.Fatalf("the first advance did not build v1.1.0:\n%s", out)
	}
	patches := filepath.Join(fx.fork(t).Root, "patches")
	packBefore := treeDigest(t, patches)
	rc, out, errw := packVerb(t, "update")
	cmds := recipeCommands(errw)
	if rc == 0 || len(cmds) != 4 || !strings.Contains(errw, "  Repair: "+packsrc.RecipeLead+"\n") {
		t.Fatalf("update rc=%d, want the conflict's block with four recipe commands, got %q\n%s\n%s", rc, cmds, out, errw)
	}
	for i, w := range []string{"git clone ", "git checkout -q -b yolo-rebase " + fx.fork(t).RebaseRecipe().Base +
		" && git am " + shquote.QuoteDisplay(patches) + "/*.patch && git tag yolo-applied", "git rebase " + v12,
		"test -n \"$(git rev-list -n 1 yolo-rebase --not " + v12 + " yolo-applied --)\" && mkdir " + shquote.QuoteDisplay(patches+".new")} {
		if !strings.HasPrefix(cmds[i], w) {
			t.Errorf("recipe command %d is %q, want it to start %q", i, cmds[i], w)
		}
	}
	if strings.Contains(out+errw, "yolo pack rebase") {
		t.Errorf("update names the retired verb:\n%s\n%s", out, errw)
	}

	work := t.TempDir()
	if said, ok := pasted(t, work, cmds[0]); !ok {
		t.Fatalf("the clone failed:\n%s", said)
	}
	clone := filepath.Join(work, "forkpack-tool-rebase")
	if said, ok := pasted(t, clone, cmds[1]); !ok {
		t.Fatalf("the series did not apply at its base:\n%s", said)
	}
	if said, ok := pasted(t, clone, cmds[2]); ok || !midRebase(clone) {
		t.Fatalf("the rebase did not stop at the conflict:\n%s", said)
	}
	if said, ok := pasted(t, clone, cmds[3]); ok || treeDigest(t, patches) != packBefore {
		t.Fatalf("the export pasted mid-rebase ran or changed the series:\n%s", said)
	}
	// Each stop resolved by writing f.txt as the member being picked leaves it.
	states := []map[int]string{
		{10: "ten", 11: "eleven", 14: "fourteen"},
		{10: "ten", 11: "eleven", 12: "twelve", 14: "fourteen"},
	}
	for i := 0; midRebase(clone); i++ {
		if i >= len(states) {
			t.Fatal("the rebase stopped more often than the series has members")
		}
		done, _ := strconv.Atoi(upstreamGit(t, clone, "rev-list", "--count", v12+"..HEAD"))
		writeFile(t, filepath.Join(clone, "f.txt"), lines30(states[done]))
		if said, ok := pasted(t, clone, "git add f.txt && git rebase --continue"); !ok && !midRebase(clone) {
			t.Fatalf("the continue failed:\n%s", said)
		}
	}
	if said, ok := pasted(t, clone, cmds[3]); !ok {
		t.Fatalf("the export failed once the rebase was done:\n%s", said)
	}

	series := mustSeries(t, fx)
	if series.Base != v12 || series.Len() != 2 {
		t.Fatalf("the exported series has %d members at base %s, want 2 at v1.2.0", series.Len(), series.Base)
	}
	for _, m := range series.Members {
		if strings.Contains(string(m.Data), "\n-- \n") {
			t.Errorf("the exported %s carries a signature trailer:\n%s", m.Name, m.Data)
		}
	}
	fx.later(2 * time.Hour)
	moved, out, _ := fx.launch(t, "podman")
	g := fx.record(t).Good
	if moved.delivery.Key == "" || g.Commit != v12 || g.Series != series.Digest {
		t.Fatalf("the next launch handed %+v with good build %+v, want v1.2.0 with the new series\n%s",
			moved.delivery, g, out)
	}
	if want := lines30(map[int]string{10: "ten", 11: "eleven", 12: "twelve", 14: "fourteen"}); fx.builds[len(fx.builds)-1] != want {
		t.Errorf("the build saw f.txt\n%s\nwant the resolved series at v1.2.0\n%s", fx.builds[len(fx.builds)-1], want)
	}
}

// `yolo pack rebase` IS RETIRED, and typing it names what replaced it rather than an unknown verb.
func TestTheRetiredRebaseVerbNamesWhatReplacedIt(t *testing.T) {
	var out, errw strings.Builder
	rc := packMain([]string{"rebase", "forkpack/tool"}, &out, &errw, false)
	if rc != 2 || !strings.Contains(errw.String(), "yolo pack rebase was removed") ||
		!strings.Contains(errw.String(), "`yolo pack status` and `yolo pack series check` print the commands") ||
		strings.Contains(errw.String(), "unknown verb") {
		t.Errorf("rc=%d\n%s%s", rc, out.String(), errw.String())
	}
}

// `yolo pack status` NAMES THE RECIPE under the newest conflict it records, and nothing names the
// retired verb.
func TestPackStatusPrintsTheRecipeUnderTheConflict(t *testing.T) {
	t.Setenv("YOLO_ALLOW_PATCH_FAILURES", "")
	fx, _, _, _, _, _ := firstAdvance(t)
	v12 := fx.commit(t, "v1.2.0", map[int]string{14: "fourteen", 11: "eleven"})
	packVerb(t, "update")
	_, status, errw := packVerb(t, "status")
	for _, w := range []string{"does not take 0001-ten.patch (conflicts in f.txt)", "    " + packsrc.RecipeLead,
		"git rebase " + v12} {
		if !strings.Contains(status, w) {
			t.Errorf("pack status lacks %q:\n%s\n%s", w, status, errw)
		}
	}
	if strings.Contains(status+errw, "yolo pack rebase") {
		t.Errorf("pack status names the retired verb:\n%s", status)
	}
}
