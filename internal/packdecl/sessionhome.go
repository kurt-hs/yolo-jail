package packdecl

// sessionhome.go is the `session_homes` declaration (docs/design/per-session-profiles.md §4.4,
// SP-D13): a pack's statement of the ONE directory its program reads its config from, the
// environment variable that relocates the program to another directory, and the names in that
// directory that are shared state rather than config.
//
// # What core does with it
//
// A session's boot pass (not the jail's own boot) builds a SESSION HOME for each declaration: a
// per-session directory, `<parent(dir)>/.yolo-sessions/<session id>/<base(dir)>`, holding that
// session's render of every surface whose path lies in the declared directory, and a symbolic
// link to everything else (internal/sessionhome). The pass exports the declared variable,
// pointing at the session home, so every process of the session reads its own config while
// logins, history and installed packages stay one shared copy (SP-D2..D4). Core reads the
// declaration generically: it never knows which agent declared it (P4).
//
// # Why it is a TOP-LEVEL key
//
// An older yolo's tolerant decoder ignores an unknown top-level key but skips a whole
// contribution for an unknown field inside it (DecodeTolerant, skew.go). Declared on the
// program contribution, it would make an older jail drop the pack's program; declared here, an
// older jail ignores it and keeps today's shared render (SP-D13; packs/releasedecode_test.go).

import (
	"fmt"
	"strings"
)

// SessionHome is one session-home declaration.
//
//	"session_homes": [{
//	  "dir": ".tool/config",           // home-relative, strictly under one of this pack's state dirs
//	  "env": "TOOL_CONFIG_DIR",        // the variable the program reads its config dir from
//	  "shared": ["auth.json", "sessions/"],
//	  "prefer": {"agents/": "~/.agents"}
//	}]
//
// A trailing "/" on a name marks a DIRECTORY.
type SessionHome struct {
	// Dir is the program's config directory, home-relative ("~/" is not written), and
	// strictly under the `at` of one of this pack's `state` contributions: the session homes
	// live beside it, in `<parent(dir)>/.yolo-sessions/`, which must be the pack's own writable
	// state for the homes to survive a container restart until the next boot captures them
	// (SP-D7). Required.
	Dir string `json:"dir"`
	// Env is the environment variable that relocates the program's config directory (the
	// shipped pi pack names stock pi's own). The session's pass exports it, pointing at the session home
	// (SP-D4). A session whose environment already sets it gets no session home for this
	// declaration, and one line says so (SP-D5). Required; a portable variable name.
	Env string `json:"env"`
	// Shared are names in Dir that are SHARED STATE and must be linked into every session home
	// even when absent from Dir when the home is made: a directory name (trailing "/") is
	// created in Dir first, and a file name is linked dangling, so the program's first write
	// creates the shared file (a session home without the link would, for stock pi, start
	// logged out). Every entry present in Dir when the home is made is linked anyway, so this
	// list is only what may be ABSENT. One name each: no "/" inside it, not "." or "..", and
	// never a `*.lock` name, since a lock a program takes by mkdir reads as held forever
	// through a dangling link (SP-D10). Optional.
	Shared []string `json:"shared,omitempty"`
	// Prefer maps a name in the session home to a home-relative path ("~/…") the link should
	// point at instead of Dir's entry, WHEN that path exists; when it does not, the name is
	// linked into Dir like a Shared name. It exists for a program that reads another directory
	// until relocated and reads `<dir>/<name>` after: pi-subagents reads `~/.agents` with
	// its relocation variable unset and `<dir>/agents` with it set, so `{"agents/": "~/.agents"}`
	// keeps stock behavior exactly. Keys follow Shared's rules; values must start with "~/" and
	// contain no "..". Optional.
	Prefer map[string]string `json:"prefer,omitempty"`
}

