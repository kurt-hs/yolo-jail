package entrypoint

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/jailcontent"
	"github.com/mschulkind-oss/yolo-jail/internal/logcap"
	"github.com/mschulkind-oss/yolo-jail/internal/provision"
	"github.com/mschulkind-oss/yolo-jail/internal/shquote"
	"github.com/mschulkind-oss/yolo-jail/internal/testutil"
)

func TestGeneratedReceiptRetentionAndConcurrentGoAppend(t *testing.T) {
	bin := testutil.YoloBinaryDir(t)
	ws := t.TempDir()
	path := filepath.Join(ws, ".yolo", "receipts.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	old := bytes.Repeat([]byte("{\"old\":true}\n"), logcap.MaxBytes/13+100)
	if err := os.WriteFile(path, old, 0600); err != nil {
		t.Fatal(err)
	}
	script := "set -euo pipefail\n_YOLO_RECEIPTS=" + shquote.Quote(path) + "\n" + receiptShellFns + `
for i in {1..20}; do _yolo_receipt '{"schema":1,"kind":"shell"' "" "" "" install; done
`
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cmd := exec.Command("bash", "-c", script)
			cmd.Env = append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"))
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Errorf("shell: %v\n%s", err, out)
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 80; i++ {
			if err := AppendReceiptLine(path, `{"kind":"go"}`); err != nil {
				t.Error(err)
				return
			}
		}
	}()
	wg.Wait()
	active, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	archive, err := os.ReadFile(path + ".1")
	if err != nil {
		t.Fatal(err)
	}
	if len(archive) > logcap.MaxBytes {
		t.Fatalf("archive %d", len(archive))
	}
	if bytes.Count(active, []byte(`"kind":"shell"`)) != 80 || bytes.Count(active, []byte(`"kind":"go"`)) != 80 {
		t.Fatalf("lost simultaneous records: %s", active)
	}
	if bytes.Contains(active, []byte(`"old"`)) || !bytes.Contains(archive, []byte(`"old"`)) {
		t.Fatal("oversized history was not retained in archive")
	}
}

func TestGeneratedStartupRetentionStreamingAndRefusal(t *testing.T) {
	bin := testutil.YoloBinaryDir(t)
	ws := t.TempDir()
	path := provision.StartupLog(ws)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, bytes.Repeat([]byte("old history\n"), logcap.MaxBytes/12+1000), 0600); err != nil {
		t.Fatal(err)
	}
	// The first stderr bytes must arrive before setup is allowed to finish.
	release := filepath.Join(ws, "release")
	body := fmt.Sprintf("printf 'streamed now'; while [ ! -e %s ]; do sleep 0.01; done; exit %d", shquote.Quote(release), provision.RefusedStatus)
	cmd := exec.Command("bash", "-c", provision.Script(path, body, false))
	cmd.Env = append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"))
	stderr, err := cmd.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Process.Kill()
	first := make([]byte, len("streamed now"))
	if _, err := io.ReadFull(stderr, first); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(first, []byte("streamed")) {
		t.Fatalf("stderr not streamed: %q", first)
	}
	if err := os.WriteFile(release, nil, 0600); err != nil {
		t.Fatal(err)
	}
	// Drain the refusal message before waiting.
	var rest bytes.Buffer
	_, _ = rest.ReadFrom(stderr)
	err = cmd.Wait()
	if ee, ok := err.(*exec.ExitError); !ok || ee.ExitCode() != provision.RefusedStatus {
		t.Fatalf("setup refusal status: %v", err)
	}
	active, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	archive, err := os.ReadFile(path + ".1")
	if err != nil {
		t.Fatal(err)
	}
	if len(archive) > logcap.MaxBytes || !bytes.Contains(archive, []byte("old history")) {
		t.Fatal("startup history not retained")
	}
	if !bytes.Contains(active, []byte("streamed now")) || !bytes.Contains(active, []byte(provision.FailedMarker+fmt.Sprintf(" (exit %d)", provision.RefusedStatus))) {
		t.Fatalf("missing output/marker: %s", active)
	}
}

func TestGeneratedStartupTrimsDuringSetupAndKeepsFailureMarker(t *testing.T) {
	bin := testutil.YoloBinaryDir(t)
	ws := t.TempDir()
	output := filepath.Join(ws, "installer-output")
	payload := bytes.Repeat([]byte("installer progress\n"), logcap.MaxBytes/19+5000)
	if err := os.WriteFile(output, payload, 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", "-c", provision.Script(provision.StartupLog(ws), "cat "+shquote.Quote(output)+"; exit 23", false))
	cmd.Env = append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("ordinary setup failure must still continue: %v", err)
	}
	if !bytes.HasPrefix(out, payload) {
		t.Fatal("tee changed or withheld setup output")
	}
	active, err := os.ReadFile(provision.StartupLog(ws))
	if err != nil {
		t.Fatal(err)
	}
	archive, err := os.ReadFile(provision.StartupLog(ws) + ".1")
	if err != nil {
		t.Fatal(err)
	}
	if len(archive) > logcap.MaxBytes || len(active) > logcap.MaxBytes+32<<10 {
		t.Fatalf("retention: active %d archive %d", len(active), len(archive))
	}
	if !bytes.Contains(active, []byte(provision.FailedMarker+" (exit 23)")) {
		t.Fatal("setup failure marker was lost after trim")
	}
}

func TestGeneratedStartupHealthyRunClearsPreviousFailure(t *testing.T) {
	bin := testutil.YoloBinaryDir(t)
	ws := t.TempDir()
	for _, setup := range []string{"exit 23", "echo healthy setup"} {
		cmd := exec.Command("bash", "-c", provision.Script(provision.StartupLog(ws), setup, false))
		cmd.Env = append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"))
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("setup: %v\n%s", err, out)
		}
		failed := jailcontent.ReadProvisioningFailed(ws)
		if failed != (setup == "exit 23") {
			t.Fatalf("production failure reader after %q: %v", setup, failed)
		}
	}
}
