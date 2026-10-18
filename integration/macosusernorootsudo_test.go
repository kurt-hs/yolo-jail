package integration

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	goruntime "runtime"
	"strconv"
	"testing"
)

// TestMacosUserSuiteSpendsNoRootSudo is OQ-DM1's guard (docs/design/macos-user-suite-on-a-developer-mac.md,
// ruled 2026-10-18: the suite never needs passwordless root sudo). It reads every Go file in this
// package and fails on any string literal that IS a sudo command name, "sudo" or "/usr/bin/sudo":
// the one way a test reaches another account is sandboxCommand (or a production argv builder
// such as macosuser.RunAsSandboxArgv), whose sudo is internal/macosuser's constant and always
// carries `--user=_yolojail` and /usr/bin/env, so a sudo this suite spells itself is one that may
// run as root, or off the rule's shape, and fails on a Mac that grants only what `yolo
// macos-setup` installs.
//
// Not behind a gate and not -short-gated: it is a reading of the source, so the Linux unit run
// catches the regression before any Mac does. The prefix puts it in the macos-user job too.
//
// ⚠ What it cannot see: a sudo inside a shell script's text ("… && sudo rm …"), which is one
// literal of many words, and a sudo path read out of a production argv rather than spelled
// (runPreflight compares an argv's head with macosuser.RunAsGrantProbeArgv()[0], and runs nothing
// with it). The one host-side script that names sudo stubs it out with a shell function first
// (macosuserlayoutrefusal_test.go); a message that names a sudo command for a human to type is
// such a literal, and is allowed.
func TestMacosUserSuiteSpendsNoRootSudo(t *testing.T) {
	// Each entry is file:function, with how many sudo literals it holds and why none of them takes
	// a privilege. The count is exact, so a new sudo in an allowed function still fails.
	type allowance struct {
		count int
		why   string
	}
	allowed := map[string]allowance{
		"macosuserpasswordless_test.go:TestMacosUserPasswordlessStartAndExit": {2, "`sudo -k` drops a " +
			"cached credential, and `sudo -n /usr/bin/true` is the probe that must FAIL: the test " +
			"refuses to run while root sudo needs no password"},
		"macosmachinereset_test.go:runNightlyMachineStep": {1, "the name of a stub written into a " +
			"temporary PATH, so the nightly workflow's script runs no real sudo"},
	}
	_, self, _, ok := goruntime.Caller(0)
	if !ok {
		t.Fatal("cannot locate this package's directory")
	}
	files, err := filepath.Glob(filepath.Join(filepath.Dir(self), "*.go"))
	if err != nil || len(files) == 0 {
		t.Fatalf("listing this package's Go files: %v (%d found)", err, len(files))
	}
	used := map[string]int{}
	fset := token.NewFileSet()
	for _, path := range files {
		f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parsing %s: %v", path, err)
		}
		base := filepath.Base(path)
		for _, decl := range f.Decls {
			name := "(package scope)"
			if fn, ok := decl.(*ast.FuncDecl); ok {
				name = fn.Name.Name
			}
			ast.Inspect(decl, func(n ast.Node) bool {
				lit, ok := n.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return true
				}
				v, err := strconv.Unquote(lit.Value)
				// Spelled in halves, so this file's own literals are not what it finds.
				if err != nil || (v != "su"+"do" && v != "/usr/bin/su"+"do") {
					return true
				}
				key := base + ":" + name
				if _, ok := allowed[key]; ok {
					used[key]++
					return true
				}
				t.Errorf("%s spells a sudo command (%s) in %s. The macos-user suite takes no "+
					"privilege `yolo macos-setup` does not install (OQ-DM1): run a step as the "+
					"sandbox account through sandboxCommand, which builds the one run-as rule's "+
					"argv (macosuser.RunAsSandboxArgv), and every other step as yourself.",
					fset.Position(lit.Pos()), v, name)
				return true
			})
		}
	}
	for key, a := range allowed {
		if used[key] != a.count {
			t.Errorf("%s holds %d sudo literal(s) and its allowance admits exactly %d (%s). A new one "+
				"there is a new privilege: route it through sandboxCommand. One gone: lower the count, "+
				"or remove the allowance, so it cannot admit a new one later", key, used[key], a.count, a.why)
		}
	}
}
