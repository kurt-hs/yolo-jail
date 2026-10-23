package run

// packtreepins_test.go pins a PACK-TREE PROGRAM's launch trigger at its call site, Run's own entry
// point (docs/design/pack-buildable-programs.md §6.1): a launch whose packs carry one hands the
// build act a pin keyed on the tree digest of its source directory in the launch's staged pack
// tree, and the jail the key the act answers — with no "Forks this launch" line, since it is no
// fork and a hit says nothing.

import (
	"go/ast"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/entrypoint"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
)

func TestALaunchHandsItsPackTreeProgramToTheBuildActByDigest(t *testing.T) {
	home := packHome(t)
	t.Setenv("YOLO_VERSION", "")
	t.Setenv("YOLO_PACK_ROOT", "")
	pack := t.TempDir()
	write := func(rel, body string) {
		t.Helper()
		p := filepath.Join(pack, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("pack.json", `{"contributes":[{"kind":"program","bin":"x","via":"pack","from":"tools/x","build":"make","run":"x"}]}`)
	write("tools/x/VERSION", "1\n")
	write("tools/x/.git/HEAD", "ref: refs/heads/main\n")
	writeUserPacks(t, home, `[{"source":"file://`+pack+`","name":"fx"}]`)
	want, err := packload.PackTreeDigest(filepath.Join(pack, "tools", "x"))
	if err != nil || want == "" {
		t.Fatalf("digest of the fixture: %q %v", want, err)
	}

	var got []packload.ForkPin
	argv, printed := fakePodmanLaunch(t, func(o *Options) {
		o.BuildForks = func(req ForkBuildRequest) map[string]entrypoint.ForkDelivery {
			got = append(got, req.Pins...)
			return map[string]entrypoint.ForkDelivery{"x": {Key: "k-tree"}}
		}
	})
	if len(got) != 1 || !got[0].Fork.IsPackTree() || got[0].Commit != want || got[0].Fork.BuildSource() != "pack:fx/tools/x" {
		t.Fatalf("the build act was handed %+v, want x keyed on its tree digest %s\n%s", got, want, printed)
	}
	if strings.HasPrefix(got[0].Fork.Root, pack) {
		t.Errorf("the pin reads the pack's own directory %s, not the launch's staged copy", got[0].Fork.Root)
	}
	if d := forkBuildsInArgv(t, argv); d["x"].Key != "k-tree" {
		t.Errorf("the jail is handed %+v, want x's key k-tree\n%s", d, printed)
	}
	if strings.Contains(printed, "Forks this launch") {
		t.Errorf("a launch with no fork printed the forks' block:\n%s", printed)
	}
}

// THE PREBUILT OPT-IN AT A LAUNCH (BG-D18, OQ-BG2): with the source present, a launch hands the build
// act the BUILD — the declared prebuilt is never substituted — unless the user's own config sets
// `programs.overrides.x.prebuilt`, when it hands the verified prebuilt's pin (its sha256) instead.
// Delete packTreePins' read of config.ProgramPrebuiltOptIns and the opted-in half goes red.
func TestALaunchTakesThePrebuiltOverAPresentSourceOnlyWithTheOptIn(t *testing.T) {
	sum := strings.Repeat("c", 64)
	for _, optIn := range []bool{false, true} {
		home := packHome(t)
		t.Setenv("YOLO_VERSION", "")
		t.Setenv("YOLO_PACK_ROOT", "")
		pack := t.TempDir()
		for rel, body := range map[string]string{
			"pack.json": `{"contributes":[{"kind":"program","bin":"x","via":"pack","from":"tools/x","build":"make",` +
				`"run":"x","prebuilt":{"` + jailPlatform("podman") + `":{"url":"https://example.invalid/x","sha256":"` + sum + `"}}}]}`,
			"tools/x/VERSION": "1\n",
		} {
			p := filepath.Join(pack, rel)
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		packs := `[{"source":"file://` + pack + `","name":"fx"}]`
		if optIn {
			packs += `, "programs": {"overrides": {"x": {"prebuilt": true}}}`
		}
		writeUserPacks(t, home, packs)

		var got []packload.ForkPin
		_, printed := fakePodmanLaunch(t, func(o *Options) {
			o.BuildForks = func(req ForkBuildRequest) map[string]entrypoint.ForkDelivery {
				got = append(got, req.Pins...)
				return map[string]entrypoint.ForkDelivery{"x": {Key: "k"}}
			}
		})
		if len(got) != 1 {
			t.Fatalf("opt-in %v: the build act was handed %d pins, want one\n%s", optIn, len(got), printed)
		}
		if optIn {
			if !got[0].Fork.IsPrebuilt() || got[0].Commit != sum {
				t.Errorf("opted in: the build act was handed %+v, want the prebuilt at sha256 %s", got[0], sum)
			}
			continue
		}
		if got[0].Fork.IsPrebuilt() || got[0].Commit == sum || got[0].Commit == "" {
			t.Errorf("no opt-in: the build act was handed %+v, want the build of the present source", got[0])
		}
	}
}

// NEVER DIGEST ON AN ATTACH OR A DRY RUN (docs/design/pack-buildable-programs.md §10): the digest
// reads every source directory whole, so the pack-tree pins are made on the fresh-launch path
// alone — in runContainer below every attach site, and in Run's macos-user arm in the branch a dry
// run does not take — each just before the slot. Red if the pins move back above the attach
// decision or into the dry run.
func TestPackTreePinsAreMadeOnTheFreshLaunchPathAlone(t *testing.T) {
	type sites struct{ pins, direct, slot, lastAttach, dryRun []token.Pos }
	find := func(name string) sites {
		var s sites
		ast.Inspect(funcDecl(t, "run.go", name), func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			if f, ok := call.Fun.(*ast.SelectorExpr); ok {
				switch f.Sel.Name {
				case "addPackTreePins":
					s.pins = append(s.pins, call.Pos())
				case "packTreePins":
					s.direct = append(s.direct, call.Pos())
				case "runForkBuildSlotAndRefusals":
					s.slot = append(s.slot, call.Pos())
				case "attachExisting":
					s.lastAttach = append(s.lastAttach, call.Pos())
				case "dryRunForkDeliveries":
					s.dryRun = append(s.dryRun, call.Pos())
				}
			}
			return true
		})
		return s
	}
	c := find("runContainer")
	if len(c.direct) != 0 || len(c.pins) != 1 || len(c.slot) != 1 || len(c.lastAttach) == 0 ||
		c.pins[0] < c.lastAttach[len(c.lastAttach)-1] || c.pins[0] > c.slot[0] {
		t.Errorf("runContainer must pin the pack-tree programs once, below every attach site and before the slot: %+v", c)
	}
	r := find("Run")
	if len(r.direct) != 0 || len(r.pins) != 1 || len(r.slot) != 1 || len(r.dryRun) != 1 ||
		r.pins[0] < r.dryRun[0] || r.pins[0] > r.slot[0] {
		t.Errorf("Run must pin the pack-tree programs only in the macos-user arm's non-dry-run branch, before its slot: %+v", r)
	}
}
