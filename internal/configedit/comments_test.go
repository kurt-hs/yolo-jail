package configedit

import (
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/configdoc"
)

func TestPlanEditRefusesToDeleteCommentsInsideReplacedValues(t *testing.T) {
	source := []byte("{\n  nested: {\n    value: 1, // retain within nested object\n  },\n  items: [{\n    name: 'one', // retain within array item\n  }],\n}\n")
	cases := []Mutation{
		{Action: Set, Path: []configdoc.Step{{Key: "nested"}}, Value: map[string]any{"value": 2}},
		{Action: Remove, Path: []configdoc.Step{{Key: "nested"}}},
		{Action: Remove, Path: []configdoc.Step{{Key: "items"}, {Index: 0, IsIndex: true}}},
	}
	for i, mutation := range cases {
		if _, err := PlanEdit(source, configdoc.JSONC, mutation); err == nil || !strings.Contains(err.Error(), "containing comments") {
			t.Errorf("case %d did not protect nested comment: %v", i, err)
		}
	}
}

func TestPlanEditAddsAfterInlineBlockCommentWithoutReattachingIt(t *testing.T) {
	source := []byte(`{"old": 1 /* keep this */}`)
	plan, err := PlanEdit(source, configdoc.JSONC, Mutation{Action: Add, Path: []configdoc.Step{{Key: "new"}}, Value: 2})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(plan.Updated), `{"old": 1, /* keep this */ "new": 2}`; got != want {
		t.Fatalf("want %q, got %q", want, got)
	}
	if _, err := configdoc.Parse(plan.Updated, configdoc.JSONC); err != nil {
		t.Fatalf("block-comment insertion invalid: %v", err)
	}
}

func TestPlanEditAddsAfterMultilineBlockComment(t *testing.T) {
	source := []byte("{\n  old: 1 /* first line\n    second line */\n}\n")
	plan, err := PlanEdit(source, configdoc.JSONC, Mutation{Action: Add, Path: []configdoc.Step{{Key: "new"}}, Value: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := configdoc.Parse(plan.Updated, configdoc.JSONC); err != nil {
		t.Fatalf("multiline block-comment insertion invalid: %v\n%s", err, plan.Updated)
	}
	if !strings.Contains(string(plan.Updated), "/* first line\n    second line */\n  \"new\"") {
		t.Fatalf("comment position changed: %q", plan.Updated)
	}
}
