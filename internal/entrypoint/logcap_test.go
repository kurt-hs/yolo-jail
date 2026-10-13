package entrypoint

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/logcap"
	"github.com/mschulkind-oss/yolo-jail/internal/perf"
)

func seedDiagnosticLog(t *testing.T, path string) {
	t.Helper()
	line := []byte("earlier diagnostic line\n")
	if err := os.WriteFile(path, bytes.Repeat(line, logcap.MaxBytes/len(line)+2), 0o644); err != nil {
		t.Fatal(err)
	}
}

func assertDiagnosticLogCapped(t *testing.T, path string) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) > logcap.MaxBytes || bytes.Contains(b, []byte("earlier diagnostic")) {
		t.Fatalf("active log was not trimmed: %s (%d bytes)", path, len(b))
	}
	b, err = os.ReadFile(path + logcap.ArchiveSuffix)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) > logcap.MaxBytes || !bytes.Contains(b, []byte("earlier diagnostic")) || b[len(b)-1] != '\n' {
		t.Fatal("archive lost whole diagnostic lines or exceeded cap")
	}
}

func TestAppendReceiptLineCapsItsLog(t *testing.T) {
	path := filepath.Join(t.TempDir(), "receipts.jsonl")
	seedDiagnosticLog(t, path)
	line := CaptureReceipt{Bin: "tool", Act: "install"}.Line()
	if err := AppendReceiptLine(path, line); err != nil {
		t.Fatal(err)
	}
	assertDiagnosticLogCapped(t, path)
	b, _ := os.ReadFile(path)
	if string(b) != line+"\n" {
		t.Fatalf("fresh receipt lost: %q", b)
	}
}

func TestSharedHookDecisionCapsItsLog(t *testing.T) {
	e := &Env{Home: t.TempDir(), Stderr: &strings.Builder{}}
	path := filepath.Join(e.Home, sharedCredsLog)
	seedDiagnosticLog(t, path)
	e.logSharedHook("shared_credentials", "example", "auth.json", "shared", "fresh decision")
	assertDiagnosticLogCapped(t, path)
	b, _ := os.ReadFile(path)
	if !bytes.Contains(b, []byte("fresh decision")) {
		t.Fatal("new decision lost")
	}
}

func TestContainerForwardingCapsItsLog(t *testing.T) {
	port, _ := holdPort(t)
	var warnings strings.Builder
	e := forwardEnv(t, &warnings, port, port)
	path := filepath.Join(e.Home, ".yolo-socat.log")
	seedDiagnosticLog(t, path)
	startContainerPortForwarding(e)
	assertDiagnosticLogCapped(t, path)
}

func TestPerfDumpRetainsNewestRunsThroughReadableDescriptor(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".yolo-perf.log")
	old := "=== YOLO oldest run\n"
	seed := old + strings.Repeat("=== YOLO recent run\n", perf.MaxRuns-1)
	if err := os.WriteFile(path, []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}
	p := &perfLog{entries: []perfEntry{{}}}
	p.dump(home)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(b), bootPerfRunPrefix) != perf.MaxRuns || bytes.Contains(b, []byte("oldest run")) || !bytes.Contains(b, []byte("Entrypoint Perf")) {
		t.Fatalf("run retention failed: %s", b)
	}
}
