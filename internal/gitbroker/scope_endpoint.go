package gitbroker

import (
	"strings"

	"github.com/mschulkind-oss/yolo-jail/internal/brokerscope"
)

// scopeContains preserves literal matching and admits only the SSH endpoint spellings
// approved by the Git source's narrow SSH comparison.
func scopeContains(f brokerscope.File, remote string) bool {
	for _, approved := range f.Repos {
		if approved == remote || equivalentSSHEndpoint(approved, remote) {
			return true
		}
	}
	return false
}

// equivalentSSHEndpoint compares the SSH target Git will use, without equating aliases,
// usernames, ports, other path suffixes, or other URL spellings. The sole path exceptions are
// for GitHub's git@github.com user with no port: its relative scp path versus its absolute SSH
// URL path, and a single trailing .git on the repository segment, with or without the slash.
func equivalentSSHEndpoint(approved, request string) bool {
	if approved == request {
		return true
	}
	if _, err := transportOf(approved); err != nil {
		return false
	}
	if _, err := transportOf(request); err != nil {
		return false
	}
	approvedTarget, err := parseSSH(approved)
	if err != nil {
		return false
	}
	requestTarget, err := parseSSH(request)
	if err != nil {
		return false
	}
	if approvedTarget.dest != requestTarget.dest || approvedTarget.port != requestTarget.port {
		return false
	}
	if approvedTarget.path == requestTarget.path {
		return true
	}
	if approvedTarget.dest != "git@github.com" || approvedTarget.port != "" {
		return false
	}
	return githubRelativeAbsolutePath(approvedTarget.path, requestTarget.path)
}

// githubRelativeAbsolutePath reports whether two different GitHub repository paths name the same
// repository: one optional leading slash and one trailing .git are the only differences allowed.
// The repository segment left after stripping must be a plain owner/repo pair.
func githubRelativeAbsolutePath(a, b string) bool {
	a = strings.TrimSuffix(strings.TrimPrefix(a, "/"), ".git")
	b = strings.TrimSuffix(strings.TrimPrefix(b, "/"), ".git")
	if a != b || strings.ContainsAny(a, "%~\\?#") || strings.HasSuffix(a, ".git") {
		return false
	}
	parts := strings.Split(a, "/")
	if len(parts) != 2 {
		return false
	}
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}
