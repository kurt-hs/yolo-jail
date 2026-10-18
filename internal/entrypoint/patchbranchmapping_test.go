package entrypoint

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/packload"
	"github.com/mschulkind-oss/yolo-jail/internal/packsrc"
)

func TestPackTreeMapsHostPatchBranchIdentityIntoASeparateGuestCheckout(t *testing.T) {
	hostWorkspace := filepath.Join(t.TempDir(), "host-checkout")
	if err := os.MkdirAll(hostWorkspace, 0o755); err != nil {
		t.Fatal(err)
	}
	gitTest(t, hostWorkspace, "init", "-q")
	gitTest(t, hostWorkspace, "config", "user.email", "test@example.invalid")
	gitTest(t, hostWorkspace, "config", "user.name", "Test")
	forkSource := filepath.Join(hostWorkspace, "yolo-packs", "forkpack")
	if err := os.MkdirAll(forkSource, 0o755); err != nil {
		t.Fatal(err)
	}
	writePackManifest(t, filepath.Join(forkSource, "pack.json"), `{"name":"forkpack","contributes":[{"kind":"program","bin":"tool","via":"source","fork_of":"basepack","source":"git+https://example.invalid/upstream?ref=main","patch_branch":"../..?ref=mine","build":"sh build.sh","produces":[".local/bin/tool"]}]}`)
	writePackManifest(t, filepath.Join(hostWorkspace, "yolo-packs", "basepack", "pack.json"), `{"name":"basepack","contributes":[{"kind":"program","bin":"tool","via":"npm","package":"tool"}]}`)
	gitTest(t, hostWorkspace, "add", "-A")
	gitTest(t, hostWorkspace, "commit", "-qm", "host checkout")
	gitTest(t, hostWorkspace, "branch", "mine")

	guestWorkspace := filepath.Join(t.TempDir(), "workspace")
	gitTest(t, filepath.Dir(guestWorkspace), "clone", "-q", hostWorkspace, guestWorkspace)
	guestFork := filepath.Join(guestWorkspace, "yolo-packs", "forkpack")
	hostIdentity, err := packsrc.RelativeRepositoryIdentity("../..?ref=mine", forkSource)
	if err != nil {
		t.Fatal(err)
	}
	guestIdentity, err := packsrc.RelativeRepositoryIdentity("../..?ref=mine", guestFork)
	if err != nil {
		t.Fatal(err)
	}
	if hostIdentity == guestIdentity {
		t.Fatalf("fixture did not distinguish host and guest filesystem identities: %q", hostIdentity)
	}
	hostAddress, err := packsrc.ParseFrom("../..?ref=mine", forkSource)
	if err != nil {
		t.Fatal(err)
	}
	guestAddress, err := packsrc.ParseFrom("../..?ref=mine", guestFork)
	if err != nil {
		t.Fatal(err)
	}
	if hostAddress.Repo == guestAddress.Repo {
		t.Fatalf("fixture did not map host checkout %q to a distinct guest path %q", hostAddress.Repo, guestAddress.Repo)
	}

	tree := filepath.Join(t.TempDir(), "pack-tree")
	stagedFork := filepath.Join(tree, "forkpack")
	stagedBase := filepath.Join(tree, "basepack")
	if err := os.MkdirAll(stagedFork, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(stagedBase, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"forkpack", "basepack"} {
		source := filepath.Join(hostWorkspace, "yolo-packs", name, "pack.json")
		dest := filepath.Join(tree, name, "pack.json")
		data, err := os.ReadFile(source)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dest, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	fork, problems := packload.LoadDir(stagedFork, "forkpack")
	if len(problems) > 0 {
		t.Fatal(problems)
	}
	fork.SourceRoot = forkSource
	base, problems := packload.LoadDir(stagedBase, "basepack")
	if len(problems) > 0 {
		t.Fatal(problems)
	}
	if err := packload.WritePackTreeRecordForWorkspace(tree, []*packload.Pack{base, fork}, hostWorkspace); err != nil {
		t.Fatal(err)
	}

	e := NewEnv(map[string]string{"YOLO_PACK_ROOT": tree, "YOLO_WORKSPACE": guestWorkspace})
	packs, err := loadPackRootAsStaged(e, tree)
	if err != nil {
		t.Fatal(err)
	}
	var guestForkDecl packload.Fork
	for _, f := range packload.Forks(packs) {
		if f.Key() == "forkpack/tool" {
			guestForkDecl = f
		}
	}
	if guestForkDecl.Key() == "" {
		t.Fatal("guest loader did not retain selected forkpack")
	}
	if guestForkDecl.PatchBranchDir != guestFork {
		t.Errorf("guest branch directory = %q, want its workspace-mapped source %q", guestForkDecl.PatchBranchDir, guestFork)
	}
	inputs, _, _, err := guestForkDecl.CheckWant(nil).Inputs()
	if err != nil {
		t.Fatal(err)
	}
	if inputs.PatchBranchRepo != hostIdentity {
		t.Errorf("guest record identity = %q, want host-established identity %q (guest filesystem identity %q)",
			inputs.PatchBranchRepo, hostIdentity, guestIdentity)
	}

	unrelatedWorkspace := filepath.Join(t.TempDir(), "unrelated-workspace")
	if err := packload.WritePackTreeRecordForWorkspace(tree, []*packload.Pack{base, fork}, unrelatedWorkspace); err != nil {
		t.Fatal(err)
	}
	entries, recorded, err := packload.ReadPackTreeRecord(tree)
	if err != nil || !recorded {
		t.Fatalf("read unmapped pack-tree record: recorded=%v err=%v", recorded, err)
	}
	var mapping packload.PatchBranchMapping
	for _, entry := range entries {
		if entry.Name == "forkpack" {
			mapping = entry.PatchBranches["forkpack/"]
		}
	}
	if mapping.Repository != hostIdentity || mapping.Mapped || mapping.Directory != "" {
		t.Fatalf("unmapped host repository record = %+v (entries=%+v), want identity only and no host path", mapping, entries)
	}
	unmappedPacks, err := loadPackRootAsStaged(NewEnv(map[string]string{
		"YOLO_PACK_ROOT": tree, "YOLO_WORKSPACE": guestWorkspace,
	}), tree)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range packload.Forks(unmappedPacks) {
		if f.Key() == "forkpack/tool" {
			if f.PatchBranchRepoIdentity != hostIdentity {
				t.Errorf("unmapped guest lost host identity: got %q, want %q", f.PatchBranchRepoIdentity, hostIdentity)
			}
			if f.PatchBranchDir == hostWorkspace || f.PatchBranchDir == forkSource {
				t.Errorf("unmapped guest gained access to host repository path %q", f.PatchBranchDir)
			}
			return
		}
	}
	t.Fatal("unmapped guest loader did not retain selected forkpack")
}

func gitTest(t *testing.T, dir string, args ...string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func writePackManifest(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}
