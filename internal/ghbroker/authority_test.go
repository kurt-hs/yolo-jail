package ghbroker

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/brokerscope"
	"github.com/mschulkind-oss/yolo-jail/internal/brokerstore"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

func authorityFixture(t *testing.T) (*Authority, brokerstore.Request) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("YOLO_USER_LAYER", "")
	sf := brokerscope.File{Source: Source, LaunchID: "fixture", PID: os.Getpid(), Container: "fixture-container", Workspace: "/fixture", Repos: []string{"o/r"}}
	path, err := brokerscope.Write(sf)
	if err != nil {
		t.Fatal(err)
	}
	a, err := BindAuthority(path, sf, "fresh-start")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	d, p, err := a.Classify([]string{"pr", "comment", "1", "--body", "hello"}, "o/r")
	if err != nil {
		t.Fatal(err)
	}
	q := brokerstore.Request{Jail: paths.JailShortHash(sf.Container), Start: "fresh-start", Workspace: sf.Workspace, LaunchID: sf.LaunchID, Argv: d.Argv, Repo: "o/r", Set: d.Set, Policy: p, StdinDigest: "stdin"}
	return a, q
}
func TestHostAuthorityCurrentPolicyScopeAndStart(t *testing.T) {
	a, q := authorityFixture(t)
	if _, err := ApprovalAuthority(q); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Dir(paths.UserConfigPath())
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.UserConfigPath(), []byte(`{"brokered":{"github":{"sets":[{"name":"read-write","mode":"windowed","window":"1m","max_window":"2m","uses":2}]}}}`), 0600); err != nil {
		t.Fatal(err)
	}
	p, err := a.Revalidate(q)
	if err != nil || p.MaxWindow != 2*time.Minute || p.Uses != 2 {
		t.Fatalf("%+v %v", p, err)
	}
	if err := os.WriteFile(paths.UserConfigPath(), []byte(`{"brokered":{"github":{"sets":[{"name":"read-write","mode":"windowed","typo":true}]}}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Revalidate(q); err == nil {
		t.Fatal("unknown policy accepted")
	}
	os.Remove(paths.UserConfigPath())
	q.Start = "old-start"
	if _, err := ApprovalAuthority(q); err == nil {
		t.Fatal("stale start accepted")
	}
	q.Start = "fresh-start"
	if err := brokerscope.Remove(a.path); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Revalidate(q); err == nil {
		t.Fatal("torn down scope accepted")
	}
}
func TestHostAuthorityActualInvocationAndHostileRules(t *testing.T) {
	a, q := authorityFixture(t)
	for _, args := range [][]string{{"auth", "token"}, {"pr", "comment", "1", "--body-file", "/etc/passwd"}, {"pr", "comment", "1", "--unknown"}} {
		if _, _, err := a.Classify(args, "o/r"); err == nil {
			t.Fatalf("hostile accepted: %v", args)
		}
	}
	if _, _, err := a.Classify(q.Argv, "other/repo"); err != nil { /* canonical argv pins o/r, test explicit repository instead */
	}
	if _, _, err := a.Classify([]string{"pr", "comment", "1", "-R", "other/repo", "--body", "hello"}, "o/r"); err == nil {
		t.Fatal("actual repo not checked")
	}
}
