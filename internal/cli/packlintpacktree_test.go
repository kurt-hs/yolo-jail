package cli

// packlintpacktree_test.go pins `yolo pack lint` on a program built from its pack's own tree
// (`via: "pack"`, docs/design/pack-buildable-programs.md): lint recognizes the vocabulary, refuses
// a malformed declaration with the field and its fix, and warns (BG-D16) when the source directory
// is absent with no `prebuilt`. Lint reads the manifest through the same decoder a launch does, so
// these tests pin the recognition, not a second rule set.

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writePackTreePack writes a pack whose one program is the given JSON contribution, and, when
// source is set, a file under the program's `from` directory so the directory is present.
func writePackTreePack(t *testing.T, contribution string, source bool) string {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "pack.json"),
		`{"name":"board","description":"b","contributes":[`+contribution+`]}`)
	if source {
		if err := os.MkdirAll(filepath.Join(dir, "tools", "board-cli"), 0o755); err != nil {
			t.Fatal(err)
		}
		writeFile(t, filepath.Join(dir, "tools", "board-cli", "package.json"), "{}\n")
	}
	return dir
}

const packTreeProgram = `{"kind":"program","bin":"board","via":"pack",` +
	`"from":"tools/board-cli","build":"npm ci && npm run bundle","run":"dist/board.js"}`

// TestPackLintAcceptsAPackTreeProgram pins recognition: a valid declaration whose source is staged
// lints clean, with no warning about its `from`.
func TestPackLintAcceptsAPackTreeProgram(t *testing.T) {
	dir := writePackTreePack(t, packTreeProgram, true)
	var out, errw bytes.Buffer
	if rc := packMain([]string{"lint", dir}, &out, &errw, false); rc != 0 {
		t.Fatalf("lint refused a valid pack-tree program: rc %d\n%s%s", rc, out.String(), errw.String())
	}
	if strings.Contains(out.String(), "holds no file") {
		t.Errorf("lint warned about a source directory that is present:\n%s", out.String())
	}
}

// TestPackLintRefusesAPackTreeProgramByField pins the refusal: a malformed declaration fails lint
// and names the field it is wrong about.
func TestPackLintRefusesAPackTreeProgramByField(t *testing.T) {
	dir := writePackTreePack(t, `{"kind":"program","bin":"board","via":"pack","from":"tools/board-cli"}`, true)
	var out, errw bytes.Buffer
	if rc := packMain([]string{"lint", dir}, &out, &errw, false); rc == 0 {
		t.Fatalf("lint passed a pack-tree program with no \"run\":\n%s%s", out.String(), errw.String())
	}
	if !strings.Contains(out.String()+errw.String(), `"run"`) {
		t.Errorf("lint does not name the missing \"run\":\n%s%s", out.String(), errw.String())
	}
}

// TestPackLintWarnsWhenTheSourceDirectoryIsAbsent pins BG-D16: the source directory staging no
// file and no `prebuilt` is a warning, not a failure, and it names the program and the directory.
func TestPackLintWarnsWhenTheSourceDirectoryIsAbsent(t *testing.T) {
	dir := writePackTreePack(t, packTreeProgram, false)
	var out, errw bytes.Buffer
	packMain([]string{"lint", dir}, &out, &errw, false)
	if !strings.Contains(out.String(), `program "board"`) || !strings.Contains(out.String(), "holds no file") {
		t.Errorf("lint does not warn about an absent source directory:\n%s%s", out.String(), errw.String())
	}
}
