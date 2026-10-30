package configedit

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/configdoc"
)

func TestPlanEditSetPreservesJSONCBytesAndUsesPositionedPath(t *testing.T) {
	before := []byte("{\r\n  providers: {\r\n    'bedrock/claude': { model: 'old', note: '# not comment' }, // keep\r\n  },\r\n}\r\n")
	path := []configdoc.Step{{Key: "providers"}, {Key: "bedrock/claude"}, {Key: "model"}}
	plan, err := PlanEdit(before, configdoc.JSONC, Mutation{Action: Set, Path: path, Value: "new"})
	if err != nil {
		t.Fatal(err)
	}
	want := []byte("{\r\n  providers: {\r\n    'bedrock/claude': { model: \"new\", note: '# not comment' }, // keep\r\n  },\r\n}\r\n")
	if !bytes.Equal(plan.Updated, want) {
		t.Fatalf("splice differs\nwant %q\n got %q", want, plan.Updated)
	}
	if !bytes.Equal(plan.Original, before) {
		t.Fatal("PlanEdit modified its input")
	}
	if !bytes.Equal(plan.Updated[:plan.Hunks[0].Start], before[:plan.Hunks[0].Start]) {
		t.Fatal("prefix changed")
	}
	oldEnd := plan.Hunks[0].End
	if !bytes.Equal(plan.Updated[len(plan.Updated)-(len(before)-oldEnd):], before[oldEnd:]) {
		t.Fatal("suffix changed")
	}
}

