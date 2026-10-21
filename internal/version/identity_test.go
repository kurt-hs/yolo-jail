package version

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var hex64 = regexp.MustCompile(`^[0-9a-f]{64}$`)

// TestHashFileIsSHA256OfTheBytes pins the digest against the published SHA-256 of "abc".
func TestHashFileIsSHA256OfTheBytes(t *testing.T) {
	p := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(p, []byte("abc"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := HashFile(p)
	if err != nil {
		t.Fatal(err)
	}
	const want = "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
	if got != want {
		t.Fatalf("HashFile = %s, want %s", got, want)
	}
}

func TestHashFileMissingPathIsAnError(t *testing.T) {
	if _, err := HashFile(filepath.Join(t.TempDir(), "absent")); err == nil {
		t.Fatal("HashFile of a missing path returned no error")
	}
}

// TestBinaryLineReportsTheReasonWhenTheDigestIsUnavailable pins the failure form the
// `--full` output names, both when the executable cannot be found and when it cannot be read.
func TestBinaryLineReportsTheReasonWhenTheDigestIsUnavailable(t *testing.T) {
	noExe := func() (string, error) { return "", errors.New("no executable") }
	if got := binaryLine(noExe, HashFile); got != "binary sha256: unavailable (no executable)" {
		t.Fatalf("resolve failure line = %q", got)
	}
	exe := func() (string, error) { return "/x", nil }
	unreadable := func(string) (string, error) { return "", errors.New("permission denied") }
	if got := binaryLine(exe, unreadable); got != "binary sha256: unavailable (permission denied)" {
		t.Fatalf("hash failure line = %q", got)
	}
}

func TestBinaryLineIsTheDigestWhenItIsAvailable(t *testing.T) {
	exe := func() (string, error) { return "/x", nil }
	digest := func(string) (string, error) { return strings.Repeat("a", 64), nil }
	if got := binaryLine(exe, digest); got != "binary sha256: "+strings.Repeat("a", 64) {
		t.Fatalf("line = %q", got)
	}
}

// TestFullLinesNameTheCommitAndADigestOfTheRunningBinary checks the real path end to end: the
// test binary is the running executable, so its digest must be a real 64-hex value.
func TestFullLinesNameTheCommitAndADigestOfTheRunningBinary(t *testing.T) {
	origCommit, origBuild, origIdentity, origKind := GitCommit, buildVersion, SnapshotIdentity, BuildKind
	t.Cleanup(func() {
		GitCommit, buildVersion, SnapshotIdentity, BuildKind = origCommit, origBuild, origIdentity, origKind
	})

	GitCommit, buildVersion, SnapshotIdentity, BuildKind = "ab9cf2f90", "0.13.0-40-gab9cf2f90", "", ""
	lines := FullLines()
	if len(lines) != 5 {
		t.Fatalf("FullLines = %q, want 5 lines", lines)
	}
	if lines[0] != "build stamp: 0.13.0-40-gab9cf2f90" {
		t.Errorf("build stamp line = %q", lines[0])
	}
	if lines[1] != "commit: ab9cf2f90" {
		t.Errorf("commit line = %q", lines[1])
	}
	if lines[3] != "snapshot identity: unstamped" || lines[4] != "build kind: unstamped" {
		t.Errorf("bundle metadata lines = %q, want unstamped values", lines[3:5])
	}
	sha, ok := strings.CutPrefix(lines[2], "binary sha256: ")
	if !ok || !hex64.MatchString(sha) {
		t.Fatalf("binary sha256 line = %q, want 64 hex digits", lines[2])
	}
}

func TestFullLinesExposeMatchedBundleMetadata(t *testing.T) {
	origIdentity, origKind := SnapshotIdentity, BuildKind
	t.Cleanup(func() { SnapshotIdentity, BuildKind = origIdentity, origKind })
	SnapshotIdentity, BuildKind = strings.Repeat("a", 64), "development"
	lines := FullLines()
	if lines[3] != "snapshot identity: "+SnapshotIdentity {
		t.Errorf("snapshot identity line = %q", lines[3])
	}
	if lines[4] != "build kind: development" {
		t.Errorf("build kind line = %q", lines[4])
	}
}

func TestUnstampedBuildSaysUnknownAndUnstamped(t *testing.T) {
	origCommit, origBuild := GitCommit, buildVersion
	t.Cleanup(func() { GitCommit, buildVersion = origCommit, origBuild })

	GitCommit, buildVersion = "", ""
	if got := CommitLabel(); got != "unknown" {
		t.Errorf("CommitLabel = %q, want unknown", got)
	}
	if got := StampLabel(); got != "unstamped" {
		t.Errorf("StampLabel = %q, want unstamped", got)
	}
	GitCommit = "unknown"
	if got := CommitLabel(); got != "unknown" {
		t.Errorf("CommitLabel of the legacy literal = %q, want unknown", got)
	}
}
