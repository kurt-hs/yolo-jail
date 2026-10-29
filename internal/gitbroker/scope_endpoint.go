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
// usernames, ports, path suffixes, or other URL spellings. The sole path exception is GitHub's
// relative scp path versus its absolute SSH URL path.
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

func githubRelativeAbsolutePath(a, b string) bool {
	if strings.HasPrefix(a, "/") == strings.HasPrefix(b, "/") {
		return false
	}
	if strings.HasPrefix(a, "/") {
		a = strings.TrimPrefix(a, "/")
	} else {
		b = strings.TrimPrefix(b, "/")
	}
	if a != b || strings.ContainsAny(a, "%~\\?#") {
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
