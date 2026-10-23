package hostrun

import (
	"path/filepath"
	"strings"
)

// PrintsGHCredential reports whether argv is one of gh's credential-printing forms (HX-D17):
// `gh auth token`, `gh auth status` with -t or --show-token, and `gh config get` of a token
// key. It matches argv forms only, a speed bump against the obvious ask and not a credential
// control (design §6.4): `env gh auth token` passes it.
func PrintsGHCredential(argv []string) bool {
	if len(argv) < 2 || filepath.Base(argv[0]) != "gh" {
		return false
	}
	var pos []string
	flags := map[string]bool{}
	for _, a := range argv[1:] {
		if strings.HasPrefix(a, "-") {
			name, _, _ := strings.Cut(a, "=")
			flags[name] = true
			continue
		}
		pos = append(pos, a)
	}
	switch {
	case len(pos) >= 2 && pos[0] == "auth" && pos[1] == "token":
		return true
	case len(pos) >= 2 && pos[0] == "auth" && pos[1] == "status":
		return flags["-t"] || flags["--show-token"]
	case len(pos) >= 2 && pos[0] == "config" && pos[1] == "get":
		// A flag value (`-h github.com`) reads as a positional here; any later one naming a
		// token key counts.
		for _, k := range pos[2:] {
			if strings.Contains(k, "token") {
				return true
			}
		}
	}
	return false
}
