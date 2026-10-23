// Package version provides version-string discovery and normalization. The
// normalization is a byte contract pinned by goldens; in particular a
// dirty-on-tag build normalizes to "0.1.0+dirty" (WITH the "+").
package version

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/packsrc"
)

// buildVersion is stamped at build time via -ldflags -X (see
// scripts/build-go.sh, .goreleaser.yaml, the homebrew formula in
// .github/workflows/release.yml, and tools/build-wheels for the PyPI
// wheels). It's the installed-wheel analog of setuptools-scm's baked
// version: when present it IS the binary's version — git describe is
// only consulted for unstamped `go build`/`go install` binaries.
var buildVersion = ""

// GitCommit is the commit hash, 12 hex digits, stamped via -ldflags -X by every
// build channel: .goreleaser.yaml, the Homebrew formula, the PyPI wheels,
// scripts/build-go.sh, `just install` and the flake (buildStamp). Empty for
// unstamped builds. Not part of any user-facing byte contract yet — carried
// for release forensics until the post-cutover CLI surface pass surfaces it.
var GitCommit = ""

// SnapshotIdentity is the lowercase SHA-256 identity of the frozen source snapshot
// used to build a matched launcher bundle. Empty for legacy distribution builds.
var SnapshotIdentity = ""

// BuildKind classifies the launcher bundle as development or installed. Empty
// identifies older distribution builds that predate matched-bundle metadata.
//
// "installed" is what a from-source `just install` stamps, and it is the whole
// of that install's update-channel marker: such a binary is a snapshot of the
// checkout it was built from, the same as a release built at that commit, and
// records nothing about where that checkout is (internal/selfupdate KindSnapshot;
// docs/design/installed-launcher-provenance.md, OQ-IP1). No stamp distinguishes
// the installed launcher from the bundle's copy for the same platform, because
// they are one build.
var BuildKind = ""

// Baked returns the -ldflags-stamped version, normalized, or "" for an
// unstamped `go build`/`go install` binary. Unlike Get it never consults
// YOLO_VERSION or git: it answers what THIS binary was built as, which is the
// question an update check asks.
func Baked() string {
	if buildVersion == "" || buildVersion == "unknown" {
		return ""
	}
	return Normalize(buildVersion)
}

// Normalize converts a raw git-describe/env version string to the canonical
//
//	git format: 0.1.0-3-gabcdef1-dirty -> 0.1.0+3.gabcdef1.dirty
//	on tag, --long: 0.1.0-0-gabcdef1 -> 0.1.0+gabcdef1 (no distance)
//	exactly on tag: 0.1.0 -> 0.1.0
//	dirty on tag: 0.1.0-dirty -> 0.1.0+dirty
//	leading "v" is stripped.
func Normalize(raw string) string {
	raw = strings.TrimPrefix(raw, "v")

	parts := strings.Split(raw, "-")

	dirty := false
	if len(parts) > 0 && parts[len(parts)-1] == "dirty" {
		dirty = true
		parts = parts[:len(parts)-1]
	}

	var commitHash, commitCount string
	if len(parts) >= 2 &&
		strings.HasPrefix(parts[len(parts)-1], "g") &&
		isDigits(parts[len(parts)-2]) {
		commitHash = parts[len(parts)-1]
		commitCount = parts[len(parts)-2]
		parts = parts[:len(parts)-2]
	}

	baseVersion := strings.Join(parts, "-")

	var suffixParts []string
	// A zero distance is dropped, so a build on its tag reads <tag>+g<hash>: the
	// release form, and the form a flake build stamps (flake.nix: buildStamp).
	// Every build carries its hash (OQ-DA2, ruled 2026-10-16), which is why every
	// stamping describe passes --long; build metadata after "+" never orders a
	// version (selfupdate.Newer).
	if commitCount != "" && commitHash != "" {
		if commitCount != "0" {
			suffixParts = append(suffixParts, commitCount)
		}
		suffixParts = append(suffixParts, commitHash)
	}
	if dirty {
		suffixParts = append(suffixParts, "dirty")
	}

	if len(suffixParts) > 0 {
		return baseVersion + "+" + strings.Join(suffixParts, ".")
	}
	return baseVersion
}

