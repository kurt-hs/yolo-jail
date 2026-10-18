package darwinpkg

// floorcache.go lets an unchanged macos-user launch skip nix entirely: Materialize records what
// its last build produced, keyed on every input that decides the result, and a launch whose inputs
// hash to the same key — and whose recorded profile is still the one the GC root holds — takes the
// recorded answer instead of running the skip-list eval and the build again (1.4 s and 2.7 s on a
// Mac for a floor nothing changed, measured 2026-10-17).
//
// THE KEY IS THE INPUTS, NOT A TIME OR A VERSION. It hashes the system, the build argv (the attr
// and the out-link), the YOLO_EXTRA_PACKAGES value the flake reads, every NIXPKGS_* variable
// (nixpkgs' own --impure opt-ins, such as NIXPKGS_ALLOW_UNFREE), and the bytes of flake.nix and
// flake.lock: the same two files the flake's own `imageIdentity` treats as deciding the image, for
// the same reason — every package the floor names, and the nixpkgs it resolves them in, is spelled
// in those two files, and flake.nix reads no other environment variable (a test pins that:
// TestTheFlakeReadsOnlyTheEnvTheFloorKeyHashes). Any change to any of them is a different key, and
// that launch runs nix as before, so a changed floor is never served from the record — there is no
// fallback here to fall back from (PD-NF1): a miss builds, and a failed build is fatal, as always.
//
// THE RECORD IS ONLY BELIEVED WHILE NIX STILL HOLDS IT: the GC root link (ProfileRootLink) must
// name the recorded profile and that store path must exist. A collected or retargeted profile is a
// miss. The record is written only after a successful build whose skip-list eval also succeeded,
// so a best-effort eval that failed is never frozen into the answer.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
)

// floorRecordVersion is folded into the key: bump it when what a record means changes.
const floorRecordVersion = "darwin-floor-record/1"

// floorRecordLeaf is the record's name beside the GC root link it describes.
const floorRecordLeaf = "packages.floor.json"

// floorRecord is what a successful Materialize leaves for the next launch.
type floorRecord struct {
	Key         string   `json:"key"`
	ProfilePath string   `json:"profile_path"`
	Skipped     []string `json:"skipped"`
}

// floorRecordPath is the record for the profile rooted at link.
func floorRecordPath(link string) string {
	return filepath.Join(filepath.Dir(link), floorRecordLeaf)
}

// floorKey hashes every input that decides what Materialize's build produces; ok is false when one
// of them cannot be read, and then nothing is cached.
func floorKey(repoRoot string, packages []any, system string, argv []string) (string, bool) {
	h := sha256.New()
	field := func(s string) { fmt.Fprintf(h, "%d:%s;", len(s), s) }
	field(floorRecordVersion)
	field(system)
	field(strings.Join(argv, "\x00"))
	extra := ""
	if len(packages) > 0 {
		s, err := jsonx.DumpsCompact(packages)
		if err != nil {
			return "", false
		}
		extra = s
	}
	field(extra)
	// NIXPKGS' OWN ENVIRONMENT OPT-INS: the build is --impure, so nixpkgs' check-meta reads
	// NIXPKGS_ALLOW_UNFREE (and _INSECURE, _BROKEN, _UNSUPPORTED_SYSTEM), which move what the floor
	// can hold and what the skip list names. Every NIXPKGS_* variable is hashed, sorted, so the
	// remedy for a skipped unfree package is a different key.
	var nixpkgsEnv []string
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "NIXPKGS_") {
			nixpkgsEnv = append(nixpkgsEnv, kv)
		}
	}
	sort.Strings(nixpkgsEnv)
	field(strings.Join(nixpkgsEnv, "\x00"))
	for _, name := range []string{"flake.nix", "flake.lock"} {
		body, err := os.ReadFile(filepath.Join(repoRoot, name))
		if err != nil {
			return "", false
		}
		sum := sha256.Sum256(body)
		field(name)
		field(hex.EncodeToString(sum[:]))
	}
	return hex.EncodeToString(h.Sum(nil)), true
}

// cachedFloor returns the recorded result for key when the record matches it and nix still holds
// the recorded profile at link.
func cachedFloor(link, key string) (*DarwinPackages, bool) {
	body, err := os.ReadFile(floorRecordPath(link))
	if err != nil {
		return nil, false
	}
	var rec floorRecord
	if json.Unmarshal(body, &rec) != nil || rec.Key != key || rec.ProfilePath == "" {
		return nil, false
	}
	if target, err := os.Readlink(link); err != nil || target != rec.ProfilePath {
		return nil, false
	}
	if _, err := os.Stat(rec.ProfilePath); err != nil {
		return nil, false
	}
	skipped := rec.Skipped
	if skipped == nil {
		skipped = []string{}
	}
	pkgs := ProfilePathsFromStdout(rec.ProfilePath, skipped, nil)
	return pkgs, pkgs != nil
}

// recordFloor writes the record for a successful build, atomically; a failure to write it costs the
// next launch a build and nothing else.
func recordFloor(link, key string, pkgs *DarwinPackages) {
	body, err := json.Marshal(floorRecord{Key: key, ProfilePath: pkgs.ProfilePath, Skipped: pkgs.Skipped})
	if err != nil {
		return
	}
	path := floorRecordPath(link)
	tmp, err := os.CreateTemp(filepath.Dir(path), floorRecordLeaf+".*")
	if err != nil {
		return
	}
	_, werr := tmp.Write(append(body, '\n'))
	cerr := tmp.Close()
	if werr != nil || cerr != nil || os.Rename(tmp.Name(), path) != nil {
		_ = os.Remove(tmp.Name())
	}
}
