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
