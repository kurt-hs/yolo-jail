package configedit

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/configdoc"
)

func TestPlanEditSetContainerPreservesNestedComments(t *testing.T) {
	cases := []struct {
		name   string
		format configdoc.Format
		source string
		value  any
	}{
		{"jsonc nested line and block", configdoc.JSONC, "{\n  nested: {\n    value: 1, // keep-line\n    other: 2 /* keep-block */,\n  },\n  after: true,\n}\n", map[string]any{"value": 3, "other": 4}},
		{"toml array comment CRLF", configdoc.TOML, "nested = [\r\n  1, # keep-toml\r\n  2,\r\n]\r\nafter = true\r\n", []any{3, 4}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			source := []byte(tc.source)
			plan, err := PlanEdit(source, tc.format, Mutation{Action: Set, Path: []configdoc.Step{{Key: "nested"}}, Value: tc.value})
			if err != nil {
				t.Fatal(err)
			}
			assertCommentsPreserved(t, source, plan.Updated, tc.format)
			assertHunksPreserveUnrequested(t, source, plan.Updated, plan.Hunks)
			assertEffective(t, plan.Updated, tc.format, []configdoc.Step{{Key: "nested"}}, tc.value)
			if !bytes.Contains(plan.Updated, []byte("after = true")) && tc.format == configdoc.TOML || !bytes.Contains(plan.Updated, []byte("after: true")) && tc.format == configdoc.JSONC {
				t.Fatalf("unrequested suffix changed: %q", plan.Updated)
			}
		})
	}
}

func TestPlanEditRemoveCommentedArrayItemsAndKeys(t *testing.T) {
	cases := []struct {
		name string
		data string
		path []configdoc.Step
		want any
	}{
		{"array first map", "{items: [{\n  name: 'first', // keep-first\n}, {name: 'middle'}, {name: 'last'}], after: 1}\n", []configdoc.Step{{Key: "items"}, {Index: 0, IsIndex: true}}, []any{map[string]any{"name": "middle"}, map[string]any{"name": "last"}}},
		{"array middle map", "{items: [{name: 'first'}, {\n  name: 'middle', /* keep-middle */\n}, {name: 'last'}], after: 1}\n", []configdoc.Step{{Key: "items"}, {Index: 1, IsIndex: true}}, []any{map[string]any{"name": "first"}, map[string]any{"name": "last"}}},
		{"array last map", "{items: [{name: 'first'}, {\n  name: 'last', // keep-last\n}], after: 1}\n", []configdoc.Step{{Key: "items"}, {Index: 1, IsIndex: true}}, []any{map[string]any{"name": "first"}}},
		{"key with comment in value", "{\n  settings: {\n    remove: {nested: 1, /* keep-key */},\n    keep: true,\n  },\n  after: 2,\n}\n", []configdoc.Step{{Key: "settings"}, {Key: "remove"}}, nil},
		{"toml key with array comment", "items = [\r\n  1, # keep-key\r\n  2,\r\n]\r\nafter = 2\r\n", []configdoc.Step{{Key: "items"}}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			format := configdoc.JSONC
			if strings.HasPrefix(tc.data, "items =") {
				format = configdoc.TOML
			}
			source := []byte(tc.data)
			plan, err := PlanEdit(source, format, Mutation{Action: Remove, Path: tc.path})
			if err != nil {
				t.Fatal(err)
			}
			assertCommentsPreserved(t, source, plan.Updated, format)
			assertHunksPreserveUnrequested(t, source, plan.Updated, plan.Hunks)
			doc, err := configdoc.Parse(plan.Updated, format)
			if err != nil {
				t.Fatalf("invalid result: %v\n%s", err, plan.Updated)
			}
			if tc.want != nil {
				got, ok := lookup(doc.Values, tc.path[:len(tc.path)-1])
				want, wantErr := semanticValue(tc.want, format)
				if wantErr != nil {
					t.Fatal(wantErr)
				}
				if !ok || !reflect.DeepEqual(got, want) {
					t.Fatalf("effective parent = %#v, want %#v", got, want)
				}
			} else if format == configdoc.JSONC {
				if _, ok := lookup(doc.Values, tc.path); ok {
					t.Fatalf("removed key remains: %#v", doc.Values)
				}
			} else if _, ok := doc.Values.Get("items"); ok {
				t.Fatalf("removed key remains: %#v", doc.Values)
			}
		})
	}
}

