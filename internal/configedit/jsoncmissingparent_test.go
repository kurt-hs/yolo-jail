package configedit

import (
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/configdoc"
)

func steps(keys ...string) []configdoc.Step {
	out := make([]configdoc.Step, len(keys))
	for i, k := range keys {
		out[i] = configdoc.Step{Key: k}
	}
	return out
}

func TestPlanEditAddJSONCCreatesMissingParentObjects(t *testing.T) {
	for _, tc := range []struct {
		name, in, want string
	}{
		{"member after existing", "{\n  // keep\n  \"a\": 1\n}\n", "{\n  // keep\n  \"a\": 1,\n  \"x\": {\n    \"y\": {\n      \"z\": 5\n    }\n  }\n}\n"},
		{"empty multiline object", "{\n  // keep\n}\n", "{\n  \"x\": {\n    \"y\": {\n      \"z\": 5\n    }\n  }\n  // keep\n}\n"},
		{"nested under existing parent", "{\n  \"x\": {\n    \"q\": 1\n  }\n}\n", "{\n  \"x\": {\n    \"q\": 1,\n    \"y\": {\n      \"z\": 5\n    }\n  }\n}\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plan, err := PlanEdit([]byte(tc.in), configdoc.JSONC, Mutation{Action: Add, Path: steps("x", "y", "z"), Value: int64(5)})
			if err != nil {
				t.Fatal(err)
			}
			if string(plan.Updated) != tc.want {
				t.Fatalf("want:\n%s\ngot:\n%s", tc.want, plan.Updated)
			}
			if _, err := configdoc.Parse(plan.Updated, configdoc.JSONC); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestPlanEditAddJSONCRefusesNonObjectAncestorNamingTheFix(t *testing.T) {
	_, err := PlanEdit([]byte("{\n  \"x\": \"str\"\n}\n"), configdoc.JSONC, Mutation{Action: Add, Path: steps("x", "y", "z"), Value: int64(5)})
	if err == nil || !strings.Contains(err.Error(), "rerun the command") {
		t.Fatalf("want a refusal naming the next step, got %v", err)
	}
}
