package macosuser

// sessiontmp.go is the run-as-you sandbox's OWN TEMP DIR and the host-services dirs its profile
// grants by exact path (docs/design/macos-user-model-plan.md, MMP-D15 and MMP-D17).

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

// sessionTmpPrefix is the base-name prefix of a session's own temp dir under the invoking
// user's DARWIN_USER_TEMP_DIR.
const sessionTmpPrefix = "yolo-"

// sessionTmpIDLen is how much of the session id names the dir. Eight hex digits keep the path
// SHORT, which is the point: a unix socket's path is capped at 104 bytes on macOS, Python's
// multiprocessing falls back to the literal /tmp when len(TMPDIR)+28 reaches it, and tmux puts
// tmux-<uid>/default under TMUX_TMPDIR. The physical DARWIN_USER_TEMP_DIR measured about 54 bytes,
// so the dir is about 68, tmux's socket about 85 and Python's about 96: all under 104.
const sessionTmpIDLen = 8

// SessionTmpDir is the run-as-you session's own TMPDIR,
// <DARWIN_USER_TEMP_DIR>/yolo-<first 8 of the session id>, or "" when the user's temp dir is not
// known (the account mode asks for none, and a Mac that could not answer `getconf` gives none).
// darwinUserTemp must be physical: the profile grants it by this spelling.
//
// Under the user's temp dir rather than /tmp, so it keeps working when /tmp is denied (OQ-MM8),
// and so macOS's own sweep of that dir collects one a killed session left behind. Never under the
// workspace or HOME, whose paths are as long as the workspace's and would put sockets past the
// 104-byte cap.
func SessionTmpDir(darwinUserTemp, sessionID string) string {
	base := strings.TrimRight(darwinUserTemp, "/")
	if !filepath.IsAbs(base) || sessionID == "" {
		return ""
	}
	id := sessionID
	if len(id) > sessionTmpIDLen && id != SessionPlaceholder {
		id = id[:sessionTmpIDLen]
	}
	return filepath.Join(base, sessionTmpPrefix+id)
}

// withSessionTmpEnv points the sandbox's temp variables at dir: TMPDIR, and the two tools that
// ignore it, tmux (TMUX_TMPDIR, else the literal /tmp) and zsh (TMPPREFIX, else /tmp/zsh, for its
// heredocs and =(...)). An env layer of the user's that already set TMPDIR keeps it, and the two
// followers then follow that value instead; one outside what the profile grants refuses the launch
// (composedTmpdirProblem). "" sets nothing.
func withSessionTmpEnv(env *jsonx.OrderedMap, dir string) *jsonx.OrderedMap {
	if dir == "" {
		return env
	}
	if env != nil {
		if v, ok := env.Get("TMPDIR"); ok && asStr(v) != "" {
			dir = strings.TrimRight(asStr(v), "/")
		}
	}
	env = withEnvVar(env, "TMPDIR", dir)
	for _, kv := range [][2]string{{"TMUX_TMPDIR", dir}, {"TMPPREFIX", dir + "/zsh"},
		{"OVERMIND_SOCKET", dir + "/overmind.sock"}} {
		if old, ok := env.Get(kv[0]); !ok || asStr(old) == "" {
			env = withEnvVar(env, kv[0], kv[1])
		}
	}
	return env
}

// composedTmpdirProblem is the launch's refusal for a TMPDIR the user's env composed (tmpdir) that
// lies outside every root the run-as-you profile lets the sandbox write: roots (the workspace, the
// agent's home, the user's DARWIN_USER_TEMP_DIR, which holds the session's own), plus the shared
// /tmp when the hatch is set. "" when tmpdir is empty or inside one (MMP-D20). Compared lexically,
// with /tmp, /var and /etc in their /private spelling, as the profile grants them; a symlink inside
// a root that leads out is the profile's to refuse, not this check's.
func composedTmpdirProblem(tmpdir string, roots []string, sharedTmp bool) string {
	if tmpdir == "" {
		return ""
	}
	if sharedTmp {
		roots = append(roots, "/private/tmp")
	}
	t := privateSpelling(tmpdir)
	if filepath.IsAbs(t) {
		for _, r := range roots {
			if r = strings.TrimRight(r, "/"); r == "" {
				continue
			}
			if r = privateSpelling(r); t == r || strings.HasPrefix(t, r+"/") {
				return ""
			}
		}
	}
	msg := fmt.Sprintf("TMPDIR=%s, set by your env config, is outside everything the run-as-you sandbox "+
		"may write (the workspace, its home and your own temp folder), so every temporary file would fail "+
		"with \"Operation not permitted\". Remove the TMPDIR override so the session uses its own temp "+
		"dir, or point it inside the workspace", tmpdir)
	if t == "/private/tmp" || strings.HasPrefix(t, "/private/tmp/") {
		msg += `; or, to use the shared /tmp, set "macos_user_shared_tmp": true in your user config`
	}
	return msg + " (MMP-D20)."
}

// privateSpelling is p cleaned, with /tmp, /var and /etc spelled under /private, where macOS keeps
// them and the profile names them.
func privateSpelling(p string) string {
	p = filepath.Clean(p)
	for _, top := range []string{"/tmp", "/var", "/etc"} {
		if p == top || strings.HasPrefix(p, top+"/") {
			return "/private" + p
		}
	}
	return p
}

// SessionTmpCreateCommands make the session's temp dir, 0700, as you, before the bootstrap. No
// -p: the name carries this session's fresh random id, so a dir already there is not this
// session's, and the stage refuses rather than adopt it.
func SessionTmpCreateCommands(dir string) [][]string {
	if dir == "" {
		return nil
	}
	return [][]string{{mkdirBin, "-m", "0700", dir}}
}

// SessionTmpRemoveCommands remove the session's temp dir when the session ends. rm -r does not
// follow a symlink the agent left inside it.
func SessionTmpRemoveCommands(dir string) [][]string {
	if dir == "" {
		return nil
	}
	return [][]string{{rmBin, "-rf", dir}}
}

// sessionServicesDirs is every host-services SESSION dir an endpoint variable in envs names a
// file in: the dirs this session's sandbox is told to read, and so the ones its profile grants
// by exact path (MMP-D15). The env is the manifest, for endpointGrantCommands' reason: the run
// pipeline set one variable per published endpoint, whether this session's own services
// published it or the workspace's keeper did. A file in any other dir is not granted here: the
// grant is for the session dirs the shared-/tmp allow reaches today, and nothing else.
func sessionServicesDirs(envs ...*jsonx.OrderedMap) []string {
	var out []string
	for _, env := range envs {
		if env == nil {
			continue
		}
		for _, k := range env.Keys() {
			if !isServiceEndpointVar(k) {
				continue
			}
			v, _ := env.Get(k)
			file := asStr(v)
			if !filepath.IsAbs(file) {
				continue
			}
			dir := filepath.Dir(filepath.Clean(file))
			if !paths.IsHostServicesSessionDirName(filepath.Base(dir)) {
				continue
			}
			found := false
			for _, d := range out {
				found = found || d == dir
			}
			if !found {
				out = append(out, dir)
			}
		}
	}
	return out
}
