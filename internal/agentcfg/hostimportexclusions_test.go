package agentcfg

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/agentcfg/codec"
)

func TestHostImportExclusionsRemovesExactObjectMembers(t *testing.T) {
	input := []byte(`{"hooks":["keep?"],"nested":{"a/b":{"~key":1},"stay":2},"0":"numeric","empty":""}`)
	original := append([]byte(nil), input...)
	pointers := []string{"/hooks", "/nested/a~1b/~0key", "/0", "/missing", "/nested/stay/child"}

	got, err := FilterHostImportExclusions("json", input, pointers)
	if err != nil {
		t.Fatalf("FilterHostImportExclusions: %v", err)
	}
	want := map[string]any{
		"nested": map[string]any{"a/b": map[string]any{}, "stay": float64(2)},
		"empty":  "",
	}
	assertJSONObject(t, got, want)
	if !bytes.Equal(input, original) {
		t.Fatalf("input bytes mutated: got %q, want %q", input, original)
	}
}

func TestHostImportExclusionsRemovesLastMemberButKeepsEmptyParent(t *testing.T) {
	got, err := FilterHostImportExclusions("json", []byte(`{"nested":{"last":null}}`), []string{"/nested/last"})
	if err != nil {
		t.Fatalf("FilterHostImportExclusions: %v", err)
	}
	assertJSONObject(t, got, map[string]any{"nested": map[string]any{}})
}

func TestHostImportExclusionsRemovesEveryFinalValueShape(t *testing.T) {
	input := []byte(`{"null":null,"array":[1],"scalar":true,"object":{"child":1},"keep":0}`)
	got, err := FilterHostImportExclusions("json", input, []string{"/null", "/array", "/scalar", "/object"})
	if err != nil {
		t.Fatalf("FilterHostImportExclusions: %v", err)
	}
	assertJSONObject(t, got, map[string]any{"keep": float64(0)})
}

func TestHostImportExclusionsEmptyKeyEscapingAndNoOpPaths(t *testing.T) {
	input := []byte(`{"":{"slash/key":{"~name":1}},"arr":[{"key":1}],"scalar":3}`)
	got, err := FilterHostImportExclusions("json", input, []string{"//slash~1key/~0name", "/arr/0/key", "/scalar/child"})
	if err != nil {
		t.Fatalf("FilterHostImportExclusions: %v", err)
	}
	assertJSONObject(t, got, map[string]any{
		"":       map[string]any{"slash/key": map[string]any{}},
		"arr":    []any{map[string]any{"key": float64(1)}},
		"scalar": float64(3),
	})
	got, err = FilterHostImportExclusions("json", got, []string{"/"})
	if err != nil {
		t.Fatalf("remove empty-key member: %v", err)
	}
	assertJSONObject(t, got, map[string]any{"arr": []any{map[string]any{"key": float64(1)}}, "scalar": float64(3)})
}

