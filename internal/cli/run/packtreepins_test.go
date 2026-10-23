package run

// packtreepins_test.go pins a PACK-TREE PROGRAM's launch trigger at its call site, Run's own entry
// point (docs/design/pack-buildable-programs.md §6.1): a launch whose packs carry one hands the
// build act a pin keyed on the tree digest of its source directory in the launch's staged pack
// tree, and the jail the key the act answers — with no "Forks this launch" line, since it is no
// fork and a hit says nothing.

import (
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
