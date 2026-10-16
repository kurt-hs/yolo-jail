package version

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Identity answers `yolo --version --full`: which build of the binary is running, beyond the
// one-line version, which two different binaries can share. Two facts, one per line:
//
//   - the commit the binary was built from, as the build stamped it (GitCommit, set by the
//     Justfile `install` recipe, scripts/build-go.sh and every flake build (flake.nix:
//     buildStamp); a plain `go build` stamps nothing and reports "unknown");
//   - the sha256 of the executable's bytes, which tells two binaries apart even when they
//     were stamped from the same commit (a dirty build, or one built from another checkout).
//
// The build stamp (buildVersion, which carries the dirty marker) is printed too, so a
// reader can see whether the one-line version came from a stamp or from a live git describe.

// CommitLabel is the commit the binary was stamped with, or "unknown" for an unstamped build.
func CommitLabel() string {
	if GitCommit == "" || GitCommit == "unknown" {
		return "unknown"
	}
	return GitCommit
}

// StampLabel is the raw -ldflags build stamp, dirty marker included, or "unstamped".
func StampLabel() string {
	if buildVersion == "" || buildVersion == "unknown" {
		return "unstamped"
	}
	return buildVersion
}

// FullLines returns the lines `--version --full` prints after the one-line version, in order.
func FullLines() []string {
	return []string{
		"build stamp: " + StampLabel(),
		"commit: " + CommitLabel(),
		binaryLine(ExecutablePath, HashFile),
	}
}

// binaryLine is the `binary sha256:` line: the digest of the running executable, or the reason
// it could not be taken. The resolver and hasher are parameters so the failure line is testable.
func binaryLine(resolve func() (string, error), hash func(string) (string, error)) string {
	exe, err := resolve()
	if err != nil {
		return fmt.Sprintf("binary sha256: unavailable (%v)", err)
	}
	digest, err := hash(exe)
	if err != nil {
		return fmt.Sprintf("binary sha256: unavailable (%v)", err)
	}
	return "binary sha256: " + digest
}

// ExecutablePath is the running binary's path with symlinks resolved, so a `yolo` on PATH
// that links into the store hashes the real bytes.
func ExecutablePath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(exe)
}

// HashFile returns the lowercase hex sha256 of the file at path.
func HashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
