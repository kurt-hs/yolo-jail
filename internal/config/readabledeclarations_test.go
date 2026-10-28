package config

import (
	"reflect"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/json5"
	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
	"github.com/mschulkind-oss/yolo-jail/internal/loopholedecl"
)

func editReadableMap(t *testing.T, text string) *jsonx.OrderedMap {
	t.Helper()
	value, err := json5.Decode([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	mapping, ok := value.(*jsonx.OrderedMap)
	if !ok {
		t.Fatalf("decoded value %T, want object", value)
	}
	return mapping
}

func TestReadableBrokerDeclarationClonesExplicitPendingMetadata(t *testing.T) {
	original := loopholedecl.Brokered{
		Source: "github", RemoteHost: "github.com",
		ReadCapability:      "read",
		KindCapabilities:    map[string]string{"create-pr": "create_pr", "create-issue": "create_issue"},
		PendingCapabilities: []string{"create_pr", "create_issue"},
	}
	before := loopholedecl.Brokered{
		Source: original.Source, RemoteHost: original.RemoteHost, ReadCapability: original.ReadCapability,
		KindCapabilities: cloneStringMap(original.KindCapabilities), PendingCapabilities: append([]string(nil), original.PendingCapabilities...),
	}
	declaration := readableBrokerDeclaration("github", "github-broker", original, nil)
	if !reflect.DeepEqual(original, before) {
		t.Fatalf("adapter mutated source declaration: got %#v, want %#v", original, before)
	}
	if !reflect.DeepEqual(declaration.Brokered.Grants, []string{"create-issue", "create-pr"}) {
		t.Fatalf("pending-only mapped grant kinds = %#v", declaration.Brokered.Grants)
	}
	if !reflect.DeepEqual(declaration.PendingCapabilities, original.PendingCapabilities) {
		t.Fatalf("pending capabilities = %#v", declaration.PendingCapabilities)
	}

	plan, err := ResolveReadableAllowance(ReadableAllowanceInput{
		User:          editReadableMap(t, `{"allow":{"github":{"always":["read"],"on_request":["create_pr","create_issue"]}}}`),
		Workspace:     editReadableMap(t, `{"requests":{"github":{"create_pr":true,"create_issue":true}}}`),
		SelectedPacks: []string{"github"}, Declarations: []ReadableBrokerDeclaration{declaration},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Requests) != 2 {
		t.Fatalf("requests = %#v, want two pending requests", plan.Requests)
	}
	for _, request := range plan.Requests {
		if request.Status != ReadableRequestPending {
			t.Errorf("request %s status %q, want pending-runtime", request.Path, request.Status)
		}
	}
	if !reflect.DeepEqual(original, before) {
		t.Fatalf("allowance projection mutated source declaration: got %#v, want %#v", original, before)
	}
}
