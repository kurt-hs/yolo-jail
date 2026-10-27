package configedit

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/configdoc"
)

func TestConfigEditApplySerializesIndependentProcesses(t *testing.T) {
	if path := os.Getenv("CONFIGEDIT_HELPER_PATH"); path != "" {
		item := os.Getenv("CONFIGEDIT_HELPER_ITEM")
		_, err := Apply(path, configdoc.JSONC, Mutation{Action: Append, Path: []configdoc.Step{{Key: "packs"}}, Value: item}, ApplyOptions{})
		if err != nil {
			t.Fatal(err)
		}
		return
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "config.jsonc")
	if err := os.WriteFile(path, []byte(`{"packs":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, item := range []string{"first", "second"} {
		wg.Add(1)
		go func(item string) {
			defer wg.Done()
			cmd := exec.Command(os.Args[0], "-test.run=^TestConfigEditApplySerializesIndependentProcesses$")
			cmd.Env = append(os.Environ(), "CONFIGEDIT_HELPER_PATH="+path, "CONFIGEDIT_HELPER_ITEM="+item)
			if out, err := cmd.CombinedOutput(); err != nil {
				errs <- errors.New(string(out) + ": " + err.Error())
			}
		}(item)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := configdoc.Parse(data, configdoc.JSONC)
	if err != nil {
		t.Fatalf("concurrent result invalid: %v\n%s", err, data)
	}
	v, _ := doc.Values.Get("packs")
	items, ok := v.([]any)
	if !ok || len(items) != 2 {
		t.Fatalf("concurrent edits lost an update: %#v", v)
	}
}

func TestConfigEditApplyIOFailuresLeaveOrRestoreOriginal(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.jsonc")
	before := []byte(`{"packs":[]}`)
	if err := os.WriteFile(path, before, 0600); err != nil {
		t.Fatal(err)
	}
	mutation := Mutation{Action: Append, Path: []configdoc.Step{{Key: "packs"}}, Value: "x"}
	ops := systemFileOps
	ops.WriteAtomic = func(string, []byte, os.FileMode) error { return os.ErrPermission }
	if _, err := applyWithOps(path, configdoc.JSONC, mutation, ApplyOptions{}, ops); err == nil || !strings.Contains(err.Error(), "write config target") {
		t.Fatalf("write failure not reported: %v", err)
	}
	got, _ := os.ReadFile(path)
	if !bytes.Equal(got, before) {
		t.Fatalf("failed write changed preimage: %q", got)
	}

	ops = systemFileOps
	reads := 0
	ops.ReadFile = func(p string) ([]byte, error) {
		reads++
		if reads == 2 {
			return nil, os.ErrPermission
		}
		return os.ReadFile(p)
	}
	if _, err := applyWithOps(path, configdoc.JSONC, mutation, ApplyOptions{}, ops); err == nil || !strings.Contains(err.Error(), "original bytes restored") {
		t.Fatalf("post-write read failure did not restore: %v", err)
	}
	got, _ = os.ReadFile(path)
	if !bytes.Equal(got, before) {
		t.Fatalf("read failure left modified bytes: %q", got)
	}

	ops = systemFileOps
	calls := 0
	ops.WriteAtomic = func(p string, data []byte, mode os.FileMode) error {
		calls++
		if calls == 1 {
			return writeAtomic(p, data, mode)
		}
		return os.ErrPermission
	}
	if _, err := applyWithOps(path, configdoc.JSONC, mutation, ApplyOptions{Verify: func([]byte, []byte) error { return errors.New("deliberate mismatch") }}, ops); err == nil || !strings.Contains(err.Error(), "restoring the original bytes also failed") {
		t.Fatalf("restore failure not actionable: %v", err)
	}
}

func TestConfigEditApplyPreservesReadOnlyTargetMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "readonly.toml")
	if err := os.WriteFile(path, []byte("packs = []\n"), 0444); err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(path, configdoc.TOML, Mutation{Action: Append, Path: []configdoc.Step{{Key: "packs"}}, Value: "one"}, ApplyOptions{}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0444 {
		t.Fatalf("mode changed to %o", info.Mode().Perm())
	}
}

func TestConfigEditApplyRejectsRenameFailureWithoutChangingTarget(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	before := []byte("packs = []\n")
	if err := os.WriteFile(path, before, 0600); err != nil {
		t.Fatal(err)
	}
	ops := systemFileOps
	ops.WriteAtomic = func(string, []byte, os.FileMode) error { return errors.New("rename: permission denied") }
	_, err := applyWithOps(path, configdoc.TOML, Mutation{Action: Append, Path: []configdoc.Step{{Key: "packs"}}, Value: "x"}, ApplyOptions{}, ops)
	if err == nil || !strings.Contains(err.Error(), "rename") {
		t.Fatalf("rename failure missing: %v", err)
	}
	got, _ := os.ReadFile(path)
	if !bytes.Equal(got, before) {
		t.Fatalf("rename failure modified target: %q", got)
	}
}
