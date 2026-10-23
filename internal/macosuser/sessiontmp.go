package macosuser

// sessiontmp.go is the run-as-you sandbox's OWN TEMP DIR and the host-services dirs its profile
// grants by exact path (docs/design/macos-user-model-plan.md, MMP-D15 and MMP-D17).

import (
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
// tmux-<uid>/default under TMUX_TMPDIR. DARWIN_USER_TEMP_DIR is about 49 bytes, so the dir is
// about 63 and tmux's socket about 80.
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
// followers then follow that value instead. "" sets nothing.
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
	for _, kv := range [][2]string{{"TMUX_TMPDIR", dir}, {"TMPPREFIX", dir + "/zsh"}} {
		if old, ok := env.Get(kv[0]); !ok || asStr(old) == "" {
			env = withEnvVar(env, kv[0], kv[1])
		}
	}
	return env
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