func TestPlanEditAddRemoveJSONCAndTOMLKeepCommentsAndCRLF(t *testing.T) {
	jsonSource := []byte("{\r\n  \"settings\": {\r\n    \"a\": 1 // keep trailing\r\n  }\r\n}\r\n")
	add, err := PlanEdit(jsonSource, configdoc.JSONC, Mutation{Action: Add, Path: []configdoc.Step{{Key: "settings"}, {Key: "new/key"}}, Value: "# literal"})
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := configdoc.Parse(add.Updated, configdoc.JSONC)
	if err != nil {
		t.Fatalf("add did not parse: %v\n%s", err, add.Updated)
	}
	if got := string(add.Updated); !strings.Contains(got, "\"a\": 1, // keep trailing\r\n    \"new/key\": \"# literal\"") {
		t.Fatalf("unexpected addition: %q", got)
	}
	if !reflect.DeepEqual(parsed.Values, add.Expected) {
		t.Fatal("added effective values mismatch")
	}
	remove, err := PlanEdit(add.Updated, configdoc.JSONC, Mutation{Action: Remove, Path: []configdoc.Step{{Key: "settings"}, {Key: "new/key"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := configdoc.Parse(remove.Updated, configdoc.JSONC); err != nil {
		t.Fatalf("remove did not parse: %v\n%s", err, remove.Updated)
	}
	if !bytes.Contains(remove.Updated, []byte("// keep trailing")) {
		t.Fatalf("trailing comment lost: %q", remove.Updated)
	}

	tomlSource := []byte("[providers.\"bedrock/us\"]\r\nmodels.\"global/a.b\" = 'old' # key comment\r\n# tail\r\n")
	tomlPath := []configdoc.Step{{Key: "providers"}, {Key: "bedrock/us"}, {Key: "models"}, {Key: "global/a.b"}}
	set, err := PlanEdit(tomlSource, configdoc.TOML, Mutation{Action: Set, Path: tomlPath, Value: "# inside string"})
	if err != nil {
		t.Fatal(err)
	}
	if got := string(set.Updated); got != "[providers.\"bedrock/us\"]\r\nmodels.\"global/a.b\" = \"# inside string\" # key comment\r\n# tail\r\n" {
		t.Fatalf("TOML bytes changed unexpectedly: %q", got)
	}
	if _, err := configdoc.Parse(set.Updated, configdoc.TOML); err != nil {
		t.Fatalf("TOML set invalid: %v", err)
	}
}

func TestPlanEditAppendAndRemoveArrayItems(t *testing.T) {
	cases := []struct {
		name         string
		format       configdoc.Format
		source, want string
		path         []configdoc.Step
	}{
		{"json-inline", configdoc.JSONC, `{"values":["a","b"]}`, `{"values":["a","b", "c"]}`, []configdoc.Step{{Key: "values"}}},
		{"json-multiline", configdoc.JSONC, "{\n  values: [\n    'a', // retain\n    'b',\n  ],\n}", "{\n  values: [\n    'a', // retain\n    'b',\n    \"c\",\n  ],\n}", []configdoc.Step{{Key: "values"}}},
		{"toml-inline", configdoc.TOML, `values = ["a", "b"]`, `values = ["a", "b", "c"]`, []configdoc.Step{{Key: "values"}}},
		{"toml-multiline", configdoc.TOML, "values = [\n  'a', # retain\n  'b',\n]", "values = [\n  'a', # retain\n  'b',\n  \"c\",\n]", []configdoc.Step{{Key: "values"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plan, err := PlanEdit([]byte(tc.source), tc.format, Mutation{Action: Append, Path: tc.path, Value: "c"})
			if err != nil {
				t.Fatal(err)
			}
			if string(plan.Updated) != tc.want {
				t.Fatalf("want %q\n got %q", tc.want, plan.Updated)
			}
			if _, err := configdoc.Parse(plan.Updated, tc.format); err != nil {
				t.Fatalf("invalid output: %v", err)
			}
		})
	}
}

func TestPlanEditNewSiblingUsesTableStyleAndNoFinalNewline(t *testing.T) {
	cases := []struct {
		name         string
		format       configdoc.Format
		source, want string
		parent       []configdoc.Step
	}{
		{"json-empty", configdoc.JSONC, "{}", "{\n  \"added\": true\n}", nil},
		{"json-indented", configdoc.JSONC, "{\n  \"parent\": {\n    \"old\": 1\n  }\n}", "{\n  \"parent\": {\n    \"old\": 1,\n    \"added\": true\n  }\n}", []configdoc.Step{{Key: "parent"}}},
		{"toml-table", configdoc.TOML, "[parent]\nold = 1", "[parent]\nold = 1\nadded = true\n", []configdoc.Step{{Key: "parent"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := append(append([]configdoc.Step(nil), tc.parent...), configdoc.Step{Key: "added"})
			p, err := PlanEdit([]byte(tc.source), tc.format, Mutation{Action: Add, Path: path, Value: true})
			if err != nil {
				t.Fatal(err)
			}
			if string(p.Updated) != tc.want {
				t.Fatalf("want %q\n got %q", tc.want, p.Updated)
			}
			if _, err := configdoc.Parse(p.Updated, tc.format); err != nil {
				t.Fatalf("invalid: %v", err)
			}
		})
	}
}

func TestConfigEditApplyDryRunNoopModeAndVerifyRestore(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.jsonc")
	original := []byte(`{"packs":["one"]}`)
	if err := os.WriteFile(path, original, 0640); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(path)
	p, err := Apply(path, configdoc.JSONC, Mutation{Action: Append, Path: []configdoc.Step{{Key: "packs"}}, Value: "two"}, ApplyOptions{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if p.Noop || string(p.Updated) != `{"packs":["one", "two"]}` {
		t.Fatalf("dry-run plan: noop=%v updated=%q", p.Noop, p.Updated)
	}
	after, _ := os.ReadFile(path)
	now, _ := os.Stat(path)
	if !bytes.Equal(after, original) || !os.SameFile(info, now) {
		t.Fatal("dry run wrote the file")
	}
	p, err = Apply(path, configdoc.JSONC, Mutation{Action: Append, Path: []configdoc.Step{{Key: "packs"}}, Value: "one"}, ApplyOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !p.Noop {
		t.Fatal("duplicate array value was not recognized as noop")
	}
	after, _ = os.ReadFile(path)
	noopInfo, _ := os.Stat(path)
	if !bytes.Equal(after, original) || !os.SameFile(info, noopInfo) {
		t.Fatal("no-op edit wrote the file")
	}
	if err := os.WriteFile(path, original, 0640); err != nil {
		t.Fatal(err)
	}
	_, err = Apply(path, configdoc.JSONC, Mutation{Action: Append, Path: []configdoc.Step{{Key: "packs"}}, Value: "two"}, ApplyOptions{Verify: func(before, after []byte) error { return os.ErrInvalid }})
	if err == nil || !strings.Contains(err.Error(), "original bytes restored") || !strings.Contains(err.Error(), "before:") || !strings.Contains(err.Error(), "after:") {
		t.Fatalf("verification error lacks recovery details: %v", err)
	}
	after, _ = os.ReadFile(path)
	if !bytes.Equal(after, original) {
		t.Fatalf("failed verification did not restore exact bytes: %q", after)
	}
	mode, _ := os.Stat(path)
	if mode.Mode().Perm() != 0640 {
		t.Fatalf("permissions changed: %o", mode.Mode().Perm())
	}
}

func TestConfigEditApplyResolvesSymlinkTargetAndChecksPreimage(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "real.toml")
	alias := filepath.Join(dir, "link.toml")
	original := []byte("packs = []\n")
	if err := os.WriteFile(target, original, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, alias); err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(alias, configdoc.TOML, Mutation{Action: Append, Path: []configdoc.Step{{Key: "packs"}}, Value: "claude"}, ApplyOptions{ExpectedBytes: []byte("stale")}); err == nil || !strings.Contains(err.Error(), "changed since it was planned") {
		t.Fatalf("stale preimage accepted: %v", err)
	}
	if _, err := Apply(alias, configdoc.TOML, Mutation{Action: Append, Path: []configdoc.Step{{Key: "packs"}}, Value: "claude"}, ApplyOptions{ExpectedBytes: original}); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(target)
	if string(got) != "packs = [\"claude\"]\n" {
		t.Fatalf("target bytes %q", got)
	}
	if _, err := os.Lstat(alias); err != nil {
		t.Fatalf("symlink was replaced: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("the edit left files beside the config (RC-D34): %v", entries)
	}
}

// An edit of an existing file follows no file planted beside it: a symlink named like the old
// lock sidecar, which a repository can carry, is neither opened nor written, and no sidecar is
// made (RC-D34). The parent directory's lock is the one lock.
func TestConfigEditApplyLeavesNoSidecarAndFollowsNoPlantedOne(t *testing.T) {
	dir, ownerDir := t.TempDir(), t.TempDir()
	target := filepath.Join(dir, "yolo-jail.toml")
	if err := os.WriteFile(target, []byte("packs = []\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	victim := filepath.Join(ownerDir, "owned-file")
	const owned = "separately owned bytes\n"
	if err := os.WriteFile(victim, []byte(owned), 0o600); err != nil {
		t.Fatal(err)
	}
	planted := target + ".yolo-edit.lock"
	if err := os.Symlink(victim, planted); err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(target, configdoc.TOML, Mutation{Action: Append, Path: []configdoc.Step{{Key: "packs"}}, Value: "claude"}, ApplyOptions{}); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(victim); string(got) != owned {
		t.Fatalf("the edit wrote through a planted sidecar symlink: %q", got)
	}
	if err := os.Remove(planted); err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(target, configdoc.TOML, Mutation{Action: Append, Path: []configdoc.Step{{Key: "packs"}}, Value: "codex"}, ApplyOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(planted); !os.IsNotExist(err) {
		t.Fatalf("an edit of an existing file left a lock sidecar: %v", err)
	}
}
