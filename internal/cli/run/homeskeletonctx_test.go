package run

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/config"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

// A host_files destination that claims ctx keeps its own directory, and the skeleton lays no
// link there. The skeleton says so in one warning line.
func TestAClaimedCtxKeepsItsOwnDirAndGetsNoLink(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	hostFiles := []config.HostFileEntry{
		{Path: "ctx/notes.txt", Codec: "raw", HasContent: true, Mode: config.HostFileModeOnce},
	}
	sk, err := buildHomeSkeleton(paths.HomeSkeletonRoot("yolo-ctx-claimed"), nil, nil, hostFiles, nil)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(filepath.Join(sk.dir, paths.ContextLinkName))
	if err != nil {
		t.Fatalf("the claimed ctx entry is missing: %v", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		t.Errorf("~/ctx is a %v, want the claimed directory kept as a directory", info.Mode())
	}
	var claimed []string
	for _, w := range sk.warnings {
		if strings.Contains(w, "claimed") && strings.Contains(w, "~/ctx") {
			claimed = append(claimed, w)
		}
	}
	if len(claimed) != 1 {
		t.Errorf("want one claimed-ctx line, got %d in %v", len(claimed), sk.warnings)
	}
}

// Nothing is laid through the link: with no claim, the link is the only ctx entry, and no
// skeleton path lies under it.
func TestNothingIsCreatedUnderTheContextLink(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	sk, err := buildHomeSkeleton(paths.HomeSkeletonRoot("yolo-ctx-nothing-under"), nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := os.Readlink(filepath.Join(sk.dir, paths.ContextLinkName)); err != nil || got != paths.ContainerContextDir {
		t.Fatalf("~/ctx -> %q (err %v), want %q", got, err, paths.ContainerContextDir)
	}
	prefix := filepath.Join(sk.dir, paths.ContextLinkName) + string(filepath.Separator)
	err = filepath.WalkDir(sk.dir, func(p string, _ os.DirEntry, err error) error {
		if err == nil && strings.HasPrefix(p, prefix) {
			t.Errorf("the skeleton holds %s, under the link to %s", p, paths.ContainerContextDir)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

// The link is laid last, so no MkdirAll, touch or symlink of the skeleton ever runs through it.
// Fails if the call is moved above one of the builder's own calls.
func TestTheContextLinkIsLaidAfterEveryOtherSkeletonEntry(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "homeskeleton.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var fn *ast.FuncDecl
	for _, d := range f.Decls {
		if d, ok := d.(*ast.FuncDecl); ok && d.Name.Name == "buildHomeSkeleton" {
			fn = d
		}
	}
	if fn == nil {
		t.Fatal("buildHomeSkeleton not found")
	}
	var linkAt token.Pos
	var lastOther token.Pos
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		switch sel.Sel.Name {
		case "layContextLink":
			linkAt = call.Pos()
		case "mkdir", "touch", "symlink":
			if call.End() > lastOther {
				lastOther = call.End()
			}
		}
		return true
	})
	if linkAt == token.NoPos {
		t.Fatal("buildHomeSkeleton never calls layContextLink")
	}
	if linkAt < lastOther {
		t.Errorf("layContextLink is called before a builder entry; the link must come last")
	}
}

// A link the skeleton cannot make is one warning line, never an error: the launch boots.
func TestAFailedContextLinkIsAWarning(t *testing.T) {
	b := skeletonBuilder{dir: filepath.Join(t.TempDir(), "missing"), asked: &[]string{}}
	w := b.layContextLink(paths.ContextLinkName, paths.ContainerContextDir)
	if !strings.Contains(w, "could not link ~/ctx") {
		t.Errorf("a failed link gave %q, want the could-not-link warning", w)
	}
}

// The claim check matches the path itself and anything under it, and nothing that merely
// begins with the same letters.
func TestClaimsPathMatchesCtxAndItsChildrenOnly(t *testing.T) {
	cases := map[string]bool{
		"ctx":         true,
		"ctx/a/b":     true,
		"./ctx/x":     true,
		"ctx.bak":     false,
		"a/ctx":       false,
		"ctxtra/file": false,
		"":            false,
	}
	for path, want := range cases {
		asked := []string{path}
		if got := claimsPath(&asked, paths.ContextLinkName); got != want {
			t.Errorf("claimsPath(%q) = %v, want %v", path, got, want)
		}
	}
}
