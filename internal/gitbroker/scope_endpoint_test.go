package gitbroker

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/brokerscope"
)

func TestApprovedGitHubScopeAcceptsSSHSpellings(t *testing.T) {
	const approved = "git@github.com:mschulkind/dotfiles"
	for _, remote := range []string{"ssh://git@github.com/mschulkind/dotfiles", "git+ssh://git@github.com/mschulkind/dotfiles"} {
		t.Run(remote, func(t *testing.T) {
			f := newFixture(t, "", approved)
			body := "case \"$last\" in \"git-upload-pack '/mschulkind/dotfiles'\"|\"git-upload-pack 'mschulkind/dotfiles'\") ;; *) echo 'unexpected upstream' >&2; exit 97;; esac\nexec git-upload-pack '" + f.bare + "'"
			writeExec(t, filepath.Join(f.bin, "ssh"), sshScript(f.record, filepath.Join(f.root, "srv"), body))
			out, err := f.git("ls-remote", remote)
			if err != nil || !strings.Contains(out, "\trefs/heads/main") {
				t.Fatalf("approved GitHub alternate %q: %v\n%s", remote, err, out)
			}
			events := auditEvents(t)
			if len(events) != 1 || events[0].Set != "read" || events[0].Remote != remote {
				t.Fatalf("original URL not retained: %+v", events)
			}
		})
	}
}

func TestApprovedGitHubSSHAlternateClonesThroughBroker(t *testing.T) {
	const remote = "git+ssh://git@github.com/mschulkind/dotfiles"
	f := newFixture(t, "", "git@github.com:mschulkind/dotfiles")
	body := "case \"$last\" in \"git-upload-pack '/mschulkind/dotfiles'\") ;; *) echo 'unexpected upstream' >&2; exit 97;; esac\nexec git-upload-pack '" + f.bare + "'"
	writeExec(t, filepath.Join(f.bin, "ssh"), sshScript(f.record, filepath.Join(f.root, "srv"), body))
	out, err := f.git("clone", remote, filepath.Join(f.root, "clone"))
	if err != nil {
		t.Fatalf("approved GitHub alternate clone: %v\n%s", err, out)
	}
	if got, err := os.ReadFile(filepath.Join(f.root, "clone", "README")); err != nil || string(got) != "hello\n" {
		t.Fatalf("clone did not transfer repository content: %q, %v", got, err)
	}
	events := auditEvents(t)
	if len(events) != 1 || events[0].Set != "read" || events[0].Remote != remote {
		t.Fatalf("clone did not retain the original URL in audit: %+v", events)
	}
}

func TestSSHEndpointScopeEquivalenceIsNarrow(t *testing.T) {
	cases := []struct {
		name, approved, request string
		want                    bool
	}{
		{"scheme alias", "ssh://alice@host.example/team/repo", "git+ssh://alice@host.example/team/repo", true},
		{"GitHub relative to absolute", "git@github.com:owner/repo", "ssh://git@github.com/owner/repo", true},
		{"GitHub scheme alias with same absolute path", "ssh://git@github.com/owner/repo", "ssh+git://git@github.com/owner/repo", true},
		{"different username", "git@github.com:owner/repo", "ssh://alice@github.com/owner/repo", false},
		{"different port", "ssh://git@github.com:22/owner/repo", "git@github.com:owner/repo", false},
		{"different host spelling", "git@github.com:owner/repo", "ssh://git@GITHUB.com/owner/repo", false},
		{"different host", "git@github.com:owner/repo", "ssh://git@github.example/owner/repo", false},
		{"different path case", "git@github.com:Owner/repo", "ssh://git@github.com/owner/repo", false},
		{"different repository", "git@github.com:owner/repo", "ssh://git@github.com/owner/other", false},
		{"no git suffix stripping", "git@github.com:owner/repo.git", "ssh://git@github.com/owner/repo", false},
		{"trailing slash", "git@github.com:owner/repo", "ssh://git@github.com/owner/repo/", false},
		{"generic relative versus absolute", "alice@host.example:team/repo", "ssh://alice@host.example/team/repo", false},
		{"HTTPS is not SSH-equivalent", "git@github.com:owner/repo", "https://github.com/owner/repo", false},
		{"encoded path", "git@github.com:owner/repo", "ssh://git@github.com/owner%2Frepo", false},
		{"traversal", "git@github.com:owner/repo", "ssh://git@github.com/owner/../repo", false},
		{"helper", "git@github.com:owner/repo", "ext::git-upload-pack github.com/owner/repo", false},
		{"GitHub path needs two segments", "git@github.com:owner/repo/child", "ssh://git@github.com/owner/repo/child", false},
		{"GitHub tilde is not stripped", "git@github.com:owner/repo", "ssh://git@github.com/~/owner/repo", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := brokerscope.File{Form: brokerscope.FormRemoteURL, Repos: []string{tc.approved}}
			if got := scopeContains(f, tc.request); got != tc.want {
				t.Fatalf("scopeContains(%q, %q) = %v, want %v", tc.approved, tc.request, got, tc.want)
			}
		})
	}
	if !scopeContains(brokerscope.File{Form: brokerscope.FormRemoteURL, Repos: []string{"https://github.com/owner/repo"}}, "https://github.com/owner/repo") {
		t.Fatal("literal HTTPS matching changed")
	}
}

