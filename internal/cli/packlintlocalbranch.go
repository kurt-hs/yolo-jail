package cli

// packlintlocalbranch.go is `yolo pack lint`'s warning about a patch branch named by a LOCAL locator, a relative
// or git+file:// one (docs/reference/patched-forks.md §3.5, PF-D100). Such a locator names a repository on the
// author's machine, so whoever installs the pack by its URL has no copy of it and their launch is refused
// (measured in the pack paths inventory: "packs: recording relative patch branches for <pack>: …"). The fix
// is the published URL, which the author's own clone still answers when it is newer, through the local clone
// map. A warning, not a failure: the relative form stays the author's to keep (not ruled away).

import (
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/mschulkind-oss/yolo-jail/internal/packload"
	"github.com/mschulkind-oss/yolo-jail/internal/packsrc"
)

// localPatchBranchWarnings is one warning per patch branch pack names by a relative or git+file:// locator.
func localPatchBranchWarnings(pack *packload.Pack, root string) []string {
	var out []string
	for _, f := range patchedOfPack(pack, root) {
		raw := strings.TrimSpace(f.PatchBranch)
		if raw == "" || !(packsrc.IsRelativeLocator(raw) || strings.HasPrefix(raw, "git+file://")) {
			continue
		}
		suggest := "git+https://<host>/<owner>/<repo>?ref=<branch>"
		if a, err := packsrc.ParseFrom(raw, f.PatchBranchDir); err == nil {
			suggest = "git+https://<host>/<owner>/<repo>?ref=" + a.Ref
			if dir, ok := strings.CutPrefix(a.Repo, "file://"); ok {
				if u := publishedURLOf(dir); u != "" {
					suggest = u + "?ref=" + a.Ref
				}
			}
		}
		out = append(out, f.Label()+": the patch branch "+raw+" names a repository on this machine, so whoever "+
			"installs this pack by its URL has no copy of it and their launch is refused — name the published URL "+
			"instead, "+suggest+"; your own clone is still built from automatically when it is newer, because a "+
			"pack path inside it, or a `local_clones` entry, registers it")
	}
	return out
}

// publishedURLOf is the pack address of dir's origin remote (else its first), "" when it has none, or one that
// is another path on this machine.
func publishedURLOf(dir string) string {
	out, err := exec.Command("git", "-C", dir, "config", "--get-regexp", `^remote\..*\.url$`).Output()
	if err != nil {
		return ""
	}
	url := ""
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		key, value, ok := strings.Cut(line, " ")
		if !ok {
			continue
		}
		if key == "remote.origin.url" {
			url = strings.TrimSpace(value)
			break
		}
		if url == "" {
			url = strings.TrimSpace(value)
		}
	}
	switch {
	case strings.HasPrefix(url, "https://"), strings.HasPrefix(url, "ssh://"):
		return "git+" + url
	case url != "" && !strings.Contains(url, "://") && !filepath.IsAbs(url) && strings.Contains(url, ":"):
		host, path, _ := strings.Cut(url, ":")
		return "git+ssh://" + host + "/" + strings.TrimPrefix(path, "/")
	}
	return ""
}
