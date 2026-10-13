package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/capture"
	"github.com/mschulkind-oss/yolo-jail/internal/entrypoint"
)

func TestProgramCaptureMaterializeExactCompatibilityAcrossWorkspaces(t *testing.T) {
	store := &capture.Store{Dir: t.TempDir()}
	firstHome := t.TempDir()
	makeEntry := func(id, body string) *capture.Entry {
		t.Helper()
		staged, err := store.Stage(id)
		if err != nil {
			t.Fatal(err)
		}
		_, err = capture.Run(capture.Options{Home: t.TempDir(), Out: staged, Artifacts: []string{".local/bin/probe"}, ScanContentRefs: true, Command: []string{"/bin/sh", "-c", `mkdir -p "$HOME/.local/bin"; printf %s ` + body + ` > "$HOME/.local/bin/probe"`}})
		if err != nil {
			t.Fatal(err)
		}
		e, err := store.AdmitEntry(staged)
		if err != nil {
			t.Fatal(err)
		}
		return e
	}
	old := makeEntry("old", "old")
	const identity = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	const changed = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	record := func(e *capture.Entry, compatibility string, when time.Time) {
		t.Helper()
		r := entrypoint.CaptureReceipt{Bin: "probe", Platform: capture.Platform(), Key: e.Key, Compatibility: compatibility, Time: when}
		if err := entrypoint.AppendReceiptLine(capture.ReceiptsPath(e.Root), r.Line()); err != nil {
			t.Fatal(err)
		}
	}
	record(old, identity, time.Unix(2, 0))
	newer := makeEntry("new", "new")
	record(newer, "", time.Unix(3, 0))
	record(newer, changed, time.Unix(4, 0))
	for _, home := range []string{firstHome, t.TempDir()} {
		if rc := runInternal([]string{"capture-materialize", "--store=" + store.Dir, "--home=" + home, "--bin=probe", "--compatibility=" + identity}); rc != 0 {
			t.Fatalf("exit %d", rc)
		}
		got, err := os.ReadFile(filepath.Join(home, ".local/bin/probe"))
		if err != nil || string(got) != "old" {
			t.Fatalf("%q %v", got, err)
		}
	}
	// A newer incompatible admission does not touch a running copy; legacy lookup stays separate.
	legacy, _, err := resolveCaptureFor(store, "probe", capture.Platform())
	if err != nil || legacy.Key != newer.Key {
		t.Fatalf("legacy lookup changed: %v %v", legacy, err)
	}
	missing := t.TempDir()
	if rc := runInternal([]string{"capture-materialize", "--store=" + store.Dir, "--home=" + missing, "--bin=probe", "--compatibility=cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"}); rc == 0 {
		t.Fatal("incompatible receipt satisfied request")
	}
	if _, err := os.Stat(filepath.Join(missing, ".local")); !os.IsNotExist(err) {
		t.Fatal("miss wrote output")
	}
	if _, err := capture.PruneSupersededCaptures(store.Dir, captureRecords, true); err != nil {
		t.Fatal(err)
	}
	if !old.Complete() || !newer.Complete() {
		t.Fatal("prune removed a compatible identity's winner")
	}
}

func TestProgramCaptureDriverRequiresSealAndCapturesOnlyArtifactArgv(t *testing.T) {
	home := t.TempDir()
	out := filepath.Join(t.TempDir(), "out")
	marker := filepath.Join(home, "installer-started")
	argv := []string{"capture-run", "--home=" + home, "--out=" + out, "--scan-content-refs", "--artifact=.local/bin/probe", "--", "/bin/sh", "-c", `touch "$HOME/installer-started"; mkdir -p "$HOME/.local/bin" "$HOME/.local/share/login"; printf program > "$HOME/.local/bin/probe"; printf secret > "$HOME/.local/share/login/token"`}
	t.Setenv(entrypoint.SealedBuildEnv, "")
	if rc := runInternal(argv); rc == 0 {
		t.Fatal("unsealed artifact mode succeeded")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("unsealed route ran installer")
	}
	// The marker asserts the caller's contract, not runtime sealing. No test below
	// claims a temporary directory is a sealed jail.
	t.Setenv(entrypoint.SealedBuildEnv, "1")
	if rc := runInternal(argv); rc != 0 {
		t.Fatalf("driver exited %d", rc)
	}
	m, err := capture.ReadManifest(out)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range m.Entries {
		if strings.Contains(e.Path, "login") {
			t.Fatalf("login output captured: %+v", e)
		}
	}
}

func TestProgramCaptureMaterializeEmptyIdentityCannotSelectLegacy(t *testing.T) {
	for _, identity := range []string{"", "not-a-resolved-identity"} {
		if rc := runInternal([]string{"capture-materialize", "--store=" + t.TempDir(), "--home=" + t.TempDir(), "--bin=probe", "--compatibility=" + identity}); rc != 2 {
			t.Fatalf("identity %q: exit %d", identity, rc)
		}
	}
}
