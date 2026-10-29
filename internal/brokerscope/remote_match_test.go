package brokerscope

import "testing"

func TestValidateCurrentRemoteMatcherIsExplicitAndUsesFreshScope(t *testing.T) {
	t.Setenv("HOME", resolvedTempDir(t))
	const approved = "git@github.com:mschulkind/dotfiles"
	const request = "ssh://git@github.com/mschulkind/dotfiles"
	path, f := remoteURLFile(t, "remote-match", FormRemoteURL, approved)
	matcher := func(entry, candidate string) bool {
		return entry == approved && candidate == request
	}
	if err := ValidateCurrent(path, f, request); err == nil {
		t.Fatal("the default matcher admitted a non-identical remote URL")
	}
	if err := ValidateCurrentWithMatcher(path, f, request, matcher); err != nil {
		t.Fatalf("the explicitly supplied matcher refused the equivalent request: %v", err)
	}

	f.Repos = []string{"git@github.com:other/repository"}
	if _, err := Write(f); err != nil {
		t.Fatal(err)
	}
	if err := ValidateCurrentWithMatcher(path, f, request, matcher); err == nil {
		t.Fatal("the matcher admitted a request after its approved entry was revoked")
	}
}

func TestValidateCurrentMatcherCannotChangeOwnerRepoForm(t *testing.T) {
	t.Setenv("HOME", resolvedTempDir(t))
	path, f := remoteURLFile(t, "owner-match", "", "owner/repo")
	if err := ValidateCurrentWithMatcher(path, f, "other/repo", func(string, string) bool { return true }); err == nil {
		t.Fatal("the optional remote matcher changed owner/repo admissions")
	}
}
