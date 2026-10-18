package packsrc

// relative.go is the RELATIVE LOCATOR (a term coined here): a pack address that names a git
// repository by a path relative to the directory of the pack that declares it, with no scheme —
// `../..?ref=carry/pi-subagents` from `yolo-packs/matt` names the repository that pack lives in.
//
// It is the one spelling of "this pack's own repository" that means the same thing in both notches:
// a jail reads its packs from the host's tree, so the same relative path from the same pack reaches
// the same repository, whether that repository is `/workspace` in a jail or `~/.dotfiles` on the
// host. An absolute path exists in one notch and names nothing in the other.
//
// THE CLIMB IS BOUNDED by the git repository the pack directory itself is in (the directory holding
// the nearest `.git` above it, which is what `git rev-parse --show-toplevel` names): a relative
// locator may name that repository or one inside it, never a directory above it. So a pack copied
// out of its repository stops resolving instead of reaching whatever sits above its new home.
//
// Only the `..` guard on the ref stays (checkNoDotDot): it guards git's `a..b` range syntax, which
// the path never meets.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// IsRelativeLocator reports whether raw is written as a relative locator: no scheme, and a path
// starting `.` — `.`, `..`, `./…` or `../…` — so a host name written without its scheme
// (`github.com/you/pi?ref=main`) is never read as a directory of the pack.
func IsRelativeLocator(raw string) bool {
	s := strings.TrimSpace(raw)
	if strings.Contains(s, "://") {
		return false
	}
	p, _, _ := strings.Cut(s, "?")
	return p == "." || p == ".." || strings.HasPrefix(p, "./") || strings.HasPrefix(p, "../")
}

// ParseFrom parses an address a PACK declares, dir being the declaring pack's directory as this
// process sees it: Parse, plus the relative locator, resolved against dir to the repository it names
// (Repo is then `file://<that repository>`, Raw the address as written). A dir of "" reads the
// relative locator's grammar alone — its ref — with Repo empty, for a caller that knows no pack
// directory (the manifest's static validation); every caller that fetches passes one.
//
// It is for the `patch_branch` value: a `packs` entry is read by Parse, and its directory is the
// pack itself, so it has no "own repository" to name.
func ParseFrom(raw, dir string) (Addr, error) {
	if !IsRelativeLocator(raw) {
		a, err := Parse(raw)
		if err != nil && !strings.Contains(strings.TrimSpace(raw), "://") && !strings.HasPrefix(strings.TrimSpace(raw), "/") {
			return Addr{}, fmt.Errorf("pack address %q has no scheme — write a path relative to the pack, "+
				"starting with ./ or ../ (../..?ref=main names the repository two directories up), "+
				"git+file:///abs/repo?ref=main, or a URL (git+https://host/org/repo?ref=main)", raw)
		}
		return a, err
	}
	s := strings.TrimSpace(raw)
	rel, query, _ := strings.Cut(s, "?")
	if strings.Contains(rel, "//") {
		return Addr{}, fmt.Errorf("pack address %q: a relative locator names a repository directory, "+
			"and has no //subdirectory — write the path to the repository's root", raw)
	}
	ref, err := refFromQuery(raw, query)
	if err != nil {
		return Addr{}, err
	}
	a := Addr{Kind: KindGit, Raw: raw, Ref: ref}
	if dir == "" {
		return a, nil
	}
	repo, err := resolveRelativeRepo(raw, rel, query, dir)
	if err != nil {
		return Addr{}, err
	}
	a.Repo = "file://" + filepath.ToSlash(repo)
	return a, nil
}

