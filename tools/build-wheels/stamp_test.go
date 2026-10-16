package main

import "testing"

// TestWheelBinariesNameTheReleaseCommit: a wheel's binaries print <version>+g<commit>,
// the release form goreleaser and the Homebrew formula stamp (OQ-DA2, ruled
// 2026-10-16), and carry the commit for `--version --full`.
func TestWheelBinariesNameTheReleaseCommit(t *testing.T) {
	got := stampLdflags("0.14.0", "0123456789ab")
	want := "-s -w -X " + versionPkg + ".buildVersion=0.14.0+g0123456789ab -X " +
		versionPkg + ".GitCommit=0123456789ab"
	if got != want {
		t.Errorf("stampLdflags = %q\nwant          %q", got, want)
	}
	if got := stampLdflags("0.14.0", ""); got != "-s -w -X "+versionPkg+".buildVersion=0.14.0" {
		t.Errorf("with no commit known, stampLdflags = %q, want the plain version", got)
	}
}
