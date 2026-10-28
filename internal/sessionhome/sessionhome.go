// Package sessionhome builds a SESSION HOME (a term coined in
// docs/design/per-session-profiles.md §4.1): a per-session copy of the one directory a pack's
// program reads its config from, holding that session's own render of the files in it and a
// symbolic link to everything else, so the program's shared state (logins, history, installed
// packages) stays one copy while its config is the session's own.
//
// It is the filesystem half alone, and deliberately knows nothing else: not which pack declared
// the home (packdecl.SessionHome), not which files are rendered (the caller's surfaces), not the
// session id's source. Both the jail's session pass (internal/entrypoint) and a host launch
// (`yolo host`) build homes with it, so the link rules have one implementation (the plan's
// task 5).
//
// The rules (SP-D3, SP-D10):
//
//   - every entry present in the shared directory when the home is built is linked, except a
//     rendered name and a `*.lock` name;
//   - every declared shared name is linked even when absent: a directory (trailing "/") is
//     created in the shared directory first, a file is linked dangling, so the program's first
//     write creates the shared file;
//   - a preferred name links to its preferred target when that exists, else as a shared name;
//   - a rendered name is never a link: one left there is removed, so the render writes the
//     session's own file instead of writing through to the shared one (SP-D2);
//   - nothing is ever copied, and an occupant that is not a link is left alone: it is a STRAY
//     (coined in the design: an entry neither a link this package made nor a rendered file),
//     which Strays names.
package sessionhome

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mschulkind-oss/yolo-jail/internal/packdecl"
)

// SessionsDirName is the directory, beside a declared directory, that holds its session homes:
// `<parent(dir)>/.yolo-sessions/<session id>/<base(dir)>` (SP-D7). Beside it rather than inside
// it, so the program never lists yolo's homes as its own config, and in the parent so a home
// lives in the same writable state the declared directory does.
const SessionsDirName = ".yolo-sessions"

// SessionsRoot is the directory holding every session home of sharedDir.
func SessionsRoot(sharedDir string) string {
	return filepath.Join(filepath.Dir(filepath.Clean(sharedDir)), SessionsDirName)
}

