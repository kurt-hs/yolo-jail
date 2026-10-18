package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateReleaseAssetDirectoryAgainstPinnedGoreleaserContract(t *testing.T) {
	root := repositoryRoot(t)
	targetSHA := strings.TrimSpace(string(mustRun(t, root, "git", "rev-parse", "ce77c0099d4261c54370cecf2652d109075419b3")))
	assets, err := expectedReleaseAssets(root, targetSHA, "0.12.1", pinnedTargetConfig(t, root, targetSHA))
	if err != nil {
		t.Fatal(err)
	}
	if len(assets) != 4 {
		t.Fatalf("target's release asset set = %v, want four archives", assets)
	}
	dir := makeAssetFixture(t, assets)
	got, err := validateReleaseAssetDir(root, targetSHA, "0.12.1", pinnedTargetConfig(t, root, targetSHA), dir)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, "\n") != strings.Join(assets, "\n") {
		t.Fatalf("validated assets %v, want %v", got, assets)
	}
}

func TestReleaseArtifactHandoffFailsClosedOnUnexpectedPathsTypesAndDigests(t *testing.T) {
	root := repositoryRoot(t)
	targetSHA := strings.TrimSpace(string(mustRun(t, root, "git", "rev-parse", "ce77c0099d4261c54370cecf2652d109075419b3")))
	trustedConfig := pinnedTargetConfig(t, root, targetSHA)
	assets, err := expectedReleaseAssets(root, targetSHA, "0.12.1", trustedConfig)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		mutate func(t *testing.T, dir string, assets []string)
		want   string
	}{
		{name: "unexpected asset", mutate: func(t *testing.T, dir string, _ []string) {
			writeAsset(t, dir, "attacker.whl", []byte("unapproved"))
		}, want: "unexpected release artifact"},
		{name: "symlink asset", mutate: func(t *testing.T, dir string, assets []string) {
			if err := os.Remove(filepath.Join(dir, assets[0])); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("checksums.txt", filepath.Join(dir, assets[0])); err != nil {
				t.Fatal(err)
			}
		}, want: "must be a non-empty regular file"},
		{name: "checksum mismatch", mutate: func(t *testing.T, dir string, assets []string) {
			writeAsset(t, dir, assets[0], []byte("changed after checksum"))
		}, want: "checksum mismatch"},
		{name: "missing archive", mutate: func(t *testing.T, dir string, assets []string) {
			if err := os.Remove(filepath.Join(dir, assets[0])); err != nil {
				t.Fatal(err)
			}
		}, want: "missing release artifact"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := makeAssetFixture(t, assets)
			tc.mutate(t, dir, assets)
			if _, err := validateReleaseAssetDir(root, targetSHA, "0.12.1", trustedConfig, dir); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("validation error = %v, want %q", err, tc.want)
			}
		})
	}
	mismatch := filepath.Join(t.TempDir(), "different-goreleaser.yaml")
	if err := os.WriteFile(mismatch, []byte("version: 2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := expectedReleaseAssets(root, targetSHA, "0.12.1", mismatch); err == nil || !strings.Contains(err.Error(), "differs from the trusted release config") {
		t.Fatalf("changed target publisher config was accepted: %v", err)
	}
}

func makeAssetFixture(t *testing.T, assets []string) string {
	t.Helper()
	dir := t.TempDir()
	var checksums []string
	for _, name := range assets {
		data := []byte("offline GoReleaser artifact: " + name)
		writeAsset(t, dir, name, data)
		digest := sha256.Sum256(data)
		checksums = append(checksums, fmt.Sprintf("%s  %s", hex.EncodeToString(digest[:]), name))
	}
	writeAsset(t, dir, "checksums.txt", []byte(strings.Join(checksums, "\n")+"\n"))
	return dir
}

func writeAsset(t *testing.T, dir, name string, data []byte) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func mustRun(t *testing.T, dir, name string, args ...string) []byte {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, output)
	}
	return output
}

// pinnedTargetConfig writes the pinned target commit's own .goreleaser.yaml to a file
// and returns its path, for the tests that pin a historical target: the trusted-config
// equality is what they rely on, not what they test, so they must not break whenever
// the working tree's release config moves (OQ-DA2's release stamp moved it).
func pinnedTargetConfig(t *testing.T, root, sha string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), ".goreleaser.yaml")
	cmd := exec.Command("git", "show", sha+":.goreleaser.yaml")
	cmd.Dir = root
	data, err := cmd.Output()
	if err != nil {
		t.Fatalf("reading %s's .goreleaser.yaml: %v", sha, err)
	}
	if err := os.WriteFile(p, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}
