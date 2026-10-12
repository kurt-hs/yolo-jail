package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/capturelog"
)

// The two host-side writers of a capture overlay — the capture (`yolo config capture` and the
// fold at shutdown, both through captureSurface) and `yolo config reset` — each append what they
// changed to the capture log beside the overlay, so the next investigation can read which keys
// were captured and what they held.
func TestCaptureAndResetWriteTheCaptureLog(t *testing.T) {
	home := withScratchHome(t)
	tgt, dir := withLocalSidecarDir(t)
	s, ok := surfaceManifest().Lookup("claude", "settings")
	if !ok {
		t.Fatal("missing claude/settings")
	}
	settings := filepath.Join(home, ".claude", "settings.json")
	writeFile(t, settings, `{"model":"base","env":{"MY_SETTING":"1"}}`)
	writeSidecar(t, dir, "claude", "settings", "", `{"model":"base"}`)

	if _, err := captureSurface(tgt, s); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(dir, capturelog.FileName)
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("the capture wrote no log: %v", err)
	}
	if want := `claude/settings capture: + env.MY_SETTING = "1" (captured)`; !strings.Contains(string(data), want) {
		t.Errorf("capture.log is missing %q:\n%s", want, data)
	}

	var out, errw bytes.Buffer
	if rc := configReset(tgt, []string{"claude/settings"}, &out, &errw, false); rc != 0 {
		t.Fatalf("reset rc=%d: %s", rc, errw.String())
	}
	data, _ = os.ReadFile(logPath)
	if want := "claude/settings reset: - env.MY_SETTING (was \"1\"): removed by `yolo config reset`"; !strings.Contains(string(data), want) {
		t.Errorf("capture.log is missing %q:\n%s", want, data)
	}
}
