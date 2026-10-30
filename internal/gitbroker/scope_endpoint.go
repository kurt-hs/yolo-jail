package gitbroker

import (
	"strings"

	"github.com/mschulkind-oss/yolo-jail/internal/brokerscope"
)

// scopeContains preserves literal matching and admits only the SSH endpoint spellings
// approved by the Git source's narrow SSH comparison.
func scopeContains(f brokerscope.File, remote string) bool {
	for _, approved := range f.Repos {
		if approved == remote || equivalentEndpoint(approved, remote) {
			return true
		}
	}
	return false
}

// equivalentEndpoint is the one matcher the broker's scope checks use: the SSH comparison, plus
// the HTTPS spellings of a github.com repository a project's request approves (RC-D26).
func equivalentEndpoint(approved, request string) bool {
	return equivalentSSHEndpoint(approved, request) || equivalentGitHubHTTPS(approved, request)
}

// equivalentGitHubHTTPS reports whether two https://github.com URLs name one repository, differing
// only by one trailing slash and one trailing .git. It admits no user name, port, query, other
// host or other path shape, so it equates no more than the SSH comparison above does.
func equivalentGitHubHTTPS(approved, request string) bool {
	const prefix = "https://github.com/"
	if !strings.HasPrefix(approved, prefix) || !strings.HasPrefix(request, prefix) {
		return false
	}
	return githubRelativeAbsolutePath(strings.TrimSuffix(approved, "/")[len(prefix):], strings.TrimSuffix(request, "/")[len(prefix):])
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
