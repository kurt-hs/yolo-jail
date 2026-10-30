package configmigrate

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/mschulkind-oss/yolo-jail/internal/gitexclude"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

// Ignore keeps a converted file out of commits the way git kept its old name out
// (config-migration.md §4): when git ignored the old name and does not ignore the new one, Apply
// adds the new name to the clone's own .git/info/exclude, never to the tracked .gitignore. When
// git cannot be asked about a private file, it carries the one line to add by hand instead.
type Ignore struct {
	// Old and New are the old name and its converted twin.
	Old, New string
	// Exclude is the info/exclude file Apply appends Line to; "" when only Hint applies.
	Exclude, Line string
	// Hint is the one line said instead, when git could not be asked or the line could not be
	// written.
	Hint string
	// Done says Apply added Line to Exclude.
	Done bool
}

// planIgnores decides an Ignore for each name a conversion renames: the physical file and, for a
// linked old name, the link beside it. The rule is git's own answer for the old and new name,
// so it covers every file a migration renames, not a list of names.
func planIgnores(g *Graph, targets []Target) []Ignore {
	var out []Ignore
	for _, t := range targets {
		if t.New == t.Old {
			continue
		}
		pairs := [][2]string{{t.Old, t.New}}
		if t.Link != nil {
			pairs = append(pairs, [2]string{t.Link.Old, t.Link.Path})
		}
		for i, pair := range pairs {
			oldPath, newPath := pair[0], pair[1]
			switch gitexclude.Check(oldPath) {
			case gitexclude.Ignored:
				if gitexclude.Check(newPath) != gitexclude.NotIgnored {
					continue
				}
				ig := Ignore{Old: oldPath, New: newPath}
				line, ok := gitexclude.Line(newPath)
				exclude := gitexclude.File(filepath.Dir(newPath))
				if ok && exclude != "" {
					ig.Exclude, ig.Line = exclude, line
				} else {
					ig.Hint = ignoreHint(g.Request.Workspace, newPath)
				}
				out = append(out, ig)
			case gitexclude.Unknown:
				// Without git there is no rule to compare, so only a file private by definition —
				// the workspace's local file, under the name the workspace reads — gets the hint.
				if i == len(pairs)-1 && isLocalFile(t.File) {
					out = append(out, Ignore{Old: oldPath, New: newPath, Hint: ignoreHint(g.Request.Workspace, newPath)})
				}
			}
		}
	}
	return out
}

func isLocalFile(f *File) bool {
	return f != nil && f.Scope == ScopeProject &&
		(f.topName == paths.WorkspaceLocalConfigLegacyJSONCName || f.topName == paths.WorkspaceLocalConfigLegacyJSONName)
}

func ignoreHint(workspace, newPath string) string {
	name := shownPath(workspace, newPath)
	return fmt.Sprintf("%s is not ignored by git (git could not be asked here); to keep it out of commits, add the line `%s` to .gitignore", name, filepath.ToSlash(name))
}

// shownPath is path relative to dir when it lies inside it, else path itself.
func shownPath(dir, path string) string {
	if dir != "" {
		if rel, err := filepath.Rel(dir, path); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return rel
		}
	}
	return path
}

// Plan is the transcript's line for one Ignore.
func (ig Ignore) Plan(workspace string) string {
	if ig.Exclude == "" {
		return ig.Hint
	}
	return fmt.Sprintf("%s is not ignored by git as %s was; adding it to %s",
		shownPath(workspace, ig.New), shownPath(workspace, ig.Old), shownPath(workspace, ig.Exclude))
}

// Report is Apply's line for one Ignore, ending in a full stop.
func (ig Ignore) Report(workspace string) string {
	if ig.Done {
		return fmt.Sprintf("Added %s to %s, so %s stays out of commits as %s did.",
			ig.Line, shownPath(workspace, ig.Exclude), shownPath(workspace, ig.New), shownPath(workspace, ig.Old))
	}
	return ig.Hint + "."
}

// applyIgnores carries out the planned Ignores once the files are in place. A line that cannot
// be written becomes its hint: the migration itself has already succeeded.
func applyIgnores(planned []Ignore, workspace string) []Ignore {
	out := make([]Ignore, 0, len(planned))
	for _, ig := range planned {
		if ig.Exclude != "" {
			if gitexclude.Check(ig.New) != gitexclude.NotIgnored {
				continue // something else ignores it since the plan
			}
			if err := gitexclude.Append(ig.Exclude, ig.Line); err != nil {
				ig.Hint = fmt.Sprintf("%s is not ignored by git as %s was, and adding it to %s failed (%v); add the line `%s` to %s yourself",
					shownPath(workspace, ig.New), shownPath(workspace, ig.Old), ig.Exclude, err, ig.Line, ig.Exclude)
				ig.Exclude = ""
			} else {
				ig.Done = true
			}
		}
		out = append(out, ig)
	}
	return out
}