func TestHostImportExclusionsOrderAndDuplicatesAreDeterministic(t *testing.T) {
	input := []byte(`{"a":{"b":1,"c":2},"keep":3}`)
	first, err := FilterHostImportExclusions("json", input, []string{"/a/b", "/a", "/a/c", "/a"})
	if err != nil {
		t.Fatalf("first filter: %v", err)
	}
	second, err := FilterHostImportExclusions("json", input, []string{"/a", "/a/c", "/a/b", "/a"})
	if err != nil {
		t.Fatalf("second filter: %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("pointer order changed output: %q != %q", first, second)
	}
	assertJSONObject(t, first, map[string]any{"keep": float64(3)})
}

func TestHostImportExclusionsReturnsOriginalWhenNoMemberIsRemoved(t *testing.T) {
	input := []byte("{\n  \"present\": 1\n}\n")
	got, err := FilterHostImportExclusions("json", input, []string{"/missing", "/present/child"})
	if err != nil {
		t.Fatalf("FilterHostImportExclusions: %v", err)
	}
	if !bytes.Equal(got, input) {
		t.Fatalf("no-op changed bytes: got %q, want %q", got, input)
	}
	if len(input) > 0 && &got[0] != &input[0] {
		t.Fatal("no-op did not return the original byte slice")
	}
}

func TestHostImportExclusionsNoPointersBypassesCodecAndSource(t *testing.T) {
	input := []byte("not valid json")
	got, err := FilterHostImportExclusions("unknown-codec", input, nil)
	if err != nil {
		t.Fatalf("empty exclusions: %v", err)
	}
	if len(got) == 0 || &got[0] != &input[0] {
		t.Fatal("empty exclusions did not return original byte slice")
	}
}

func TestHostImportExclusionsSupportsObjectCodecs(t *testing.T) {
	cases := []struct {
		name  string
		input []byte
	}{
		{name: "json", input: []byte(`{"remove":1,"keep":2}`)},
		{name: "toml", input: []byte("remove = 1\nkeep = 2\n")},
		{name: "yaml", input: []byte("remove: 1\nkeep: 2\n")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := FilterHostImportExclusions(tc.name, tc.input, []string{"/remove"})
			if err != nil {
				t.Fatalf("FilterHostImportExclusions: %v", err)
			}
			c, _ := codec.LookupCodec(tc.name)
			decoded, err := c.Decode(got)
			if err != nil {
				t.Fatalf("decode result: %v", err)
			}
			gotMap, ok := decoded.(map[string]any)
			if !ok || len(gotMap) != 1 || gotMap["keep"] == nil || !isNumberTwo(gotMap["keep"]) {
				t.Fatalf("decoded result = %#v, want only keep=2", decoded)
			}
		})
	}
}

func TestHostImportExclusionsRejectsMalformedPointersBeforeDecode(t *testing.T) {
	for _, pointer := range []string{"", "hooks", "/bad~2escape"} {
		t.Run(pointer, func(t *testing.T) {
			got, err := FilterHostImportExclusions("json", []byte("not json"), []string{pointer})
			if err == nil {
				t.Fatalf("FilterHostImportExclusions(%q) unexpectedly succeeded with %q", pointer, got)
			}
			if got != nil {
				t.Fatalf("error returned bytes %q; want nil", got)
			}
		})
	}
}

func TestHostImportExclusionsRejectsNonObjectCodecsAndDocuments(t *testing.T) {
	for _, codecName := range []string{"lines", "raw", "unknown-codec"} {
		got, err := FilterHostImportExclusions(codecName, []byte("value\n"), []string{"/value"})
		if err == nil || got != nil {
			t.Fatalf("codec %s result = %q, %v; want nil bytes and error", codecName, got, err)
		}
	}
	for _, input := range [][]byte{[]byte("not json"), []byte(`[1,2]`), []byte(`null`)} {
		got, err := FilterHostImportExclusions("json", input, []string{"/x"})
		if err == nil || got != nil {
			t.Fatalf("input %q result = %q, %v; want nil bytes and error", input, got, err)
		}
	}
}

func TestHostImportExclusionsEncodeFailureReturnsNoOriginalBytes(t *testing.T) {
	failing := failingEncodeCodec{}
	input := []byte(`{"remove":1}`)
	got, err := filterHostImportExclusions(failing, input, []string{"/remove"})
	if err == nil || got != nil {
		t.Fatalf("result = %q, %v; want nil bytes and encode error", got, err)
	}
}

type failingEncodeCodec struct{}

func (failingEncodeCodec) Decode(data []byte) (any, error) { return codec.JSON{}.Decode(data) }
func (failingEncodeCodec) Encode(any) ([]byte, error)      { return nil, errEncodeFixture }
func (failingEncodeCodec) Name() string                    { return "fixture" }

var errEncodeFixture = testError("fixture encode failure")

type testError string

func (e testError) Error() string { return string(e) }

func isNumberTwo(value any) bool {
	switch number := value.(type) {
	case int:
		return number == 2
	case int64:
		return number == 2
	case float64:
		return number == 2
	default:
		return false
	}
}

func assertJSONObject(t *testing.T, data []byte, want map[string]any) {
	t.Helper()
	decoded, err := (codec.JSON{}).Decode(data)
	if err != nil {
		t.Fatalf("decode result %q: %v", data, err)
	}
	if !reflect.DeepEqual(decoded, want) {
		t.Fatalf("decoded result = %#v, want %#v", decoded, want)
	}
}
