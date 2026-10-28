package arraymerge

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
)

func TestMergeAppendingPreservesOrderIdentityNullAndCopying(t *testing.T) {
	base := map[string]any{
		"arr":    []any{"base", int64(1), map[string]any{"n": int64(2)}},
		"nested": map[string]any{"keep": []any{"old"}},
	}
	patch := map[string]any{
		"arr":    []any{"new", float64(1), map[string]any{"n": float64(2)}, "new"},
		"nested": map[string]any{"add": []any{map[string]any{"k": "v"}}},
		"remove": nil,
	}
	got := MergeAppending(base, patch)
	want := map[string]any{
		"arr":    []any{"base", int64(1), map[string]any{"n": int64(2)}, "new"},
		"nested": map[string]any{"keep": []any{"old"}, "add": []any{map[string]any{"k": "v"}}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("MergeAppending = %#v, want %#v", got, want)
	}
	got["arr"].([]any)[0] = "changed"
	got["nested"].(map[string]any)["keep"].([]any)[0] = "changed"
	if base["arr"].([]any)[0] != "base" || base["nested"].(map[string]any)["keep"].([]any)[0] != "old" {
		t.Fatal("MergeAppending aliased nested input values")
	}
}

func TestEntryKeyNormalizesNumbersAndObjectKeyOrder(t *testing.T) {
	left := map[string]any{"outer": map[string]any{"a": int64(1), "b": []any{uint(2)}}}
	right := map[string]any{"outer": map[string]any{"b": []any{float64(2)}, "a": float64(1)}}
	if EntryKey(left) != EntryKey(right) {
		t.Fatalf("equivalent decoded entries got different keys: %q and %q", EntryKey(left), EntryKey(right))
	}
}

func TestEntryKeyNormalizesOrderedMapIntLiteralAndGoIntegers(t *testing.T) {
	literal, ok := jsonx.IntLiteral("12345678901234567890")
	if !ok {
		t.Fatal("IntLiteral rejected decimal value")
	}
	ordered := jsonx.NewOrderedMap()
	ordered.Set("n", literal)
	want := map[string]any{"n": jsonx.Plain(literal)}
	if EntryKey(ordered) != EntryKey(want) {
		t.Fatalf("OrderedMap/IntLiteral identity differs from Plain value: %q != %q", EntryKey(ordered), EntryKey(want))
	}
	for _, integer := range []any{int(1), int8(1), int16(1), int32(1), int64(1), uint(1), uint8(1), uint16(1), uint32(1), uint64(1)} {
		if EntryKey(integer) != EntryKey(float64(1)) {
			t.Errorf("Go integer %T got different identity than float64: %q", integer, EntryKey(integer))
		}
	}
}

func TestEntryKeyRetainsMarshalErrorFallback(t *testing.T) {
	value := map[string]any{"unsupported": make(chan int)}
	if got, want := EntryKey(value), fmt.Sprintf("%#v", value); got != want {
		t.Fatalf("marshal-error identity = %q, want original %#v fallback %q", got, value, want)
	}
}

func TestMergeAppendingNilInputsReturnIndependentObject(t *testing.T) {
	got := MergeAppending(nil, nil)
	if got == nil || len(got) != 0 {
		t.Fatalf("nil inputs = %#v, want a non-nil empty object", got)
	}
	got["new"] = true
	if len(got) != 1 {
		t.Fatal("returned object cannot be mutated")
	}
}
