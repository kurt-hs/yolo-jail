package configmigrate

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/config"
	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
)

// launchResolvedEnv is the env a launch in ws resolves: the merged config the launch loads,
// through the resolver every launch path calls.
func launchResolvedEnv(t *testing.T, ws string) map[string]string {
	t.Helper()
	merged, err := config.LoadConfig(ws, false, func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	env, removals := config.ResolveEnvSourcesFull(ws, merged, nil)
	out := map[string]string{}
	for _, k := range env.Keys() {
		v, _ := env.Get(k)
		out[k], _ = v.(string)
	}
	for _, k := range removals {
		out[k] = "<removed>"
	}
	return out
}

// TestMigratedEnvSourcesKeepTheLaunchEnv is W3-O's finding: `yolo config migrate` proved a
// config with env_sources equal, and the converted config then lost its env at launch, because
// the proof compared the lowered `env.files` key the launch never read. Now the proof carries the
// launch's own env entry list, and the written files resolve to the env the old ones did.
func TestMigratedEnvSourcesKeepTheLaunchEnv(t *testing.T) {
	home := env(t)
	dir := filepath.Join(home, ".config", "yolo-jail")
	write(t, filepath.Join(dir, "user.env"), "FROM_USER=u\nSHARED=user\n")
	write(t, filepath.Join(dir, "config.jsonc"), `{"env_sources": ["user.env"]}`+"\n")
	ws := realDir(t)
	write(t, filepath.Join(ws, "dev.env"), "FROM_WS=w\nSHARED=ws\n")
	write(t, filepath.Join(ws, "yolo-jail.jsonc"), `{"env_sources": ["dev.env"]}`+"\n")

	before := launchResolvedEnv(t, ws)
	if want := map[string]string{"FROM_USER": "u", "FROM_WS": "w", "SHARED": "ws"}; !reflect.DeepEqual(before, want) {
		t.Fatalf("legacy fixture resolves %v, want %v", before, want)
	}

	p := plan(t, Request{UserConfigDir: dir, Workspace: ws})
	if !p.Equal {
		t.Fatalf("unequal: %+v %+v", p.Differences, p.NotConverted)
	}
	if !strings.Contains(p.canonical.env, "dev.env") || !strings.Contains(p.canonical.env, "user.env") {
		t.Fatalf("the proof does not carry the launch's env entries: %q", p.canonical.env)
	}
	if _, err := Apply(p, ApplyOptions{}); err != nil {
		t.Fatal(err)
	}
	if after := launchResolvedEnv(t, ws); !reflect.DeepEqual(after, before) {
		t.Fatalf("the converted config resolves a different env at launch:\n  before %v\n  after  %v", before, after)
	}
}

// TestTheProofNamesALaunchEnvDifference pins the comparison itself: two reads whose merged
// configs spell the same files in a different order resolve differently (a later file wins), and
// the difference is reported under the launch's env, not passed as equal.
func TestTheProofNamesALaunchEnvDifference(t *testing.T) {
	a := jsonx.NewOrderedMap()
	a.Set("env_sources", []any{"/x/a.env", "/x/b.env"})
	b := jsonx.NewOrderedMap()
	env := jsonx.NewOrderedMap()
	env.Set("files", []any{"/x/b.env", "/x/a.env"})
	b.Set("env", env)
	diffs := diffValues("the environment the launch resolves", launchEnv(a), launchEnv(b))
	if len(diffs) == 0 {
		t.Fatal("a reordered env file list was proved equal")
	}
	same := jsonx.NewOrderedMap()
	sameEnv := jsonx.NewOrderedMap()
	sameEnv.Set("files", []any{"/x/a.env", "/x/b.env"})
	same.Set("env", sameEnv)
	if diffs := diffValues("env", launchEnv(a), launchEnv(same)); len(diffs) != 0 {
		t.Fatalf("env_sources and its env.files spelling differ: %+v", diffs)
	}
}