// SessionHomes returns the pack's session-home declarations in declaration order, copied so a
// consumer cannot reach back into the manifest. THE accessor: no reader touches the field.
func (m *Manifest) SessionHomes() []SessionHome {
	if len(m.SessionHomeDecls) == 0 {
		return nil
	}
	out := make([]SessionHome, len(m.SessionHomeDecls))
	for i, h := range m.SessionHomeDecls {
		c := SessionHome{Dir: h.Dir, Env: h.Env}
		if len(h.Shared) > 0 {
			c.Shared = append([]string(nil), h.Shared...)
		}
		if len(h.Prefer) > 0 {
			c.Prefer = make(map[string]string, len(h.Prefer))
			for k, v := range h.Prefer {
				c.Prefer[k] = v
			}
		}
		out[i] = c
	}
	return out
}

// SessionHomeNameProblem is "" when name is a valid shared or prefer name (one entry of the
// declared directory, an optional trailing "/" marking a directory), else why not. Exported so
// the link builder (internal/sessionhome) refuses exactly what validation refuses.
func SessionHomeNameProblem(name string) string {
	bare := strings.TrimSuffix(name, "/")
	if bare == "" || bare == "." || bare == ".." || strings.Contains(bare, "/") {
		return fmt.Sprintf("%q must be one name in the declared directory (a trailing / marks a directory)", name)
	}
	if strings.HasSuffix(bare, ".lock") {
		return fmt.Sprintf("%q is a lock name: a lock a program takes by mkdir reads as held forever "+
			"through a link, so a session home never links one (SP-D10)", name)
	}
	return ""
}

// validateSessionHomes reports every structural problem with `session_homes`, in the house
// `session_homes[i].field: …` style. Every rule is version-invariant (a path escape, a bad
// variable name, a lock name), so it runs on the tolerant path too, as validateNeeds does.
func (m *Manifest) validateSessionHomes() []string {
	var problems []string
	var stateDirs []string
	for _, c := range m.Contributions() {
		if c.Kind == KindState && c.At != "" {
			stateDirs = append(stateDirs, strings.TrimSuffix(c.At, "/"))
		}
	}
	for i, h := range m.SessionHomeDecls {
		label := fmt.Sprintf("session_homes[%d]", i)
		dir := strings.TrimSuffix(h.Dir, "/")
		switch {
		case dir == "":
			problems = append(problems, label+".dir: is required — the directory the program reads its config from, home-relative")
		default:
			before := len(problems)
			problems = appendPathProblems(problems, label+".dir", dir)
			if len(problems) == before && !underAny(dir, stateDirs) {
				problems = append(problems, fmt.Sprintf("%s.dir: %q is not under one of this pack's "+
					"state dirs %v — declare the `state` contribution that holds it, since the session "+
					"homes live beside it and must survive a restart", label, dir, stateDirs))
			}
		}
		if !ValidEnvName(h.Env) {
			problems = append(problems, fmt.Sprintf("%s.env: %q is not a valid environment variable "+
				"name ([A-Za-z_][A-Za-z0-9_]*) — name the variable the program reads its config dir from",
				label, h.Env))
		}
		for j, name := range h.Shared {
			if p := SessionHomeNameProblem(name); p != "" {
				problems = append(problems, fmt.Sprintf("%s.shared[%d]: %s", label, j, p))
			}
		}
		for _, name := range sortedKeys(h.Prefer) {
			field := fmt.Sprintf("%s.prefer[%q]", label, name)
			if p := SessionHomeNameProblem(name); p != "" {
				problems = append(problems, field+": "+p)
			}
			target := h.Prefer[name]
			if !strings.HasPrefix(target, "~/") || len(target) == 2 {
				problems = append(problems, fmt.Sprintf("%s: %q must start with ~/ and name a path in the home", field, target))
				continue
			}
			problems = appendPathProblems(problems, field, strings.TrimPrefix(target, "~/"))
		}
	}
	return problems
}

// underAny reports whether dir is strictly beneath one of roots (slash-separated, relative).
func underAny(dir string, roots []string) bool {
	for _, r := range roots {
		if strings.HasPrefix(dir, r+"/") {
			return true
		}
	}
	return false
}
