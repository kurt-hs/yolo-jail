package integration

import (
	"encoding/json"
	"os"
	"os/exec"
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
	args := append(append(jailRunArgs(), "--"), "yolo", "--version", "--full")
	r := runCommand(t, dir, args, withHostSemantics())
	if r.rc != 0 {
		t.Fatalf("yolo --version --full in the jail: rc=%d\n%s", r.rc, r.combined())
	}
	hostCmd := exec.Command(yoloBin, "--version", "--full")
	hostCmd.Env = envWithout("YOLO_VERSION")
	hostVersionOutput, err := hostCmd.Output()
	if err != nil {
		t.Fatalf("matched host yolo --version --full: %v", err)
	}
	hostOutput := string(hostVersionOutput)
	hostVersion := versionToken(t, hostOutput)
	guestVersion := versionToken(t, r.stdout)
	if guestVersion != hostVersion {
		t.Errorf("the jail's matched yolo version = %q, want host bundle version %q", guestVersion, hostVersion)
	}
	if !strings.Contains(hostVersion, "+") || !strings.HasSuffix(hostVersion, ".g"+wantCommit) {
		t.Errorf("matched version %q does not encode stamped commit %q in its build metadata", hostVersion, wantCommit)
	}
	if hostHash, guestHash := versionField(t, hostOutput, "binary sha256: "), versionField(t, r.stdout, "binary sha256: "); hostHash != guestHash {
		t.Errorf("guest binary sha256 %q does not match paired host binary sha256 %q", guestHash, hostHash)
	}
	for _, output := range []struct {
		name string
		text string
	}{{"host", hostOutput}, {"guest", r.stdout}} {
		for _, want := range []string{"\ncommit: " + wantCommit + "\n",
			"\nsnapshot identity: " + stamp.SnapshotIdentity + "\n", "\nbuild kind: development\n"} {
			if !strings.Contains(output.text, want) {
				t.Errorf("the %s yolo does not carry matched bundle field %q:\n%s", output.name, strings.Trim(want, "\n"), output.text)
			}
		}
	}
}

func versionField(t *testing.T, output, label string) string {
	t.Helper()
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, label) {
			return strings.TrimSpace(strings.TrimPrefix(line, label))
		}
	}
	t.Fatalf("version output has no %q field:\n%s", label, output)
	return ""
}

func versionToken(t *testing.T, output string) string {
	t.Helper()
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "yolo-jail" {
			return fields[1]
		}
	}
	t.Fatalf("version output has no yolo-jail version line:\n%s", output)
	return ""
}
