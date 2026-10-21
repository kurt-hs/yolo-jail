package integration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestTheJailsYoloCarriesItsMatchedBundleStamp pins the identity of the launcher
// TestMain built, not git metadata from the module build-input directory.
func TestTheJailsYoloCarriesItsMatchedBundleStamp(t *testing.T) {
	requireJail(t)
	data, err := os.ReadFile(filepath.Join(bundleRoot, "yolo-build-stamp.json"))
	if err != nil {
		t.Fatalf("read matched bundle stamp: %v", err)
	}
	var stamp struct {
		Commit           string `json:"commit"`
		SnapshotIdentity string `json:"snapshotIdentity"`
		BuildKind        string `json:"buildKind"`
	}
	if err := json.Unmarshal(data, &stamp); err != nil {
		t.Fatalf("decode matched bundle stamp: %v", err)
	}
	if stamp.Commit == "" || stamp.SnapshotIdentity == "" || stamp.BuildKind != "development" {
		t.Fatalf("TestMain did not select a stamped development bundle: %+v", stamp)
	}
	wantCommit := stamp.Commit
	if len(wantCommit) > 12 {
		wantCommit = wantCommit[:12]
	}

	dir := writeProject(t, "{}")
	r := runYoloDirect(t, dir, "yolo", "--version", "--full")
	if r.rc != 0 {
		t.Fatalf("yolo --version --full in the jail: rc=%d\n%s", r.rc, r.combined())
	}
	for _, want := range []string{"\ncommit: " + wantCommit + "\n",
		"\nsnapshot identity: " + stamp.SnapshotIdentity + "\n", "\nbuild kind: development\n"} {
		if !strings.Contains(r.stdout, want) {
			t.Errorf("the jail's /bin/yolo does not carry matched launcher field %q:\n%s", strings.Trim(want, "\n"), r.stdout)
		}
	}
	if !strings.Contains(r.stdout, "+g"+wantCommit) {
		t.Errorf("the jail's build stamp does not carry +g%s:\n%s", wantCommit, r.stdout)
	}
}
