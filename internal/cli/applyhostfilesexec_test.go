package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/packsrc"
	"github.com/mschulkind-oss/yolo-jail/internal/testsupport"
)

// THE GUARANTEE at the host notch (docs/reference/pack-system.md, "The exec bit"): `yolo host
// apply` renders a `files` source's exec bit, read-only (0o555, or 0o444 without it), for every
// route a pack arrives by — a SHIPPED pack selected by bare name (embedded; its bits come from
// packs/executables.txt), a GIT pack fetched by `yolo pack install` (its bits come from the
// committed mode, which the store's checkout restores), and a pack
// selected BY PATH. Driven through applyHost, the verb's own entry, into a scratch HOME.
func TestHostApplyRendersAFilesSourcesExecBit(t *testing.T) {
	// The git route: a script committed 100755.
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	repo := t.TempDir()
	writeFile(t, filepath.Join(repo, "pack.json"),
		`{"name":"gp","contributes":[{"kind":"files","from":"files","into":".gp"}]}`)
	writeFile(t, filepath.Join(repo, "files", "hook.sh"), "#!/bin/sh\n")
	writeFile(t, filepath.Join(repo, "files", "notes.md"), "notes\n")
	if err := os.Chmod(filepath.Join(repo, "files", "hook.sh"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init", "-q", "-b", "main"}, {"add", "-A"}, {"commit", "-qm", "pack"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		cmd.Env = append(testsupport.HermeticGitEnv(packsrc.CleanGitEnv(os.Environ())), "GIT_AUTHOR_NAME=t",
			"GIT_AUTHOR_EMAIL=t@e", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@e")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	// The by-path route.
	local := t.TempDir()
	writeFile(t, filepath.Join(local, "pack.json"),
		`{"name":"lp","contributes":[{"kind":"files","from":"files/run.sh","into":".lp/run.sh"}]}`)
	writeFile(t, filepath.Join(local, "files", "run.sh"), "#!/bin/sh\n")
	if err := os.Chmod(filepath.Join(local, "files", "run.sh"), 0o755); err != nil {
		t.Fatal(err)
	}

	home := gitPackHome(t, "git+file://"+repo+"?ref=main", `,"host_management":"own"`)
	selectPacksWith(t, home, `"muse-planner",{"source":"git+file://`+repo+`?ref=main","name":"gp"},`+
		`{"source":"file://`+local+`","name":"lp"}`, `,"host_management":"own"`)
	t.Setenv("YOLO_VERSION", "")
	t.Chdir(t.TempDir())
	installGitPack(t)

	var out, errw bytes.Buffer
	if rc := applyHost(&out, &errw, false, true, strings.NewReader("y\n")); rc != 0 {
		t.Fatalf("host apply rc=%d\n%s%s", rc, out.String(), errw.String())
	}
	for rel, want := range map[string]os.FileMode{
		".pi/agent/bin/muse-planner-exec.sh": 0o555, // embedded
		".pi/agent/agents/muse-planner.md":   0o444,
		".gp/hook.sh":                        0o555, // git
		".gp/notes.md":                       0o444,
		".lp/run.sh":                         0o555, // by path
	} {
		fi, err := os.Stat(filepath.Join(home, filepath.FromSlash(rel)))
		if err != nil {
			t.Errorf("~/%s not rendered: %v\n%s%s", rel, err, out.String(), errw.String())
			continue
		}
		if fi.Mode().Perm() != want {
			t.Errorf("~/%s rendered at %04o, want %04o", rel, fi.Mode().Perm(), want)
		}
	}
}
