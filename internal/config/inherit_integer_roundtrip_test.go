package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/configdoc"
	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

func TestGeneratedNestedIntegersRoundTripThroughPublicUserReader(t *testing.T) {
	effective, err := jsonx.Decode([]byte(`{"resources":{"cpus":4,"pids_limit":4096},"network":{"ports":["8000:80"],"forward_host_ports":[9000]}}`))
	if err != nil {
		t.Fatal(err)
	}
	filtered, unknown, err := FilterInheritErr(effective.(*jsonx.OrderedMap), InheritNested)
	if err != nil || len(unknown) != 0 {
		t.Fatalf("FilterInheritErr unknown=%v err=%v", unknown, err)
	}
	body, format, fallback, err := RenderFilteredForDelivery(filtered, nil, InheritNested, "")
	if err != nil {
		t.Fatal(err)
	}
	if format != configdoc.TOML || fallback != "" {
		t.Fatalf("integer values unexpectedly used legacy fallback: format=%v fallback=%q", format, fallback)
	}

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("YOLO_VERSION", "9.9.9-test")
	t.Setenv(UserLayerEnv, "")
	userDir := filepath.Join(home, ".config", "yolo-jail")
	if err := os.MkdirAll(userDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(userDir, paths.UserConfigLegacyJSONCName), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(userDir, "inherited-launch.toml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := LoadReadableUserScopeConfig(true, func(string) {})
	if err != nil {
		t.Fatalf("public strict user reader rejected generated TOML: %v\n%s", err, body)
	}
	want, _ := projectLegacyB2Config(jsonx.DeepCopyMap(filtered), nil)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("public reader value differs from projected producer value:\n got %#v\nwant %#v", got, want)
	}
}