// isDigits mirrors Python str.isdigit() for the ASCII case used here: true iff
// non-empty and every rune is a decimal digit. (Python's isdigit also accepts
// some unicode digit categories, but git commit counts are always ASCII.)
func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// gitDescribe resolves the version: YOLO_VERSION env override wins and is
// returned VERBATIM (Python early-returns it before the normalization block —
// the host sets it so the in-jail banner matches the host exactly, so it must
// not be re-normalized); else the -ldflags baked version, normalized; else
// `git describe --tags --dirty --always --long` in repoRoot, normalized. Returns ""
// when nothing resolves. Prefers the linker-stamped version (via YOLO_VERSION
// env or ldflags) over git describe, since an installed binary should report
// its own version, not the describe of whatever repo it happens to be run from.
func gitDescribe(repoRoot string) string {
	if raw := os.Getenv("YOLO_VERSION"); raw != "" {
		return raw
	}
	return binaryDescribe(repoRoot)
}

// binaryDescribe is gitDescribe without the YOLO_VERSION override: the version THIS binary
// answers to, from its -ldflags stamp or, unstamped, a live describe of repoRoot.
func binaryDescribe(repoRoot string) string {
	// "unknown" guard: an older scripts/build-go.sh stamped the literal
	// string "unknown" when describe failed at build time; never let that
	// legacy stamp shadow a live describe.
	if buildVersion != "" && buildVersion != "unknown" {
		return Normalize(buildVersion)
	}

	// No stamp and no repo root: an unstamped `go build`/`go install` binary
	// run from who-knows-where. Do NOT fall through to `git describe` in the
	// process cwd — `--always` succeeds inside ANY git repository, so the
	// binary would report the version of whatever repo the user happens to
	// be standing in (reproduced: an unstamped yolo run inside a foreign
	// checkout tagged v5.2.0 reported 5.2.0+…). "unknown" (via Get) is the
	// honest answer — the §2d "sane default", swarf's "dev" analog.
	if repoRoot == "" {
		return ""
	}

	var raw string
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "describe", "--tags", "--dirty", "--always", "--long", "--abbrev=12")
	cmd.Dir = repoRoot
	cmd.Env = packsrc.CleanGitEnv(os.Environ()) // describe repoRoot, not a hook's repo (srcskew.go's gitOut)
	out, err := cmd.Output()
	if err == nil {
		raw = strings.TrimSpace(string(out))
	}

	if raw == "" {
		return ""
	}
	return Normalize(raw)
}

// Get returns the yolo-jail version string.
// git-describe result, or "unknown" if even the baked fallback is empty.
func Get(repoRoot string) string {
	v := gitDescribe(repoRoot)
	if v == "" {
		return "unknown"
	}
	return v
}

// overrideEnv is the variable whose value Get returns in place of the binary's own version.
// The launcher sets it in every jail to its own version (internal/cli/run/assemble.go), so an
// in-jail banner names the host's yolo, and a test or a script may set it too.
const overrideEnv = "YOLO_VERSION"

// Binary is the version this binary answers to with YOLO_VERSION ignored: its stamp, or an
// unstamped binary's describe of repoRoot, else "unknown".
func Binary(repoRoot string) string {
	if v := binaryDescribe(repoRoot); v != "" {
		return v
	}
	return "unknown"
}

// overridden is the YOLO_VERSION value Get returned in place of the binary's own version, ""
// when the variable is unset or names that same version, so nothing needs saying; and the
// binary's own version, computed once, since an unstamped binary's is a git describe.
func overridden(repoRoot string) (raw, binary string) {
	raw = os.Getenv(overrideEnv)
	if raw == "" {
		return "", ""
	}
	if binary = Binary(repoRoot); raw == binary {
		return "", binary
	}
	return raw, binary
}

// Line is `yolo --version`'s one line: "yolo-jail <version>", and when YOLO_VERSION names a
// version other than the binary's own, both — "yolo-jail 9.9.9 (YOLO_VERSION override; binary
// 0.14.0+428.g772997837bbd)" — so a quoted version line never names a build other than the one
// that printed it. A reader that wants the version alone takes the text up to the first space.
func Line(repoRoot string) string {
	if raw, binary := overridden(repoRoot); raw != "" {
		return "yolo-jail " + raw + " (" + overrideEnv + " override; binary " + binary + ")"
	}
	return "yolo-jail " + Get(repoRoot)
}

// CheckLine is the version `yolo check` reports: Line's version and override, then the build's
// identity, its stamp and commit (FullLines' first two), so one line of a check report says
// which build answered it.
func CheckLine(repoRoot string) string {
	detail := "build stamp " + StampLabel() + ", commit " + CommitLabel()
	if raw, binary := overridden(repoRoot); raw != "" {
		return raw + " (" + overrideEnv + " override; binary " + binary + "; " + detail + ")"
	}
	return Get(repoRoot) + " (" + detail + ")"
}
