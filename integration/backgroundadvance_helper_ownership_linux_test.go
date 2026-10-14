//go:build linux

package integration

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	goruntime "runtime"
	"slices"
	"testing"
)

// All PIDs and observations here are synthetic. No cleanup is registered and no
// signal/runtime API is called, including in the deliberately unsafe RED run.
func TestBackgroundAdvanceCaptureBuildHelperOwnership(t *testing.T) {
	bg, child := xbProcess{100, "scheduler-birth"}, xbProcess{300, "child-birth"}
	helper := xbProcess{200, "helper-birth"}
	store := filepath.Join(os.Getenv("HOME"), "private-captures")
	staging := filepath.Join(store, "staging", "fork-fixture")
	cases := []struct {
		name      string
		change    func(map[int]xbHelperObservation)
		rereadPID int
		reread    func(*xbHelperObservation)
		accept    bool
	}{
		{name: "exact-owned-chain", accept: true},
		{name: "wrong-helper", change: func(m map[int]xbHelperObservation) { s := m[200]; s.args = []string{"yolo", "unrelated"}; m[200] = s }},
		{name: "wrong-key", change: func(m map[int]xbHelperObservation) { s := m[200]; s.args[5] = "--key=other/tree"; m[200] = s }},
		{name: "extra-argument", change: func(m map[int]xbHelperObservation) {
			s := m[200]
			s.args = append(s.args, "--key=other/tree")
			m[200] = s
		}},
		{name: "pid1", change: func(m map[int]xbHelperObservation) { s := m[300]; s.parent = 1; m[300] = s; m[1] = m[200] }},
		{name: "reparented-subreaper", change: func(m map[int]xbHelperObservation) { s := m[200]; s.parent = 999; m[200] = s }},
		{name: "child-birth-changed", change: func(m map[int]xbHelperObservation) { s := m[300]; s.birth = "recycled"; m[300] = s }},
		{name: "child-birth-race", rereadPID: 300, reread: func(s *xbHelperObservation) { s.birth = "recycled" }},
		{name: "child-ppid-race", rereadPID: 300, reread: func(s *xbHelperObservation) { s.parent = 1 }},
		{name: "parent-birth-race", rereadPID: 200, reread: func(s *xbHelperObservation) { s.birth = "recycled" }},
		{name: "parent-ppid-race", rereadPID: 200, reread: func(s *xbHelperObservation) { s.parent = 1 }},
		{name: "parent-argv-race", rereadPID: 200, reread: func(s *xbHelperObservation) { s.args = []string{"yolo", "unrelated"} }},
		{name: "scheduler-birth-race", rereadPID: 100, reread: func(s *xbHelperObservation) { s.birth = "recycled" }},
		{name: "wrong-private-home", change: func(m map[int]xbHelperObservation) { s := m[200]; s.home = "/other/home"; m[200] = s }},
		{name: "wrong-executable", change: func(m map[int]xbHelperObservation) { s := m[200]; s.exe = "/other/yolo"; m[200] = s }},
		{name: "no-owned-request", change: func(m map[int]xbHelperObservation) { s := m[100]; s.request = ""; m[100] = s }},
		{name: "request-race", rereadPID: 100, reread: func(s *xbHelperObservation) { s.request = "" }},
		{name: "wrong-child-workspace", change: func(m map[int]xbHelperObservation) { s := m[300]; s.args[3] = "--workspace=/other/staging"; m[300] = s }},
		{name: "unknown-parent", change: func(m map[int]xbHelperObservation) { delete(m, 200) }},
		{name: "terminal-parent", change: func(m map[int]xbHelperObservation) { s := m[200]; s.state = "Z"; m[200] = s }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			home := os.Getenv("HOME")
			m := map[int]xbHelperObservation{
				100: {birth: bg.birth, state: "S", home: home, exe: "/private/yolo", request: "/private/request.json"},
				200: {birth: helper.birth, state: "S", parent: bg.pid, home: home, exe: "/private/yolo", args: []string{"yolo", "internal", "background-advance", "--runtime=podman", "--platform=linux/" + goruntime.GOARCH, "--key=" + xbKey, "--shared-demand"}},
				300: {birth: child.birth, state: "S", parent: helper.pid, home: home, exe: "/private/yolo", args: []string{"yolo", "internal", "fork-build-jail", "--workspace=" + staging}},
			}
			if tc.change != nil {
				tc.change(m)
			}
			f := &xbFixture{store: store, owned: []xbProcess{bg, child}}
			reads := map[int]int{}
			got, err := f.captureBuildHelper(bg, child, staging, func(pid int) (xbHelperObservation, error) {
				s, ok := m[pid]
				if !ok {
					return s, fmt.Errorf("unknown pid %d", pid)
				}
				reads[pid]++
				if pid == tc.rereadPID && reads[pid] > 1 {
					tc.reread(&s)
				}
				return s, nil
			})
			if tc.accept {
				if err != nil || got != helper || len(f.owned) != 3 || f.owned[2] != helper {
					t.Fatalf("valid ownership not adopted: got=%+v err=%v owned=%v", got, err, f.owned)
				}
			} else if err == nil || !slices.Equal(f.owned, []xbProcess{bg, child}) {
				t.Fatalf("uncertain ownership adopted: got=%+v err=%v owned=%v", got, err, f.owned)
			}
		})
	}
	for _, tc := range []struct {
		name  string
		owned []xbProcess
	}{{"unremembered-scheduler", []xbProcess{child}}, {"unremembered-child", []xbProcess{bg}}} {
		t.Run(tc.name, func(t *testing.T) {
			f := &xbFixture{store: store, owned: tc.owned}
			observed := false
			_, err := f.captureBuildHelper(bg, child, staging, func(int) (xbHelperObservation, error) {
				observed = true
				return xbHelperObservation{}, fmt.Errorf("must reject before observing an unowned chain")
			})
			if err == nil || len(f.owned) != 1 || observed {
				t.Fatal("unremembered process reached parent discovery/adoption")
			}
		})
	}
}

// Pin the public killed caller to the tested adoption method: moving validation
// into a disconnected helper or restoring raw PPID adoption must fail -short.
func TestBackgroundAdvanceSignalsUsesGuardedHelperCapture(t *testing.T) {
	_, here, _, _ := goruntime.Caller(0)
	file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(filepath.Dir(here), "backgroundadvance_linux_test.go"), nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "TestBackgroundAdvanceSignalsFenceAndRecoverItsRealBuildWorkspace" {
			continue
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			if assignment, ok := n.(*ast.AssignStmt); ok {
				for _, lhs := range assignment.Lhs {
					if sel, ok := lhs.(*ast.SelectorExpr); ok && sel.Sel.Name == "owned" {
						t.Error("public signal caller adopts processes outside ownership guard")
					}
				}
			}
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "captureBuildHelper" {
				calls++
				if len(call.Args) != 4 {
					t.Error("ownership capture lost its observations")
				}
				if id, ok := call.Args[3].(*ast.Ident); !ok || id.Name != "xbObserveHelperProcess" {
					t.Error("public caller does not use real proc observations")
				}
			}
			if id, ok := call.Fun.(*ast.Ident); ok && id.Name == "readStat" {
				t.Errorf("public signal caller bypasses ownership capture via %s", id.Name)
			}
			return true
		})
	}
	if calls != 1 {
		t.Fatalf("public signal caller has %d guarded captures, want one", calls)
	}
}
