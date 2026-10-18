package run

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/brokerscope"
)

// The two seams that are closures rather than the step itself are driven against real files:
// liveScopeFilesSeam reads a bound broker scope file under a temp HOME and carries its live-scope
// capability, and writeCtxMountRecordSeam restores the inspected token after the real record
// invalidator leaves its tombstone.
func TestLiveApplyClosureSeamsReachTheRealFiles(t *testing.T) {
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "")

	f := brokerscope.File{Source: "github", LaunchID: "l1", Workspace: "/w", Container: "yolo-c",
		PID: os.Getpid(), Repos: []string{"o/r"}, ConfigFile: "yolo-jail.jsonc", LocalFile: "yolo-jail.local.jsonc"}
	path, err := brokerscope.Write(f)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := brokerscope.BindBroker(path, f, "start-l1", func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	files, err := liveScopeFilesSeam(context.Background(), "github", "yolo-c", "/w")
	if err != nil || len(files) != 1 || files[0].Path != path || !files[0].LiveScope ||
		len(files[0].File.Repos) != 1 || files[0].File.Repos[0] != "o/r" {
		t.Fatalf("liveScopeFilesSeam = %+v, %v; want the one bound file %s, taking live scope", files, err, path)
	}
	if files, _ := liveScopeFilesSeam(context.Background(), "github", "yolo-other", "/w"); len(files) != 0 {
		t.Errorf("another container's file was returned: %+v", files)
	}

	const cname = "yolo-c"
	if err := writeCtxMountRecord(cname, "tok", nil); err != nil {
		t.Fatal(err)
	}
	if err := invalidateCtxMountRecord(cname); err != nil {
		t.Fatal(err)
	}
	want := []liveCtxMount{{Source: "/h/a", Dest: "/ctx/a", RW: true}}
	if err := writeCtxMountRecordSeam(cname, "tok", want); err != nil {
		t.Fatal(err)
	}
	got, ok := readCtxMountRecord(cname, []string{ctxMountsTokenEnv + "=tok"})
	if !ok || !reflect.DeepEqual(got, want) {
		t.Errorf("the record reads %v (ok %v) under the launch's token, want %v", got, ok, want)
	}
}

// TestLiveApplySeamsAreTheRealSteps fails when a live-apply seam's default stops being the step
// it stands for: the apply's tests swap every seam, so a default left as a stub (or replaced by
// one) would ship a command that applies nothing while its whole suite stays green.
func TestLiveApplySeamsAreTheRealSteps(t *testing.T) {
	ptr := func(f any) uintptr { return reflect.ValueOf(f).Pointer() }
	for _, c := range []struct {
		name       string
		seam, real any
	}{
		{"replaceScopeRepos", replaceScopeReposSeam, brokerscope.ReplaceReposContext},
		{"ctxMountsLive", ctxMountsLiveSeam, (*Options).ctxMountsLive},
		{"readCtxMountRecord", readCtxMountRecordSeam, readCtxMountRecord},
		{"diffCtxMounts", diffCtxMountsSeam, diffCtxMounts},
		{"injectCtxMount", injectCtxMountSeam, (*Options).injectCtxMount},
		{"removeCtxMount", removeCtxMountSeam, (*Options).removeCtxMount},
	} {
		if ptr(c.seam) != ptr(c.real) {
			t.Errorf("%s seam is not the real step", c.name)
		}
	}
}