// Home is the session home of sharedDir for the session named id. id must be one path
// segment; the caller validates its shape (the jail's session ids are lowercase hex).
func Home(sharedDir, id string) (string, error) {
	if id == "" || id == "." || id == ".." || strings.ContainsAny(id, `/\`) {
		return "", fmt.Errorf("session id %q is not one path segment", id)
	}
	clean := filepath.Clean(sharedDir)
	return filepath.Join(SessionsRoot(clean), id, filepath.Base(clean)), nil
}

// Spec is one session home to build. Every path is absolute.
type Spec struct {
	// Shared is the program's real config directory, the one every session home links into.
	Shared string
	// Home is the session home (Home(Shared, id) for a jail session; any directory for a host
	// launch).
	Home string
	// Names are the declared shared names (packdecl.SessionHome.Shared); a trailing "/" marks a
	// directory.
	Names []string
	// Prefer maps a name (trailing "/" for a directory) to the absolute path its link points at
	// when that path exists (packdecl.SessionHome.Prefer, "~/" already expanded).
	Prefer map[string]string
	// Rendered are the files the caller renders itself, as slash-separated paths relative to
	// Home: never linked. A path with a directory in it makes each directory on the way a real
	// directory of the home whose other entries are still linked (a MERGED directory), so one
	// rendered file in a subdirectory never hides the rest of that subdirectory.
	Rendered []string
}

func bare(name string) string { return strings.TrimSuffix(name, "/") }

// renderedSets is s.Rendered split into the rendered files and the merged directories on their
// paths, both as clean slash-separated paths relative to the home, plus the top-level names
// either occupies (never linked at the top).
func (s Spec) renderedSets() (files, merged, top map[string]bool) {
	files, merged, top = map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, r := range s.Rendered {
		rel := strings.Trim(filepath.ToSlash(filepath.Clean(filepath.FromSlash(bare(r)))), "/")
		if rel == "" || rel == "." || rel == ".." || strings.HasPrefix(rel, "../") {
			continue
		}
		files[rel] = true
		parts := strings.Split(rel, "/")
		top[parts[0]] = true
		for i := 1; i < len(parts); i++ {
			merged[strings.Join(parts[:i], "/")] = true
		}
	}
	return files, merged, top
}

// sortedKeys is m's keys, sorted, so a parent directory precedes its children.
func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// targets is the link each name in the home should be, deciding nothing on disk except that a
// declared directory is created in the shared dir (create == true). The map is name -> target.
func (s Spec) targets(create bool) (map[string]string, error) {
	_, _, rendered := s.renderedSets()
	out := map[string]string{}
	entries, err := os.ReadDir(s.Shared)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("reading the shared directory %s: %w", s.Shared, err)
	}
	for _, ent := range entries {
		name := ent.Name()
		if rendered[name] || packdecl.SessionHomeNameProblem(name) != "" {
			continue
		}
		out[name] = filepath.Join(s.Shared, name)
	}
	shared := func(name string) error {
		b := bare(name)
		if rendered[b] {
			return nil
		}
		if create && strings.HasSuffix(name, "/") {
			if err := os.MkdirAll(filepath.Join(s.Shared, b), 0o755); err != nil {
				return fmt.Errorf("creating the shared directory %s: %w", filepath.Join(s.Shared, b), err)
			}
		}
		out[b] = filepath.Join(s.Shared, b)
		return nil
	}
	for _, name := range s.Names {
		if p := packdecl.SessionHomeNameProblem(name); p != "" {
			return nil, fmt.Errorf("shared name %s", p)
		}
		if err := shared(name); err != nil {
			return nil, err
		}
	}
	names := make([]string, 0, len(s.Prefer))
	for name := range s.Prefer {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if p := packdecl.SessionHomeNameProblem(name); p != "" {
			return nil, fmt.Errorf("preferred name %s", p)
		}
		if _, err := os.Stat(s.Prefer[name]); err == nil {
			if !rendered[bare(name)] {
				out[bare(name)] = s.Prefer[name]
			}
			continue
		}
		if err := shared(name); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// Build makes s.Home and lays every link the package rules call for. It is idempotent: a link
// already pointing where it should is kept, a link pointing elsewhere is replaced, and a
// non-link occupant (a stray) is left where it is. An error names the path it failed on, and
// the caller refuses the session on it (SP-D6): a session falling back to the shared directory
// would read another session's profile.
func Build(s Spec) error {
	if !filepath.IsAbs(s.Shared) || !filepath.IsAbs(s.Home) {
		return fmt.Errorf("session home paths must be absolute (shared %q, home %q)", s.Shared, s.Home)
	}
	if err := os.MkdirAll(s.Shared, 0o755); err != nil {
		return fmt.Errorf("creating the shared directory %s: %w", s.Shared, err)
	}
	if err := mkdirAllNamed(filepath.Dir(s.Home)); err != nil {
		return err
	}
	if err := os.Mkdir(s.Home, 0o755); err != nil && !os.IsExist(err) {
		return fmt.Errorf("creating the session home %s: %w", s.Home, err)
	}
	if fi, err := os.Lstat(s.Home); err != nil || !fi.IsDir() {
		return fmt.Errorf("the session home %s is not a directory", s.Home)
	}
	files, merged, _ := s.renderedSets()
	// The merged directories first, parents before children: each is a real directory of the
	// home (a link left there is replaced, since writing through it would write the shared copy).
	for _, dir := range sortedKeys(merged) {
		p := filepath.Join(s.Home, filepath.FromSlash(dir))
		if fi, err := os.Lstat(p); err == nil && fi.Mode()&os.ModeSymlink != 0 {
			if err := os.Remove(p); err != nil {
				return fmt.Errorf("unlinking %s, which the session renders into: %w", p, err)
			}
		}
		if err := os.Mkdir(p, 0o755); err != nil && !os.IsExist(err) {
			return fmt.Errorf("creating %s in the session home: %w", p, err)
		}
		if fi, err := os.Lstat(p); err != nil || !fi.IsDir() {
			return fmt.Errorf("%s in the session home is not a directory, and the session renders into it", p)
		}
	}
	for _, rel := range sortedKeys(files) {
		p := filepath.Join(s.Home, filepath.FromSlash(rel))
		if fi, err := os.Lstat(p); err == nil && fi.Mode()&os.ModeSymlink != 0 {
			if err := os.Remove(p); err != nil {
				return fmt.Errorf("unlinking %s, which the session renders itself: %w", p, err)
			}
		}
	}
	// Inside each merged directory, every entry of its shared counterpart is linked except a
	// rendered file, a deeper merged directory and a lock.
	for _, dir := range sortedKeys(merged) {
		sharedDir := filepath.Join(s.Shared, filepath.FromSlash(dir))
		entries, err := os.ReadDir(sharedDir)
		if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("reading the shared directory %s: %w", sharedDir, err)
		}
		for _, ent := range entries {
			rel := dir + "/" + ent.Name()
			if files[rel] || merged[rel] || packdecl.SessionHomeNameProblem(ent.Name()) != "" {
				continue
			}
			if err := link(filepath.Join(s.Home, filepath.FromSlash(rel)),
				filepath.Join(s.Shared, filepath.FromSlash(rel))); err != nil {
				return err
			}
		}
	}
	targets, err := s.targets(true)
	if err != nil {
		return err
	}
	names := make([]string, 0, len(targets))
	for name := range targets {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if err := link(filepath.Join(s.Home, name), targets[name]); err != nil {
			return err
		}
	}
	return nil
}

// mkdirAllNamed is os.MkdirAll whose error names the first path in the way, not only the leaf.
func mkdirAllNamed(dir string) error {
	err := os.MkdirAll(dir, 0o755)
	if err == nil {
		return nil
	}
	var pe *os.PathError
	if errors.As(err, &pe) {
		return fmt.Errorf("creating %s: %v at %s", dir, pe.Err, pe.Path)
	}
	return fmt.Errorf("creating %s: %w", dir, err)
}

// link makes at a symbolic link to target, replacing a link to anything else and leaving a
// non-link occupant alone.
func link(at, target string) error {
	if fi, err := os.Lstat(at); err == nil {
		if fi.Mode()&os.ModeSymlink == 0 {
			return nil // a stray: kept, and named by Strays
		}
		if got, _ := os.Readlink(at); got == target {
			return nil
		}
		if err := os.Remove(at); err != nil {
			return fmt.Errorf("replacing the link %s: %w", at, err)
		}
	}
	if err := os.Symlink(target, at); err != nil {
		return fmt.Errorf("linking %s to %s: %w", at, target, err)
	}
	return nil
}

// Strays names, sorted, every entry in s.Home that is neither a rendered name nor a link to
// where Build points that name: a file the program created that the declaration did not
// foresee, or a link it replaced with a regular file by renaming over it. Build keeps each one;
// the caller names them in one warning line, saying the declaration should list them. It
// creates nothing.
func Strays(s Spec) ([]string, error) {
	entries, err := os.ReadDir(s.Home)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	targets, err := s.targets(false)
	if err != nil {
		return nil, err
	}
	files, merged, rendered := s.renderedSets()
	var out []string
	for _, dir := range sortedKeys(merged) {
		inner, err := os.ReadDir(filepath.Join(s.Home, filepath.FromSlash(dir)))
		if err != nil {
			continue
		}
		for _, ent := range inner {
			rel := dir + "/" + ent.Name()
			if files[rel] || merged[rel] || packdecl.SessionHomeNameProblem(ent.Name()) != "" {
				continue
			}
			if ent.Type()&os.ModeSymlink != 0 {
				got, _ := os.Readlink(filepath.Join(s.Home, filepath.FromSlash(rel)))
				if got == filepath.Join(s.Shared, filepath.FromSlash(rel)) {
					continue
				}
			}
			out = append(out, rel)
		}
	}
	for _, ent := range entries {
		name := ent.Name()
		// A `*.lock` is the program's own lock (SP-D10), never linked and never a stray.
		if rendered[name] || packdecl.SessionHomeNameProblem(name) != "" {
			continue
		}
		if ent.Type()&os.ModeSymlink != 0 {
			got, _ := os.Readlink(filepath.Join(s.Home, name))
			if want, ok := targets[name]; ok && got == want {
				continue
			}
			if got == filepath.Join(s.Shared, name) || got == s.Prefer[name] || got == s.Prefer[name+"/"] {
				continue
			}
		}
		out = append(out, name)
	}
	sort.Strings(out)
	return out, nil
}

// RepatriateOutcome is one stray's fate: moved into the shared directory, or kept where it is.
type RepatriateOutcome struct {
	// Name is the entry, slash-separated and relative to the home.
	Name string
	// To is where it was moved, empty when it stayed.
	To string
	// Kept is why it stayed, empty when it moved.
	Kept string
}

// Fate says what Repatriate does with one stray, WITHOUT doing it: to is where it would move, and
// kept is why it stays (exactly one of them is non-empty). The jail's boot uses it to name only
// the strays it cannot handle for the user, so no entry is reported twice — once as a prediction
// and once as an outcome.
//
// Three cases stay, and each is a decision this package must not make on the user's behalf:
//
//   - a name the shared directory already holds — two divergent copies, and merging two trees
//     silently is not a thing a tool may do;
//   - a top-level FILE — it may be a rendered file the program renamed (`settings.json.old`), so
//     it is not obviously the program's own state;
//   - anything nested — it lies inside a directory the session rendered into, where the two trees
//     cannot be reconciled without knowing which side is newer.
func Fate(s Spec, name string) (to, kept string, err error) {
	if strings.Contains(name, "/") {
		return "", "it lies inside a directory the session rendered into, so the two trees cannot be reconciled automatically", nil
	}
	from := filepath.Join(s.Home, filepath.FromSlash(name))
	info, err := os.Lstat(from)
	if err != nil {
		return "", "", fmt.Errorf("inspecting %s: %w", from, err)
	}
	if !info.IsDir() {
		return "", "a file may be a rendered file the program renamed, so it is not clearly the program's own state", nil
	}
	to = filepath.Join(s.Shared, filepath.FromSlash(name))
	if _, err := os.Lstat(to); err == nil {
		return "", "the shared directory already holds that name, so the two copies are kept apart", nil
	} else if !os.IsNotExist(err) {
		return "", "", fmt.Errorf("inspecting %s: %w", to, err)
	}
	return to, "", nil
}

// Repatriate moves what a program created in a session home into the SHARED directory that home
// stands in for (docs/design/session-home-contents-and-reaping.md §3, SHR-D4).
//
// From the program's side the session home IS its config directory, so the entries it created
// there are addressed to the shared one: that is where they belong, and once they are there the
// next Build links them and the name can never become a stray again. Moving them is the whole
// cure; keeping them aside is only preservation.
//
// Fate decides each entry; an I/O failure is returned as an error, and the caller keeps the home,
// which loses nothing. It creates nothing and removes nothing.
func Repatriate(s Spec, strays []string) ([]RepatriateOutcome, error) {
	out := make([]RepatriateOutcome, 0, len(strays))
	for _, name := range strays {
		to, kept, err := Fate(s, name)
		if err != nil {
			return out, err
		}
		if to == "" {
			out = append(out, RepatriateOutcome{Name: name, Kept: kept})
			continue
		}
		if err := os.Rename(filepath.Join(s.Home, filepath.FromSlash(name)), to); err != nil {
			return out, fmt.Errorf("moving %s into %s: %w", filepath.Join(s.Home, filepath.FromSlash(name)), s.Shared, err)
		}
		out = append(out, RepatriateOutcome{Name: name, To: to})
	}
	return out, nil
}
