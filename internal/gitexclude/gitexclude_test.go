package gitexclude

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func repo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not on PATH")
	}
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(dir))
	if out, err := exec.Command("git", "-C", dir, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	return dir
}

// A line names the path from the top of the work tree, so a workspace in a subdirectory of its
// repository excludes exactly its own file, and appending it makes git ignore that file.
func TestLineAnchorsAtTheWorkTreeTopAndAppendIgnores(t *testing.T) {
	top := repo(t)
	sub := filepath.Join(top, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(sub, "private.toml")
	if got := Check(path); got != NotIgnored {
		t.Fatalf("Check before = %v", got)
	}
	line, ok := Line(path)
	if !ok || line != "/sub/private.toml" {
		t.Fatalf("Line = %q, %v", line, ok)
	}
	exclude := File(sub)
	if exclude != filepath.Join(top, ".git", "info", "exclude") {
		t.Fatalf("File = %q", exclude)
	}
	if err := os.WriteFile(exclude, []byte("# no trailing newline"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Append(exclude, line); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(exclude); string(data) != "# no trailing newline\n/sub/private.toml\n" {
		t.Fatalf("exclude = %q", data)
	}
	if got := Check(path); got != Ignored {
		t.Fatalf("Check after = %v", got)
	}
	if got := Check(filepath.Join(top, "private.toml")); got != NotIgnored {
		t.Fatalf("a same-named file elsewhere became ignored: %v", got)
	}
}

func TestOutsideAWorkTreeGitCannotAnswer(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not on PATH")
	}
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(dir))
	path := filepath.Join(dir, "x.toml")
	if Check(path) != Unknown || File(dir) != "" {
		t.Fatal("git answered outside a work tree")
	}
	if _, ok := Line(path); ok {
		t.Fatal("Line named a path outside a work tree")
	}
}
