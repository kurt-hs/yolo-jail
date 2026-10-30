package config

import (
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
)

func TestAllowPackNudgesJudgesTablesAgainstThePackList(t *testing.T) {
	allow := jsonx.NewOrderedMap()
	for _, name := range []string{"aws-auth", "aws-aut", "claude", "off-one", "mounts"} {
		table := jsonx.NewOrderedMap()
		table.Set("x", "y")
		allow.Set(name, table)
	}
	allow.Set("off-one", "off")
	user := jsonx.NewOrderedMap()
	user.Set("allow", allow)
	facts := AllowPackFacts{
		Listed:       []string{"claude"},
		Names:        map[string]string{"aws-auth": "aws-auth", "claude": "claude", "off-one": "off-one"},
		LoginProfile: map[string]string{"claude": "profile"},
	}

	got := AllowPackNudges(user, nil, facts)

	kinds := map[string]AllowNudgeKind{}
	for _, n := range got {
		kinds[n.Table] = n.Kind
	}
	want := map[string]AllowNudgeKind{
		"aws-auth": AllowNudgeUnselected,
		"aws-aut":  AllowNudgeUnknown,
		"claude":   AllowNudgeLoginNoProfile,
	}
	if len(kinds) != len(want) {
		t.Fatalf("findings = %v, want exactly %v (mounts and an `off` tombstone are not findings)", kinds, want)
	}
	for table, kind := range want {
		if kinds[table] != kind {
			t.Errorf("allow.%s = %q, want %q", table, kinds[table], kind)
		}
	}
	for _, n := range got {
		if n.Kind == AllowNudgeUnknown && n.Closest != "aws-auth" {
			t.Errorf("closest for aws-aut = %q, want aws-auth", n.Closest)
		}
	}
}
