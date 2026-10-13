package cgd

import (
	"bytes"
	"github.com/mschulkind-oss/yolo-jail/internal/logcap"
	"os"
	"testing"
)

func TestAuditAppendCapsItsLogAndPreservesPermissions(t *testing.T) {
	a := NewAuditor(t.TempDir(), func(s string) { t.Errorf("unexpected warning: %s", s) })
	line := []byte("earlier audit line\n")
	if err := os.WriteFile(a.Path(), bytes.Repeat(line, logcap.MaxBytes/len(line)+2), 0o600); err != nil {
		t.Fatal(err)
	}
	a.Append(AuditEvent{Op: "fresh", OK: true})
	b, err := os.ReadFile(a.Path())
	if err != nil {
		t.Fatal(err)
	}
	if len(b) > logcap.MaxBytes || bytes.Contains(b, line) || !bytes.Contains(b, []byte("op=fresh")) || a.Appended() != 1 {
		t.Fatal("audit append did not trim and record new event")
	}
	b, err = os.ReadFile(a.Path() + logcap.ArchiveSuffix)
	if err != nil || len(b) > logcap.MaxBytes || !bytes.Contains(b, line) {
		t.Fatalf("missing bounded archive: %v", err)
	}
	for _, path := range []string{a.Path(), a.Path() + logcap.ArchiveSuffix} {
		st, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if st.Mode().Perm() != 0o600 {
			t.Errorf("%s permissions: %v", path, st.Mode())
		}
	}
}
