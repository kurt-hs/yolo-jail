package hostrun

import "testing"

func TestGHCredentialForms(t *testing.T) {
	for _, argv := range [][]string{
		{"gh", "auth", "token"},
		{"/usr/bin/gh", "auth", "token", "-h", "github.com"},
		{"gh", "auth", "status", "-t"},
		{"gh", "auth", "status", "--hostname", "x", "--show-token"},
		{"gh", "auth", "status", "--show-token=true"},
		{"gh", "config", "get", "oauth_token"},
		{"gh", "config", "get", "-h", "github.com", "oauth_token"},
	} {
		if !PrintsGHCredential(argv) {
			t.Errorf("%q passed", argv)
		}
	}
	for _, argv := range [][]string{
		{"gh", "auth", "status"},
		{"gh", "pr", "view"},
		{"gh", "config", "get", "editor"},
		{"ghx", "auth", "token"},
		{"echo", "gh", "auth", "token"},
		{"gh"},
	} {
		if PrintsGHCredential(argv) {
			t.Errorf("%q refused", argv)
		}
	}
}
