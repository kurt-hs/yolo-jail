package configedit

import (
	"bytes"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/configdoc"
)

func TestPlanEditRemoveFirstMiddleLastArrayElements(t *testing.T) {
	cases := []struct {
		name, source, want string
		index              int
	}{
		{"first", `{"items":["a", "b", "c"]}`, `{"items":[ "b", "c"]}`, 0},
		{"middle", `{"items":["a", "b", "c"]}`, `{"items":["a", "c"]}`, 1},
		{"last", `{"items":["a", "b", "c"]}`, `{"items":["a", "b"]}`, 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, err := PlanEdit([]byte(tc.source), configdoc.JSONC, Mutation{Action: Remove, Path: []configdoc.Step{{Key: "items"}, {Index: tc.index, IsIndex: true}}})
			if err != nil {
				t.Fatal(err)
			}
			if string(p.Updated) != tc.want {
				t.Fatalf("want %q, got %q", tc.want, p.Updated)
			}
			if _, err := configdoc.Parse(p.Updated, configdoc.JSONC); err != nil {
				t.Fatalf("invalid result: %v", err)
			}
		})
	}
}

func TestPlanEditInlineAndSparseArrayTableMembers(t *testing.T) {
	inline := []byte("settings = { old = 1 }\n")
	p, err := PlanEdit(inline, configdoc.TOML, Mutation{Action: Add, Path: []configdoc.Step{{Key: "settings"}, {Key: "new/key"}}, Value: "# literal"})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(p.Updated), "settings = { old = 1, \"new/key\" = \"# literal\" }\n"; got != want {
		t.Fatalf("inline TOML want %q, got %q", want, got)
	}
	if _, err := configdoc.Parse(p.Updated, configdoc.TOML); err != nil {
		t.Fatalf("inline result invalid: %v", err)
	}

	sparse := []byte("[[program]]\nname = 'first'\n\n[[program]]\nname = 'second'\n")
	p, err = PlanEdit(sparse, configdoc.TOML, Mutation{Action: Add, Path: []configdoc.Step{{Key: "program"}, {Index: 1, IsIndex: true}, {Key: "enabled"}}, Value: true})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(p.Updated), "[[program]]\nname = 'first'\n\n[[program]]\nname = 'second'\nenabled = true\n"; got != want {
		t.Fatalf("sparse array-table want %q, got %q", want, got)
	}
	if _, err := configdoc.Parse(p.Updated, configdoc.TOML); err != nil {
		t.Fatalf("array-table result invalid: %v", err)
	}
}

func TestPlanEditCreatesMissingTOMLTableAtEnd(t *testing.T) {
	source := []byte("[providers]\nname = 'bedrock'")
	p, err := PlanEdit(source, configdoc.TOML, Mutation{Action: Add, Path: []configdoc.Step{{Key: "providers"}, {Key: "new.provider"}, {Key: "models"}, {Key: "x/y"}}, Value: "Sol"})
	if err != nil {
		t.Fatal(err)
	}
	want := "[providers]\nname = 'bedrock'\n\n[providers.\"new.provider\".models]\n\"x/y\" = \"Sol\"\n"
	if string(p.Updated) != want {
		t.Fatalf("want %q\ngot  %q", want, p.Updated)
	}
	if _, err := configdoc.Parse(p.Updated, configdoc.TOML); err != nil {
		t.Fatalf("new table invalid: %v\n%s", err, p.Updated)
	}
}

func TestPlanEditPreservesCRLFAndNoFinalNewlineForValueSet(t *testing.T) {
	source := []byte("value = 'old' # retain\r\nsecond = \"# string\"")
	p, err := PlanEdit(source, configdoc.TOML, Mutation{Action: Set, Path: []configdoc.Step{{Key: "value"}}, Value: "new"})
	if err != nil {
		t.Fatal(err)
	}
	want := []byte("value = \"new\" # retain\r\nsecond = \"# string\"")
	if !bytes.Equal(p.Updated, want) {
		t.Fatalf("want %q, got %q", want, p.Updated)
	}
	if !bytes.Equal(p.Updated[:p.Hunks[0].Start], source[:p.Hunks[0].Start]) {
		t.Fatal("prefix changed")
	}
	if !bytes.Equal(p.Updated[len(p.Updated)-(len(source)-p.Hunks[0].End):], source[p.Hunks[0].End:]) {
		t.Fatal("suffix changed")
	}
}

func TestPlanEditAppendDoesNotMoveArrayTrailingComments(t *testing.T) {
	cases := []struct {
		name, source, want string
		format             configdoc.Format
	}{
		{"jsonc", "{\n  values: [\n    'old', // item note\n  ],\n}", "{\n  values: [\n    'old', // item note\n    \"new\"\n  ],\n}", configdoc.JSONC},
		{"toml", "values = [\n  'old', # item note\n]\n", "values = [\n  'old', # item note\n  \"new\"\n]\n", configdoc.TOML},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, err := PlanEdit([]byte(tc.source), tc.format, Mutation{Action: Append, Path: []configdoc.Step{{Key: "values"}}, Value: "new"})
			if err != nil {
				t.Fatal(err)
			}
			if string(p.Updated) != tc.want {
				t.Fatalf("want %q, got %q", tc.want, p.Updated)
			}
			if _, err := configdoc.Parse(p.Updated, tc.format); err != nil {
				t.Fatalf("invalid: %v", err)
			}
		})
	}
}