// RelativeRepositoryIdentity names the resolved declaring repository by its filesystem identity rather than its
// path. A container bind mount exposes the same device and inode under the jail's path, while independent clones
// receive different identities. The returned identity is empty for an explicit, non-relative address.
func RelativeRepositoryIdentity(raw, dir string) (string, error) {
	if !IsRelativeLocator(raw) {
		return "", nil
	}
	a, err := ParseFrom(raw, dir)
	if err != nil {
		return "", err
	}
	root := strings.TrimPrefix(a.Repo, "file://")
	info, err := os.Stat(root)
	if err != nil {
		return "", fmt.Errorf("reading the relative patch branch repository %s: %w", root, err)
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return "", fmt.Errorf("reading the relative patch branch repository %s: filesystem identity is unavailable", root)
	}
	return fmt.Sprintf("file-id:%x:%x", st.Dev, st.Ino), nil
}

// resolveRelativeRepo is the repository rel names from dir, bounded by dir's own repository.
func resolveRelativeRepo(raw, rel, query, dir string) (string, error) {
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", fmt.Errorf("pack address %q is relative to the pack's directory %s, which cannot be read (%v) — "+
			"check the pack's path", raw, dir, err)
	}
	top := gitTopLevel(real)
	if top == "" {
		return "", fmt.Errorf("pack address %q is relative to the pack's directory, and %s is in no git repository\n\n"+
			"A relative locator may name only the repository the pack lives in, or one inside it, so with no "+
			"repository it names nothing. Put the pack in its repository, or write the repository's absolute "+
			"address:\n\n  git+file:///abs/path/to/repo?%s", raw, real, query)
	}
	target := filepath.Join(real, filepath.FromSlash(rel))
	if !within(target, top) {
		return "", fmt.Errorf("pack address %q climbs to %s, above the git repository the pack lives in\n\n"+
			"A relative locator is bounded by %s (the pack's own repository, what `git -C %s rev-parse "+
			"--show-toplevel` names), so a pack copied elsewhere never reaches what sits above it. Name that "+
			"repository, or write the other repository's absolute address:\n\n  %s\n  git+file://%s?%s",
			raw, target, top, real, relativeTo(raw, real, top), target, query)
	}
	resolved, err := filepath.EvalSymlinks(target)
	if err != nil {
		return "", fmt.Errorf("pack address %q names %s, which does not exist (%v) — check the path, relative "+
			"to the pack's directory %s", raw, target, unwrapPath(err), real)
	}
	if !within(resolved, top) {
		return "", fmt.Errorf("pack address %q names %s, a link to %s above the git repository %s the pack lives in "+
			"— a relative locator is bounded by that repository; write the repository's absolute address "+
			"(git+file://%s?%s)", raw, target, resolved, top, resolved, query)
	}
	if !hasGitDir(resolved) {
		return "", fmt.Errorf("pack address %q names %s, which is not a repository's root — a patch branch is merged "+
			"as a whole repository; name the root of the repository the pack lives in:\n\n  %s",
			raw, resolved, relativeTo(raw, real, top))
	}
	return resolved, nil
}

// gitTopLevel is the directory holding the nearest `.git` at or above dir (a directory, or the file
// a worktree or a submodule has), "" when none does.
func gitTopLevel(dir string) string {
	for cur := dir; ; {
		if hasGitDir(cur) {
			return cur
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return ""
		}
		cur = parent
	}
}

func hasGitDir(dir string) bool {
	_, err := os.Lstat(filepath.Join(dir, ".git"))
	return err == nil
}

// within reports whether p is base or inside it.
func within(p, base string) bool {
	r, err := filepath.Rel(base, p)
	return err == nil && r != ".." && !strings.HasPrefix(r, ".."+string(filepath.Separator))
}

// relativeTo is raw rewritten to name top from dir, keeping its query: the spelling that names the
// pack's own repository.
func relativeTo(raw, dir, top string) string {
	r, err := filepath.Rel(dir, top)
	if err != nil {
		return raw
	}
	_, query, _ := strings.Cut(strings.TrimSpace(raw), "?")
	r = filepath.ToSlash(r)
	if r != "." && r != ".." && !strings.HasPrefix(r, "../") {
		r = "./" + r
	}
	return r + "?" + query
}

func unwrapPath(err error) error {
	var pe *os.PathError
	if errors.As(err, &pe) {
		return pe.Err
	}
	return err
}