func TestConfigEditApplyCommentedSetIsAtomicAndVerifies(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.jsonc")
	before := []byte("{nested: {value: 1, // keep-apply\n}, after: true}\n")
	if err := os.WriteFile(path, before, 0600); err != nil {
		t.Fatal(err)
	}
	var verified bool
	plan, err := Apply(path, configdoc.JSONC, Mutation{Action: Set, Path: []configdoc.Step{{Key: "nested"}}, Value: map[string]any{"value": 2}}, ApplyOptions{Verify: func(_, after []byte) error {
		verified = true
		assertCommentsPreserved(t, before, after, configdoc.JSONC)
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !verified {
		t.Fatal("Apply skipped caller verification")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	assertCommentsPreserved(t, before, got, configdoc.JSONC)
	assertEffective(t, got, configdoc.JSONC, []configdoc.Step{{Key: "nested"}}, map[string]any{"value": 2})
	if !bytes.Equal(got, plan.Updated) {
		t.Fatalf("file differs from plan: %q != %q", got, plan.Updated)
	}
	if err := os.WriteFile(path, before, 0600); err != nil {
		t.Fatal(err)
	}
	_, err = Apply(path, configdoc.JSONC, Mutation{Action: Set, Path: []configdoc.Step{{Key: "nested"}}, Value: map[string]any{"value": 2}}, ApplyOptions{Verify: func([]byte, []byte) error { return errors.New("deliberate rollback") }})
	if err == nil || !strings.Contains(err.Error(), "original bytes restored") {
		t.Fatalf("verification failure did not restore: %v", err)
	}
	restored, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(restored, before) {
		t.Fatalf("rollback did not restore exact original bytes: %q", restored)
	}
}

func TestConfigEditCommentScannerIgnoresQuotedCommentMarkers(t *testing.T) {
	cases := []struct {
		format configdoc.Format
		source string
		value  any
	}{
		{configdoc.JSONC, "{text: 'inside // not a comment /* either */', nested: {n: 1}}", map[string]any{"n": 2}},
		{configdoc.TOML, "text = \"inside # not a comment\"\nnested = [1, 2]\n", []any{3, 4}},
	}
	for _, tc := range cases {
		plan, err := PlanEdit([]byte(tc.source), tc.format, Mutation{Action: Set, Path: []configdoc.Step{{Key: "nested"}}, Value: tc.value})
		if err != nil {
			t.Fatal(err)
		}
		assertEffective(t, plan.Updated, tc.format, []configdoc.Step{{Key: "nested"}}, tc.value)
	}
}

func assertHunksPreserveUnrequested(t *testing.T, before, after []byte, hunks []Hunk) {
	t.Helper()
	oldOffset, newOffset := 0, 0
	for _, hunk := range hunks {
		if hunk.Start < oldOffset || hunk.End < hunk.Start || hunk.End > len(before) {
			t.Fatalf("invalid or overlapping hunk: %#v", hunk)
		}
		untouched := before[oldOffset:hunk.Start]
		if newOffset+len(untouched) > len(after) || !bytes.Equal(untouched, after[newOffset:newOffset+len(untouched)]) {
			t.Fatalf("unrequested bytes changed before hunk %#v", hunk)
		}
		if !bytes.Equal(before[hunk.Start:hunk.End], hunk.Before) {
			t.Fatalf("hunk preimage does not match original: %#v", hunk)
		}
		oldOffset = hunk.End
		newOffset += len(untouched) + len(hunk.After)
	}
	if !bytes.Equal(before[oldOffset:], after[newOffset:]) {
		t.Fatalf("unrequested suffix changed: before %q, after %q", before[oldOffset:], after[newOffset:])
	}
}

func assertCommentsPreserved(t *testing.T, before, after []byte, format configdoc.Format) {
	t.Helper()
	oldDoc, err := configdoc.Parse(before, format)
	if err != nil {
		t.Fatal(err)
	}
	newDoc, err := configdoc.Parse(after, format)
	if err != nil {
		t.Fatal(err)
	}
	comments := func(data []byte, doc *configdoc.Document) [][]byte {
		var got [][]byte
		for _, span := range doc.Index.Comments() {
			got = append(got, append([]byte(nil), data[span.Start:span.End]...))
		}
		return got
	}
	old, got := comments(before, oldDoc), comments(after, newDoc)
	if !reflect.DeepEqual(old, got) {
		t.Fatalf("comment byte sequence changed: before %q, after %q\n%s", old, got, after)
	}
}

func assertEffective(t *testing.T, data []byte, format configdoc.Format, path []configdoc.Step, want any) {
	t.Helper()
	doc, err := configdoc.Parse(data, format)
	if err != nil {
		t.Fatalf("parse: %v\n%s", err, data)
	}
	got, ok := lookup(doc.Values, path)
	normalized, normErr := semanticValue(want, format)
	if normErr != nil {
		t.Fatal(normErr)
	}
	if !ok || !reflect.DeepEqual(got, orderedLike(got, normalized)) {
		t.Fatalf("effective value = %#v, want %#v\n%s", got, normalized, data)
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
