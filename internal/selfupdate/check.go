package selfupdate

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/packsrc"
)

// GitRunner runs git in dir and returns its trimmed stdout.
type GitRunner func(ctx context.Context, dir string, args ...string) (string, error)

// CheckDeps are Check's side effects, injected so it is testable offline.
type CheckDeps struct {
	HTTP       *http.Client
	ReleaseURL string // LatestReleaseAPI unless a test points it elsewhere
	Now        func() time.Time
}

// DefaultCheckDeps are the real network and clock.
func DefaultCheckDeps() CheckDeps {
	return CheckDeps{
		HTTP:       &http.Client{Timeout: 15 * time.Second},
		ReleaseURL: LatestReleaseAPI,
		Now:        time.Now,
	}
}

// Check asks whether something newer than ch exists and returns the answer as
// the State to cache. A failure is recorded in State.Error rather than
// returned, because a failed check is an answer too: it decides when the next
// one runs (see State.Fresh). prev carries the user's earlier "no" forward when
// it was given for this same binary.
func Check(ctx context.Context, ch Channel, prev State, deps CheckDeps) State {
	st := State{
		CheckedAt: deps.Now().UTC(),
		Identity:  ch.Identity(),
		Kind:      ch.Kind,
		Current:   ch.Version,
	}
	if prev.Identity == st.Identity {
		st.DeclinedFor = prev.DeclinedFor
	}
	// The disclosure is about the machine, not the binary: an update must not
	// make it repeat.
	st.Disclosed = prev.Disclosed
	var err error
	switch ch.Kind {
	case KindSnapshot:
		// No caller asks: `yolo update` and `yolo check` stop before a check,
		// and no automatic check runs for a snapshot. Answering here keeps a
		// future caller from comparing a snapshot against a release.
		err = fmt.Errorf("%s", SnapshotSummary)
	case KindUnknown:
		err = fmt.Errorf("cannot tell how this yolo was installed")
	default:
		st.Latest, err = latestRelease(ctx, deps.HTTP, deps.ReleaseURL)
		st.Available = err == nil && Newer(st.Latest, ch.Version)
	}
	if err != nil {
		st.Error = err.Error()
	}
	return st.sanitized()
}

// latestRelease returns the newest published release's version, without the
// tag's leading "v".
func latestRelease(ctx context.Context, client *http.Client, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "yolo-jail-update-check")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%s: HTTP %d", url, resp.StatusCode)
	}
	var body struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body); err != nil {
		return "", fmt.Errorf("%s: %w", url, err)
	}
	if body.TagName == "" {
		return "", fmt.Errorf("%s: no tag_name in the response", url)
	}
	return strings.TrimPrefix(body.TagName, "v"), nil
}

// RunGit is the real GitRunner, which `yolo update --from` reads the named
// checkout with (DeployCheckout). It describes dir and nothing else — a git
// hook's GIT_DIR must not redirect it (packsrc.CleanGitEnv) — and never prompts,
// so it fails cleanly instead of opening a GUI helper.
func RunGit(ctx context.Context, dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	out, err := gitCommand(ctx, dir, args...).Output()
	if ctx.Err() != nil {
		return "", fmt.Errorf("git %s in %s: %w", firstArg(args), dir, ctx.Err())
	}
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok && len(ee.Stderr) > 0 {
			return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(string(ee.Stderr)))
		}
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// gitCommand builds the command RunGit runs, split out so its hygiene is
// testable as a property of the command. packsrc.DetachGit runs it in a new
// session whose whole process group dies with ctx, with a bounded wait on the
// output pipe, so nothing git starts can outlive the deadline.
func gitCommand(ctx context.Context, dir string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "git", append([]string{
		"-C", dir,
		"-c", "credential.interactive=false",
		"-c", "core.askPass=",
	}, args...)...)
	cmd.Env = unattendedGitEnv(packsrc.CleanGitEnv(os.Environ()))
	packsrc.DetachGit(cmd)
	return cmd
}

func firstArg(args []string) string {
	if len(args) == 0 {
		return ""
	}
	return args[0]
}

// unattendedGitEnv strips and sets what it takes for git to FAIL rather than
// ask, on every path it can ask by: its own terminal prompt
// (GIT_TERMINAL_PROMPT), an askpass helper that would pop a GUI dialog
// (GIT_ASKPASS and SSH_ASKPASS forced empty, core.askPass overridden above,
// SSH_ASKPASS_REQUIRE=never for ssh
// itself), and Git Credential Manager's own UI (GCM_INTERACTIVE). RunGit is
// deliberately non-interactive even when its caller has a terminal. A key
// already loaded in an ssh agent, or a stored credential, still works: only
// asking is refused.
func unattendedGitEnv(env []string) []string {
	out := make([]string, 0, len(env)+3)
	for _, kv := range env {
		key, _, _ := strings.Cut(kv, "=")
		switch key {
		case "GIT_ASKPASS", "SSH_ASKPASS", "SSH_ASKPASS_REQUIRE", "GIT_TERMINAL_PROMPT", "GCM_INTERACTIVE":
			continue
		}
		out = append(out, kv)
	}
	return append(out,
		"GIT_TERMINAL_PROMPT=0",
		"GIT_ASKPASS=",
		"SSH_ASKPASS=",
		"SSH_ASKPASS_REQUIRE=never",
		"GCM_INTERACTIVE=never",
	)
}

// Newer reports whether version a is newer than version b. Both are compared
// on their MAJOR.MINOR.PATCH core; a pre-release ("0.11.0-rc1") sorts before
// its release, and build metadata after a "+" (a from-HEAD Homebrew build's
// "+510.gcfefa8bf") is ignored. Anything unparsable is never newer, so a
// malformed tag cannot produce a notice.
func Newer(a, b string) bool {
	cmp, ok := compareVersions(a, b)
	return ok && cmp > 0
}

// AtLeast reports whether version a is the same as or newer than b.
func AtLeast(a, b string) bool {
	cmp, ok := compareVersions(a, b)
	return ok && cmp >= 0
}

func compareVersions(a, b string) (int, bool) {
	ac, apre, aok := parseVersion(a)
	bc, bpre, bok := parseVersion(b)
	if !aok || !bok {
		return 0, false
	}
	for i := range ac {
		if ac[i] != bc[i] {
			if ac[i] > bc[i] {
				return 1, true
			}
			return -1, true
		}
	}
	switch {
	case apre == bpre:
		return 0, true
	case !apre && bpre:
		return 1, true
	default:
		return -1, true
	}
}

func parseVersion(v string) (core [3]int, pre, ok bool) {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if i := strings.IndexByte(v, '+'); i >= 0 {
		v = v[:i]
	}
	if i := strings.IndexByte(v, '-'); i >= 0 {
		v, pre = v[:i], true
	}
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return core, false, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return core, false, false
		}
		core[i] = n
	}
	return core, pre, true
}