func TestRealGitRoutedEndpointDenialsHaveNoUpstreamOrPendingRequest(t *testing.T) {
	for _, remote := range []string{
		"ssh://alice@github.com/mschulkind/dotfiles",
		"ssh://git@github.com:2222/mschulkind/dotfiles",
		"ssh://git@github.example/mschulkind/dotfiles",
		"ssh://git@GITHUB.com/mschulkind/dotfiles",
		"ssh://git@github.com/mschulkind/other",
		"ssh://git@github.com/mschulkind/dotfiles/",
		"ssh://git@github.com/mschulkind%2Fdotfiles",
		"ssh+git://git@github.com/mschulkind/other",
	} {
		t.Run(remote, func(t *testing.T) {
			f := newFixture(t, "", "git@github.com:mschulkind/dotfiles")
			out, err := f.git("ls-remote", remote)
			if err == nil || !strings.Contains(out, "approved scope") {
				t.Fatalf("unapproved endpoint was not refused before transport: %v\n%s", err, out)
			}
			if f.sshRan() {
				t.Fatal("refused endpoint started the host's SSH process")
			}
			requests, _, err := f.broker.store.List()
			if err != nil {
				t.Fatal(err)
			}
			if len(requests) != 0 {
				t.Fatalf("read refusal created approval requests: %+v", requests)
			}
			events := auditEvents(t)
			if len(events) != 1 || events[0].Remote != remote || events[0].Outcome != "refused" {
				t.Fatalf("refusal audit did not retain original endpoint: %+v", events)
			}
		})
	}
}

func TestRealGitDeniesRevokedSSHEndpointBeforeStartingSSH(t *testing.T) {
	const remote = "ssh://git@github.com/mschulkind/dotfiles"
	setup := func(t *testing.T) *fixture {
		t.Helper()
		f := newFixture(t, "", "git@github.com:mschulkind/dotfiles")
		body := "case \"$last\" in \"git-upload-pack '/mschulkind/dotfiles'\") ;; *) echo 'unexpected upstream' >&2; exit 97;; esac\nexec git-upload-pack '" + f.bare + "'"
		writeExec(t, filepath.Join(f.bin, "ssh"), sshScript(f.record, filepath.Join(f.root, "srv"), body))
		if out, err := f.git("ls-remote", remote); err != nil {
			t.Fatalf("initial approved fetch: %v\n%s", err, out)
		}
		return f
	}
	t.Run("membership", func(t *testing.T) {
		f := setup(t)
		before, err := os.ReadFile(filepath.Join(f.record, "argv"))
		if err != nil {
			t.Fatal(err)
		}
		f.setScope("git@github.com:other/repository")
		out, err := f.git("ls-remote", remote)
		if err == nil || !strings.Contains(out, "not in this workspace's approved scope") {
			t.Fatalf("revoked endpoint was not refused: %v\n%s", err, out)
		}
		assertNoNewSSH(t, f, before)
		requests, _, err := f.broker.store.List()
		if err != nil {
			t.Fatal(err)
		}
		if len(requests) != 0 {
			t.Fatalf("revoked endpoint created approval requests: %+v", requests)
		}
	})
	t.Run("read grant", func(t *testing.T) {
		f := setup(t)
		before, err := os.ReadFile(filepath.Join(f.record, "argv"))
		if err != nil {
			t.Fatal(err)
		}
		sf, err := brokerscope.ReadFile(f.scopeFile)
		if err != nil {
			t.Fatal(err)
		}
		sf.Grants = nil
		if _, err := brokerscope.Write(sf); err != nil {
			t.Fatal(err)
		}
		out, err := f.git("ls-remote", remote)
		if err == nil || !strings.Contains(out, "no read grant") {
			t.Fatalf("revoked read grant was not enforced: %v\n%s", err, out)
		}
		assertNoNewSSH(t, f, before)
		requests, _, err := f.broker.store.List()
		if err != nil {
			t.Fatal(err)
		}
		if len(requests) != 0 {
			t.Fatalf("revoked read grant created approval requests: %+v", requests)
		}
	})
}

func assertNoNewSSH(t *testing.T, f *fixture, before []byte) {
	t.Helper()
	after, err := os.ReadFile(filepath.Join(f.record, "argv"))
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatalf("a refused request started another host SSH process:\nbefore %q\nafter  %q", before, after)
	}
}
