package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var releaseAssetHash = regexp.MustCompile(`^[0-9a-f]{64}$`)
var releaseCommitSHA = regexp.MustCompile(`^[0-9a-f]{40}$`)
var releaseVersion = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$`)

var releaseArchivePlatforms = []string{
	"darwin/amd64", "darwin/arm64", "linux/amd64", "linux/arm64",
}

// normalizeVersion strips a tag's leading "v" and refuses anything but X.Y.Z[-pre], so a
// binary's stamped `X.Y.Z+g<hash>` is never taken for a release version.
func normalizeVersion(version string) (string, error) {
	v := strings.TrimPrefix(version, "v")
	if !releaseVersion.MatchString(v) {
		return "", fmt.Errorf("%q is not a release version (want X.Y.Z or X.Y.Z-pre)", version)
	}
	return v, nil
}

// expectedReleaseAssets derives the only files GoReleaser may upload: one archive per
// platform of the pinned archive matrix. A release publishes nothing else — an official
// pack program ships as a yolo subcommand, never as a release file
// (docs/reference/pack-binaries.md OQ-BP8). It reads the target's GoReleaser config as a Git
// blob; no target source is executed in a write-capable job.
func expectedReleaseAssets(root, targetSHA, version, trustedConfig string) ([]string, error) {
	if !releaseCommitSHA.MatchString(targetSHA) {
		return nil, fmt.Errorf("target SHA must be a full lowercase commit SHA")
	}
	normalized, err := normalizeVersion(version)
	if err != nil {
		return nil, err
	}
	targetConfig, err := gitBlob(root, targetSHA, ".goreleaser.yaml")
	if err != nil {
		return nil, fmt.Errorf("read target GoReleaser config: %w", err)
	}
	trusted, err := os.ReadFile(trustedConfig)
	if err != nil {
		return nil, fmt.Errorf("read trusted GoReleaser config: %w", err)
	}
	if string(targetConfig) != string(trusted) {
		return nil, errors.New("target .goreleaser.yaml differs from the trusted release config; no target-selected publisher config is accepted")
	}

	assets := make([]string, 0, len(releaseArchivePlatforms))
	for _, platform := range releaseArchivePlatforms {
		osArch := strings.Split(platform, "/")
		assets = append(assets, fmt.Sprintf("yolo-jail_%s_%s_%s.tar.gz", normalized, osArch[0], osArch[1]))
	}
	sort.Strings(assets)
	return assets, nil
}

func gitBlob(root, commit, name string) ([]byte, error) {
	cmd := exec.Command("git", "show", commit+":"+name)
	cmd.Dir = root
	return cmd.Output()
}

// validateReleaseAssetDir checks the inert artifact handoff after download and
// returns a sorted asset list. The target tree determines expected names, but
// every path and byte digest is revalidated by this trusted caller.
func validateReleaseAssetDir(root, targetSHA, version, trustedConfig, dir string) ([]string, error) {
	expected, err := expectedReleaseAssets(root, targetSHA, version, trustedConfig)
	if err != nil {
		return nil, err
	}
	rootInfo, err := os.Lstat(dir)
	if err != nil {
		return nil, fmt.Errorf("release artifact directory: %w", err)
	}
	if !rootInfo.IsDir() || rootInfo.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("release artifact root must be a real directory")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	allowed := make(map[string]struct{}, len(expected)+1)
	for _, name := range expected {
		allowed[name] = struct{}{}
	}
	allowed["checksums.txt"] = struct{}{}
	actual := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if filepath.Base(name) != name || name == "." || name == ".." || strings.HasPrefix(name, "-") {
			return nil, fmt.Errorf("unsafe release artifact name %q", name)
		}
		if _, ok := allowed[name]; !ok {
			return nil, fmt.Errorf("unexpected release artifact %q", name)
		}
		info, err := os.Lstat(filepath.Join(dir, name))
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() <= 0 || info.Size() > 1<<30 {
			return nil, fmt.Errorf("release artifact %q must be a non-empty regular file no larger than 1 GiB", name)
		}
		actual[name] = struct{}{}
	}
	for name := range allowed {
		if _, ok := actual[name]; !ok {
			return nil, fmt.Errorf("missing release artifact %q", name)
		}
	}
	if err := verifyReleaseChecksums(dir, expected); err != nil {
		return nil, err
	}
	return expected, nil
}

func verifyReleaseChecksums(dir string, expected []string) error {
	file, err := os.Open(filepath.Join(dir, "checksums.txt"))
	if err != nil {
		return fmt.Errorf("open release checksums: %w", err)
	}
	defer file.Close()
	expectedSet := make(map[string]struct{}, len(expected))
	for _, name := range expected {
		expectedSet[name] = struct{}{}
	}
	seen := make(map[string]struct{}, len(expected))
	scanner := bufio.NewScanner(io.LimitReader(file, 1<<20))
	for scanner.Scan() {
		line := scanner.Text()
		if len(line) < 67 || line[64:66] != "  " || !releaseAssetHash.MatchString(line[:64]) {
			return errors.New("malformed release checksums.txt entry")
		}
		name := line[66:]
		if filepath.Base(name) != name || strings.ContainsAny(name, "\\\x00") {
			return fmt.Errorf("unsafe checksum filename %q", name)
		}
		if _, ok := expectedSet[name]; !ok {
			return fmt.Errorf("unexpected checksum entry %q", name)
		}
		if _, duplicate := seen[name]; duplicate {
			return fmt.Errorf("duplicate checksum entry %q", name)
		}
		seen[name] = struct{}{}
		file, err := os.Open(filepath.Join(dir, name))
		if err != nil {
			return fmt.Errorf("open release asset %q: %w", name, err)
		}
		info, err := file.Stat()
		if err != nil {
			file.Close()
			return fmt.Errorf("stat release asset %q: %w", name, err)
		}
		if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > 1<<30 {
			file.Close()
			return fmt.Errorf("release asset %q changed to an invalid file type or size", name)
		}
		digest := sha256.New()
		written, copyErr := io.Copy(digest, io.LimitReader(file, (1<<30)+1))
		closeErr := file.Close()
		if copyErr != nil {
			return fmt.Errorf("read release asset %q: %w", name, copyErr)
		}
		if closeErr != nil {
			return fmt.Errorf("close release asset %q: %w", name, closeErr)
		}
		if written != info.Size() {
			return fmt.Errorf("release asset %q changed while checksums were validated", name)
		}
		if hex.EncodeToString(digest.Sum(nil)) != line[:64] {
			return fmt.Errorf("checksum mismatch for release asset %q", name)
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read release checksums: %w", err)
	}
	if len(seen) != len(expected) {
		return fmt.Errorf("checksums cover %d release assets; want %d", len(seen), len(expected))
	}
	return nil
}

// writeReleaseAssets implements the trusted validate-assets CLI used by the
// write-capable upload job. It outputs names only after the entire artifact set
// and every digest has been checked.
func writeReleaseAssets(args []string, out io.Writer, errOut io.Writer) int {
	flags := flag.NewFlagSet("validate-assets", flag.ContinueOnError)
	flags.SetOutput(errOut)
	root := flags.String("repo-root", ".", "trusted checkout containing target Git objects")
	targetSHA := flags.String("sha", "", "exact target commit SHA")
	version := flags.String("version", "", "release version without v")
	config := flags.String("trusted-config", ".goreleaser.yaml", "trusted GoReleaser config path")
	dir := flags.String("dir", "release-assets", "downloaded release asset directory")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(errOut, "validate-assets: unexpected arguments")
		return 2
	}
	assets, err := validateReleaseAssetDir(*root, *targetSHA, *version, *config, *dir)
	if err != nil {
		fmt.Fprintf(errOut, "validate-assets: %v\n", err)
		return 1
	}
	encoder := json.NewEncoder(out)
	if err := encoder.Encode(append([]string{"checksums.txt"}, assets...)); err != nil {
		fmt.Fprintf(errOut, "validate-assets: write asset list: %v\n", err)
		return 1
	}
	return 0
}

func contains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}
