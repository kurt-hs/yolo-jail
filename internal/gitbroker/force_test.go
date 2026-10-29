package gitbroker

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/brokeraudit"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

// TestRealGitPushRejectionPhases records whether real Git rejects a refspec locally or
// delivers it to the broker's unchanged push policy. No test invokes push policy directly.
func TestRealGitPushRejectionPhases(t *testing.T) {
	gitVersion, err := exec.Command("git", "--version").CombinedOutput()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("client %s; production git-remote-yolo helper offers only capabilities=connect; fixture.git runs real Git through it", strings.TrimSpace(string(gitVersion)))

	countPushes := func(t *testing.T) int {
		t.Helper()
		data, err := os.ReadFile(paths.BrokerAuditLog())
		if os.IsNotExist(err) {
			return 0
		}
		if err != nil {
			t.Fatal(err)
		}
		var n int
		for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
			var e brokeraudit.Event
			if err := json.Unmarshal([]byte(line), &e); err != nil {
				t.Fatalf("audit line %q: %v", line, err)
			}
			if len(e.Argv) == 1 && e.Argv[0] == serviceReceivePack {
				n++
			}
		}
		return n
	}
	assertRejected := func(t *testing.T, f *fixture, before, old, new string, wantBroker bool, args ...string) string {
		t.Helper()
		nBefore := countPushes(t)
		cmd := append([]string{"push"}, args...)
		out, pushErr := f.git(cmd...)
		if pushErr == nil {
			t.Fatalf("push unexpectedly succeeded: %v\n%s", cmd, out)
		}
		after := mustGit(t, f.bare, "rev-parse", "main")
		if after != before {
			t.Fatalf("bare main moved %s -> %s", before, after)
		}
		nAfter := countPushes(t)
		if wantBroker {
			if nAfter != nBefore+1 || !strings.Contains(out, "! [remote rejected]") || !strings.Contains(out, reasonNeedsApproval) || strings.Contains(out, "needs force") {
				t.Fatalf("expected broker refusal; push count %d -> %d, output:\n%s", nBefore, nAfter, out)
			}
			e := lastPush(t)
			if e.Set != "refused" || e.Remote != origin || len(e.Refs) != 1 || e.Refs[0].Ref != "refs/heads/main" || e.Refs[0].Old != old || e.Refs[0].New != new {
				t.Fatalf("push audit does not witness refspec %s -> %s: %+v", old, new, e)
			}
			sent, readErr := os.ReadFile(filepath.Join(f.record, "stdin"))
			if readErr != nil || string(sent) != "0000" {
				t.Fatalf("upstream stdin %q (%v), want only empty command list", sent, readErr)
			}
		} else {
			if nAfter != nBefore+1 {
				t.Fatalf("expected client-side stale/non-FF check after receive-pack advertisement; audit count %d -> %d\n%s", nBefore, nAfter, out)
			}
			e := lastPush(t)
			if e.Outcome != "ran" || len(e.Refs) != 0 || strings.Contains(out, "! [remote rejected]") {
				t.Fatalf("client rejection did not produce an empty ref command list: %+v\n%s", e, out)
			}
			sent, readErr := os.ReadFile(filepath.Join(f.record, "stdin"))
			if readErr != nil || string(sent) != "0000" {
				t.Fatalf("upstream stdin %q (%v), want exactly empty command list", sent, readErr)
			}
			t.Logf("client-side rejection: fixture bare-current=%s (server advertisement inferred from local bare state, not packet-captured), candidate=%s; audit refs=0, upstream bytes=0000; output:\n%s", before, new, out)
		}
		if wantBroker {
			t.Logf("broker-refused refspec HEAD:refs/heads/main old=%s new=%s; output:\n%s", old, new, out)
		}
		return out
	}

	t.Run("known-ff-and-force", func(t *testing.T) {
		f := newFixture(t, "tee")
		before := mustGit(t, f.bare, "rev-parse", "main")
		f.localCommit()
		newID := mustGit(t, f.ws, "rev-parse", "HEAD")
		oldType := mustGit(t, f.ws, "cat-file", "-t", before)
		newType := mustGit(t, f.ws, "cat-file", "-t", newID)
		ancestry := mustGit(t, f.ws, "merge-base", "--is-ancestor", before, newID)
		t.Logf("known FF old=%s(%s) new=%s(%s) ancestor=%q shallow=%s", before, oldType, newID, newType, ancestry, mustGit(t, f.ws, "rev-parse", "--is-shallow-repository"))
		assertRejected(t, f, before, before, newID, true, "origin", "HEAD:refs/heads/main")
		assertRejected(t, f, before, before, newID, true, "--force", "origin", "HEAD:refs/heads/main")
	})

	t.Run("divergence", func(t *testing.T) {
		f := newFixture(t, "tee")
		before := mustGit(t, f.bare, "rev-parse", "main")
		f.localCommit()
		newID := mustGit(t, f.ws, "rev-parse", "HEAD")
		seed := filepath.Join(f.root, "seed")
		if err := os.WriteFile(filepath.Join(seed, "upstream-change"), []byte("upstream\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		mustGit(t, seed, "add", "upstream-change")
		mustGit(t, seed, "commit", "-q", "-m", "upstream")
		mustGit(t, seed, "push", "-q", f.bare, "main")
		remoteNow := mustGit(t, f.bare, "rev-parse", "main")
		if out, err := f.git("fetch", "origin"); err != nil {
			t.Fatalf("fetch divergent upstream commit: %v\n%s", err, out)
		}
		_, ancestryErr := run(t, f.ws, gitEnv(), "merge-base", "--is-ancestor", remoteNow, newID)
		if ee, ok := ancestryErr.(*exec.ExitError); !ok || ee.ExitCode() != 1 {
			t.Fatalf("divergence ancestry error = %v, want exit 1 (known non-ancestor)", ancestryErr)
		}
		t.Logf("divergence stale-advertisement old=%s bare-now=%s new=%s remoteNowAncestorErr=%v oldType=%s remoteType=%s newType=%s", before, remoteNow, newID, ancestryErr, mustGit(t, f.ws, "cat-file", "-t", before), mustGit(t, f.ws, "cat-file", "-t", remoteNow), mustGit(t, f.ws, "cat-file", "-t", newID))
		out := assertRejected(t, f, remoteNow, remoteNow, newID, false, "origin", "HEAD:refs/heads/main")
		if !strings.Contains(strings.ToLower(out), "non-fast-forward") && !strings.Contains(strings.ToLower(out), "fetch first") {
			t.Fatalf("normal divergent push did not identify the client-side stale/non-FF check:\n%s", out)
		}
		assertRejected(t, f, remoteNow, remoteNow, newID, true, "--force", "origin", "HEAD:refs/heads/main")
	})

	t.Run("missing-old-object", func(t *testing.T) {
		f := newFixture(t, "tee")
		old := mustGit(t, f.bare, "rev-parse", "main")
		seed := filepath.Join(f.root, "seed")
		if err := os.WriteFile(filepath.Join(seed, "remote-only"), []byte("remote\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		mustGit(t, seed, "add", "remote-only")
		mustGit(t, seed, "commit", "-q", "-m", "remote-only")
		mustGit(t, seed, "push", "-q", f.bare, "main")
		newID := mustGit(t, f.bare, "rev-parse", "main")
		if _, err := run(t, f.ws, gitEnv(), "cat-file", "-e", newID); err == nil {
			t.Fatalf("workspace unexpectedly has advertised old %s", newID)
		}
		t.Logf("missing old object old=%s absent-in-workspace new=%s type=%s oldType=%s shallow=%s", old, newID, mustGit(t, f.bare, "cat-file", "-t", newID), mustGit(t, f.ws, "cat-file", "-t", old), mustGit(t, f.ws, "rev-parse", "--is-shallow-repository"))
		// The client's stale tracking ref still knows the original tip, but the actual advertised old is absent.
		f.localCommit()
		local := mustGit(t, f.ws, "rev-parse", "HEAD")
		out := assertRejected(t, f, newID, newID, local, false, "origin", "HEAD:refs/heads/main")
		if !strings.Contains(strings.ToLower(out), "fetch first") {
			t.Fatalf("normal missing-old-object output lacks the expected client fetch-first phase:\n%s", out)
		}
		assertRejected(t, f, newID, newID, local, true, "--force", "origin", "HEAD:refs/heads/main")
	})

	t.Run("noncommit-object", func(t *testing.T) {
		f := newFixture(t, "tee")
		before := mustGit(t, f.bare, "rev-parse", "main")
		blob := mustGit(t, f.ws, "hash-object", "-w", "--stdin")
		// hash-object with empty stdin creates a real blob without writing fixture files.
		blob = strings.TrimSpace(blob)
		t.Logf("noncommit refspec old=%s(%s) source=%s(%s) target=refs/heads/main", before, mustGit(t, f.ws, "cat-file", "-t", before), blob, mustGit(t, f.ws, "cat-file", "-t", blob))
		args := []string{"origin", blob + ":refs/heads/main"}
		nBefore := countPushes(t)
		out, pushErr := f.git(append([]string{"push"}, args...)...)
		if pushErr == nil || !strings.Contains(out, "! [rejected]") || !strings.Contains(out, blob) || !strings.Contains(out, "(needs force)") {
			t.Fatalf("normal noncommit push did not expose the client-side needs-force check: %v\n%s", pushErr, out)
		}
		if countPushes(t) != nBefore+1 {
			t.Fatalf("normal noncommit push audit count %d -> %d\n%s", nBefore, countPushes(t), out)
		}
		if e := lastPush(t); e.Outcome != "ran" || len(e.Refs) != 0 {
			t.Fatalf("normal noncommit push should send no ref commands: %+v", e)
		}
		if sent, err := os.ReadFile(filepath.Join(f.record, "stdin")); err != nil || string(sent) != "0000" {
			t.Fatalf("normal noncommit upstream bytes %q (%v)", sent, err)
		}
		t.Logf("normal blob push client phase (needs force), audit no ref commands, upstream bytes=0000: %s", out)
		args = []string{"--force", "origin", blob + ":refs/heads/main"}
		nBefore = countPushes(t)
		out, pushErr = f.git(append([]string{"push"}, args...)...)
		if pushErr == nil || !strings.Contains(out, "! [remote rejected]") || !strings.Contains(out, blob) || !strings.Contains(out, "("+reasonNeedsApproval+")") || strings.Contains(out, "needs force") {
			t.Fatalf("forced blob did not reach broker needs-approval policy: %v\n%s", pushErr, out)
		}
		if countPushes(t) != nBefore+1 {
			t.Fatalf("forced blob broker audit count %d -> %d\n%s", nBefore, countPushes(t), out)
		}
		if e := lastPush(t); e.Set != "refused" || e.Remote != origin || len(e.Refs) != 1 || e.Refs[0].Old != before || e.Refs[0].New != blob || e.Refs[0].Ref != "refs/heads/main" {
			t.Fatalf("forced noncommit audit: %+v", e)
		}
		sent, err := os.ReadFile(filepath.Join(f.record, "stdin"))
		if err != nil || string(sent) != "0000" {
			t.Fatalf("forced noncommit upstream bytes %q (%v)", sent, err)
		}
		t.Logf("forced blob reached broker needs-approval, one ref audit, upstream bytes=0000: %s", out)
		if got := mustGit(t, f.bare, "rev-parse", "main"); got != before {
			t.Fatalf("bare main moved %s -> %s", before, got)
		}
	})

	t.Run("shallow-known-ff", func(t *testing.T) {
		f := newFixture(t, "tee")
		seed := filepath.Join(f.root, "seed")
		if err := os.WriteFile(filepath.Join(seed, "second"), []byte("second\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		mustGit(t, seed, "add", "second")
		mustGit(t, seed, "commit", "-q", "-m", "second")
		mustGit(t, seed, "push", "-q", f.bare, "main")
		shallow := filepath.Join(f.root, "shallow")
		mustGit(t, f.root, "clone", "-q", "--depth=1", "file://"+f.bare, shallow)
		if err := os.RemoveAll(f.ws); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(shallow, f.ws); err != nil {
			t.Fatal(err)
		}
		mustGit(t, f.ws, "remote", "set-url", "origin", origin)
		before := mustGit(t, f.bare, "rev-parse", "main")
		f.localCommit()
		newID := mustGit(t, f.ws, "rev-parse", "HEAD")
		if got := mustGit(t, f.ws, "rev-parse", "--is-shallow-repository"); got != "true" {
			t.Fatalf("workspace is not shallow: %s", got)
		}
		ancestry, ancestryErr := run(t, f.ws, gitEnv(), "merge-base", "--is-ancestor", before, newID)
		if ancestryErr != nil || strings.TrimSpace(ancestry) != "" {
			t.Fatalf("shallow known-FF ancestry = %q, %v; want exit 0", ancestry, ancestryErr)
		}
		t.Logf("shallow FF old=%s new=%s ancestry=%q ancestryErr=%v", before, newID, ancestry, ancestryErr)
		assertRejected(t, f, before, before, newID, true, "origin", "HEAD:refs/heads/main")
	})

	t.Run("https-known-ff", func(t *testing.T) {
		h := newHTTPSFixture(t, httpsOpts{})
		before := mustGit(t, h.bare, "rev-parse", "main")
		h.localCommit()
		newID := mustGit(t, h.ws, "rev-parse", "HEAD")
		out, err := h.git("push", "origin", "HEAD:refs/heads/main")
		if err == nil || !strings.Contains(out, reasonNeedsApproval) || strings.Contains(out, "needs force") {
			t.Fatalf("HTTPS FF disposition: %v\n%s", err, out)
		}
		if len(h.pushPosts()) != 0 {
			t.Fatalf("refused HTTPS push issued POST: %s", h.srv.log(t))
		}
		if got := mustGit(t, h.bare, "rev-parse", "main"); got != before {
			t.Fatalf("HTTPS upstream moved %s -> %s", before, got)
		}
		e := lastPush(t)
		if e.Remote != h.url || e.Set != "refused" || len(e.Refs) != 1 || e.Refs[0].Old != before || e.Refs[0].New != newID {
			t.Fatalf("HTTPS push audit: %+v", e)
		}
		t.Logf("HTTPS known FF normal old=%s new=%s remote=%q posts=%d: %s", before, newID, e.Remote, len(h.pushPosts()), out)
		forceOut, forceErr := h.git("push", "--force", "origin", "HEAD:refs/heads/main")
		if forceErr == nil || !strings.Contains(forceOut, reasonNeedsApproval) || strings.Contains(forceOut, "needs force") {
			t.Fatalf("HTTPS forced FF disposition: %v\n%s", forceErr, forceOut)
		}
		if len(h.pushPosts()) != 0 {
			t.Fatalf("refused forced HTTPS push issued POST: %s", h.srv.log(t))
		}
		if got := mustGit(t, h.bare, "rev-parse", "main"); got != before {
			t.Fatalf("HTTPS forced upstream moved %s -> %s", before, got)
		}
		e = lastPush(t)
		if e.Remote != h.url || e.Set != "refused" || len(e.Refs) != 1 || e.Refs[0].Old != before || e.Refs[0].New != newID {
			t.Fatalf("HTTPS forced push audit: %+v", e)
		}
		t.Logf("HTTPS known FF force old=%s new=%s remote=%q posts=%d: %s", before, newID, e.Remote, len(h.pushPosts()), forceOut)
	})
}
