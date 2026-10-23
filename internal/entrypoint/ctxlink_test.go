package entrypoint

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

func ctxLinkEnv(t *testing.T, ctxDir string) (*Env, *strings.Builder) {
	t.Helper()
	home := t.TempDir()
	var errw strings.Builder
	vars := map[string]string{}
	if ctxDir != "" {
		vars[paths.ContextDirEnv] = ctxDir
	}
	return &Env{Home: home, Workspace: t.TempDir(), Vars: vars, Stderr: &errw}, &errw
}

func TestLinkContextDirCreatesTheLink(t *testing.T) {
	ctx := t.TempDir()
	e, errw := ctxLinkEnv(t, ctx)
	if err := LinkContextDir(e); err != nil {
		t.Fatal(err)
	}
	got, err := os.Readlink(filepath.Join(e.Home, "ctx"))
	if err != nil {
		t.Fatalf("~/ctx is not a link: %v", err)
	}
	if got != ctx {
		t.Errorf("~/ctx -> %q, want %q", got, ctx)
	}
	if errw.Len() != 0 {
		t.Errorf("unexpected stderr: %q", errw.String())
	}
}

func TestLinkContextDirIsANoOpWhenAlreadyCorrect(t *testing.T) {
	ctx := t.TempDir()
	e, errw := ctxLinkEnv(t, ctx)
	link := filepath.Join(e.Home, "ctx")
	if err := os.Symlink(ctx, link); err != nil {
		t.Fatal(err)
	}
	if err := LinkContextDir(e); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.Readlink(link); got != ctx {
		t.Errorf("~/ctx -> %q, want it unchanged at %q", got, ctx)
	}
	if errw.Len() != 0 {
		t.Errorf("unexpected stderr: %q", errw.String())
	}
}

func TestLinkContextDirLeavesAnythingElseAloneAndSaysSo(t *testing.T) {
	ctx := t.TempDir()
	other := t.TempDir()
	cases := map[string]func(t *testing.T, p string){
		"file": func(t *testing.T, p string) {
			if err := os.WriteFile(p, []byte("user data"), 0o644); err != nil {
				t.Fatal(err)
			}
		},
		"directory": func(t *testing.T, p string) {
			if err := os.Mkdir(p, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(p, "keep.txt"), []byte("x"), 0o644); err != nil {
				t.Fatal(err)
			}
		},
		"other link": func(t *testing.T, p string) {
			if err := os.Symlink(other, p); err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, mk := range cases {
		t.Run(name, func(t *testing.T) {
			e, errw := ctxLinkEnv(t, ctx)
			p := filepath.Join(e.Home, "ctx")
			mk(t, p)
			before, err := os.Lstat(p)
			if err != nil {
				t.Fatal(err)
			}
			if err := LinkContextDir(e); err != nil {
				t.Fatal(err)
			}
			after, err := os.Lstat(p)
			if err != nil {
				t.Fatalf("~/ctx vanished: %v", err)
			}
			if after.Mode() != before.Mode() || !os.SameFile(before, after) {
				t.Errorf("~/ctx was replaced: before %v, after %v", before.Mode(), after.Mode())
			}
			want := "yolo: ~/ctx already exists and is not yolo's link to $YOLO_CONTEXT_DIR (" + ctx + "); leaving it.\n"
			if errw.String() != want {
				t.Errorf("stderr = %q, want %q", errw.String(), want)
			}
		})
	}
}

// A link the home cannot take (here, a home that does not exist; on podman, a read-only home
// root) is a warning, never a refused boot.
func TestLinkContextDirFailureIsAWarningNotARefusal(t *testing.T) {
	e, errw := ctxLinkEnv(t, t.TempDir())
	e.Home = filepath.Join(t.TempDir(), "missing")
	if err := LinkContextDir(e); err != nil {
		t.Fatalf("a link that cannot be made refused the boot: %v", err)
	}
	if !strings.Contains(errw.String(), "could not link ~/ctx") {
		t.Errorf("no warning for the failed link: %q", errw.String())
	}
}

func TestLinkContextDirDoesNothingWithoutTheVariable(t *testing.T) {
	e, errw := ctxLinkEnv(t, "")
	if err := LinkContextDir(e); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(e.Home, "ctx")); !os.IsNotExist(err) {
		t.Errorf("~/ctx exists without YOLO_CONTEXT_DIR: %v", err)
	}
	if errw.Len() != 0 {
		t.Errorf("unexpected stderr: %q", errw.String())
	}
}

// The step is in the table of both boots, and the table entry names LinkContextDir as its
// generator. Fails if the production call site is removed or points at another function.
func TestTheContextLinkStepCallsLinkContextDir(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "bootsteps.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	ast.Inspect(f, func(n ast.Node) bool {
		lit, ok := n.(*ast.CompositeLit)
		if !ok {
			return true
		}
		var name string
		var gen ast.Expr
		for _, el := range lit.Elts {
			kv, ok := el.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			key, _ := kv.Key.(*ast.Ident)
			if key == nil {
				continue
			}
			switch key.Name {
			case "name":
				if bl, ok := kv.Value.(*ast.BasicLit); ok {
					name = strings.Trim(bl.Value, `"`)
				}
			case "gen":
				gen = kv.Value
			}
		}
		if name == "link_context_dir" {
			if id, ok := gen.(*ast.Ident); ok && id.Name == "LinkContextDir" {
				found = true
			}
		}
		return true
	})
	if !found {
		t.Error(`bootsteps.go has no step {name: "link_context_dir", gen: LinkContextDir}`)
	}
	for _, target := range []bootTarget{bootContainer, bootDarwin} {
		if !slices.Contains(bootStepNames(target), "link_context_dir") {
			t.Errorf("%s boot does not run link_context_dir", target)
		}
	}
}
