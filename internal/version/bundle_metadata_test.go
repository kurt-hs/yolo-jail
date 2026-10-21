package version

import (
	"strings"
	"testing"
)

func TestBundleLinkerMetadataIsExposed(t *testing.T) {
	if SnapshotIdentity != "" && len(SnapshotIdentity) != 64 {
		t.Errorf("SnapshotIdentity = %q, want empty legacy value or lowercase SHA-256", SnapshotIdentity)
	}
	if SnapshotIdentity != strings.ToLower(SnapshotIdentity) {
		t.Errorf("SnapshotIdentity is not lowercase: %q", SnapshotIdentity)
	}
	if BuildKind != "" && BuildKind != "development" && BuildKind != "installed" {
		t.Errorf("BuildKind = %q, want empty legacy, development, or installed", BuildKind)
	}
}
