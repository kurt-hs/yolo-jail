package run

import (
	"os"
	"path/filepath"
	"testing"
)

// THE GUARANTEE (docs/reference/pack-system.md, "The exec bit"): a `files` source's exec bit
// reaches the jail, through the launch's real staging and its real mount plan, for both
// routes a pack arrives by — a SHIPPED pack selected by bare name (embedded, whose bits come
// from packs/executables.txt) and a pack selected BY PATH (whose bits come from its own
// files). Both shapes of mount are covered: a single file is bound from the staged tree as
// is, and contributions sharing a root are merged into a per-launch copy by copyOneFile.
func TestAFilesSourcesExecBitReachesTheJail(t *testing.T) {
	home := packHome(t)
	local := filepath.Join(t.TempDir(), "hooks")
	if err := os.MkdirAll(filepath.Join(local, "files", "hooks"), 0o755); err != nil {
		t.Fatal(err)
	}
	writePack(t, local, `{"name":"hooks","contributes":[
		{"kind":"files","from":"files/hooks","into":".config/hookpack"},
		{"kind":"files","from":"files/extra.sh","into":".config/hookpack/extra.sh"}]}`)
	for name, mode := range map[string]os.FileMode{
		"hooks/session-start.sh": 0o755, "hooks/README.md": 0o644, "extra.sh": 0o755,
	} {
		p := filepath.Join(local, "files", filepath.FromSlash(name))
		if err := os.WriteFile(p, []byte("#!/bin/sh\n"), mode); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(p, mode); err != nil {
			t.Fatal(err)
		}
	}
	writeUserPacks(t, home, `["muse-planner", {"source":"file://`+local+`"}]`)

	o := &Options{Workspace: t.TempDir(), Stdout: discardBuf(), Stderr: discardBuf()}
	_, loaded, _, err := o.stagePacks("yolo-test-files-exec")
	if err != nil {
		t.Fatalf("stagePacks: %v", err)
	}
	agentsPath := t.TempDir()
	plan := planPackFilesMounts(loaded, nil, agentsPath)
	if len(plan.Conflicts) > 0 {
		t.Fatalf("conflicts: %v", plan.Conflicts)
	}
	mounts := map[string]packFilesMount{}
	for _, m := range plan.Mounts {
		mounts[m.Dest] = m
	}

	execOf := func(p string) bool {
		t.Helper()
		fi, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode().Perm()&0o022 != 0 {
			t.Errorf("%s is group/world-writable: %04o", p, fi.Mode().Perm())
		}
		return fi.Mode().Perm()&0o111 != 0
	}

	script, ok := mounts[".pi/agent/bin/muse-planner-exec.sh"]
	if !ok {
		t.Fatalf("no mount for the shipped script; plan = %+v", plan.Mounts)
	}
	if !execOf(script.Src) {
		t.Errorf("the shipped pack's script %s is not executable in the jail", script.Src)
	}
	if def := mounts[".pi/agent/agents/muse-planner.md"]; def.Src == "" || execOf(def.Src) {
		t.Errorf("the shipped pack's non-executable definition gained an exec bit (or is missing): %+v", def)
	}

	dir, ok := mounts[".config/hookpack"]
	// Two contributions under one root: the mount is the per-launch MERGED copy, which is
	// copyOneFile's output rather than packstage's.
	if want := filepath.Join(agentsPath, filesStagingName(".config/hookpack")); !ok || !dir.IsDir || dir.Src != want {
		t.Fatalf("no merged directory mount at %s for the by-path pack; plan = %+v", want, plan.Mounts)
	}
	if !execOf(filepath.Join(dir.Src, "extra.sh")) {
		t.Error("the by-path pack's second script lost its exec bit in the merged copy")
	}
	if !execOf(filepath.Join(dir.Src, "session-start.sh")) {
		t.Error("the by-path pack's hook script lost its exec bit in the merged copy")
	}
	if execOf(filepath.Join(dir.Src, "README.md")) {
		t.Error("the by-path pack's README gained an exec bit in the merged copy")
	}
}
