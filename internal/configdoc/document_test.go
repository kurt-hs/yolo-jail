package configdoc

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
)

func TestTOMLIndexFindsOnlyRequestedValue(t *testing.T) {
	source := []byte("# keep\r\n[active_provider]\r\nclaude = 'native' # keep too\r\n")
	doc, err := Parse(source, TOML)
	if err != nil {
		t.Fatal(err)
	}
	span, found, err := doc.Index.Value(Step{Key: "active_provider"}, Step{Key: "claude"})
	if err != nil || !found {
		t.Fatalf("value: %v %v", found, err)
	}
	if got := string(source[span.Start:span.End]); got != "'native'" {
		t.Fatalf("span includes unrelated bytes: %q", got)
	}
	line, col := doc.Index.Position(span.Start)
	if line != 3 || col != 10 {
		t.Fatalf("position: %d:%d", line, col)
	}
	if len(doc.Index.Comments()) != 2 {
		t.Fatal("comments not retained")
	}
}

func TestJSONCIndexFindsArrayNestedValue(t *testing.T) {
	source := []byte("{\n  // keep\n  packs: [{ git: 'https://example.test/a' }],\n}\n")
	doc, err := Parse(source, JSONC)
	if err != nil {
		t.Fatal(err)
	}
	span, found, err := doc.Index.Value(Step{Key: "packs"}, Step{Index: 0, IsIndex: true}, Step{Key: "git"})
	if err != nil || !found || string(source[span.Start:span.End]) != "'https://example.test/a'" {
		t.Fatalf("value span = %q, found %v, err %v", source[span.Start:span.End], found, err)
	}
	if got := string(source[doc.Index.Comments()[0].Start:doc.Index.Comments()[0].End]); got != "// keep" {
		t.Fatalf("comment span = %q", got)
	}
	member, ok, err := doc.Index.Member(Step{Key: "packs"}, Step{Index: 0, IsIndex: true}, Step{Key: "git"})
	if err != nil || !ok || !bytes.HasPrefix(source[member.Start:member.End], []byte("git:")) {
		t.Fatalf("JSONC nested member = %q, %v, %v", source[member.Start:member.End], ok, err)
	}
}

func TestJSONCUnrelatedDuplicateDoesNotPoisonLookup(t *testing.T) {
	source := []byte(`{"duplicate":1,"duplicate":2,"wanted":3}`)
	doc, err := Parse(source, JSONC)
	if err != nil {
		t.Fatal(err)
	}
	span, ok, err := doc.Index.Value(Step{Key: "wanted"})
	if err != nil || !ok || string(source[span.Start:span.End]) != "3" {
		t.Fatalf("unrelated duplicate lookup %q, %v, %v", source[span.Start:span.End], ok, err)
	}
	if _, _, err := doc.Index.Value(Step{Key: "duplicate"}); err == nil {
		t.Fatal("lookup of duplicated requested key did not report ambiguity")
	}
}

func TestTOMLIndexFindsQuotedDottedKeysAndBoundaries(t *testing.T) {
	source := []byte("# top\n[providers]\n[providers.\"my.provider\"]\nmodels.\"id.with.dots\" = [{ name = \"one\" }, { name = \"two\" }] # tail\n")
	doc, err := Parse(source, TOML)
	if err != nil {
		t.Fatal(err)
	}
	path := []Step{{Key: "providers"}, {Key: "my.provider"}, {Key: "models"}, {Key: "id.with.dots"}, {Index: 1, IsIndex: true}, {Key: "name"}}
	span, ok, err := doc.Index.Value(path...)
	if err != nil || !ok || string(source[span.Start:span.End]) != `"two"` {
		t.Fatalf("nested value %q, %v, %v", source[span.Start:span.End], ok, err)
	}
	member, ok, err := doc.Index.Member(path[:4]...)
	if err != nil || !ok || !bytes.Contains(source[member.Start:member.End], []byte("models.")) {
		t.Fatalf("member %q, %v, %v", source[member.Start:member.End], ok, err)
	}
	table, ok, err := doc.Index.Table(Step{Key: "providers"}, Step{Key: "my.provider"})
	if err != nil || !ok || !bytes.Contains(source[table.Start:table.End], []byte("models.")) {
		t.Fatalf("table %q, %v, %v", source[table.Start:table.End], ok, err)
	}
	item, ok, err := doc.Index.Item(path[:5]...)
	if err != nil || !ok || string(source[item.Start:item.End]) != `{ name = "two" }` {
		t.Fatalf("item %q, %v, %v", source[item.Start:item.End], ok, err)
	}
}

func TestIndexRejectsDuplicateKeysAndRetainsUnattachedComments(t *testing.T) {
	duplicate := []byte(`{"x":1,"x":2}`)
	jsonDoc, err := Parse(duplicate, JSONC)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := jsonDoc.Index.Value(Step{Key: "x"}); err == nil {
		t.Fatal("ambiguous duplicate JSONC key accepted")
	}
	if _, err := Parse([]byte("x = 1\nx = 2\n"), TOML); err == nil {
		t.Fatal("duplicate key accepted")
	}
	source := []byte("# before\nvalue = 1 # inline\n# after\n")
	doc, err := Parse(source, TOML)
	if err != nil {
		t.Fatal(err)
	}
	comments := doc.Index.Comments()
	if len(comments) != 3 || string(source[comments[0].Start:comments[0].End]) != "# before" || string(source[comments[2].Start:comments[2].End]) != "# after" {
		t.Fatalf("comments = %#v", comments)
	}
}

func TestTOMLCommentsInsideStringsAreNotComments(t *testing.T) {
	source := []byte("basic = \"inside # basic\"\nliteral = 'inside # literal'\nmultibasic = \"\"\"first # multiline\nsecond\"\"\"\nmultiliteral = '''first # literal multiline\nsecond''' # actual\n# trailing actual\n")
	doc, err := Parse(source, TOML)
	if err != nil {
		t.Fatal(err)
	}
	comments := doc.Index.Comments()
	if len(comments) != 2 || string(source[comments[0].Start:comments[0].End]) != "# actual" || string(source[comments[1].Start:comments[1].End]) != "# trailing actual" {
		t.Fatalf("TOML comment spans: %#v", comments)
	}
}

func TestEncodeTOMLOrdersNestedMapsDeterministically(t *testing.T) {
	first := jsonx.NewOrderedMap()
	first.Set("z", "last")
	first.Set("a", "first")
	root := jsonx.NewOrderedMap()
	root.Set("items", []any{map[string]any{"z": "last", "a": "first"}, first})
	one, err := Encode(root, TOML)
	if err != nil {
		t.Fatal(err)
	}
	two, err := Encode(root, TOML)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(one, two) {
		t.Fatalf("encoding is nondeterministic:\n%s\n---\n%s", one, two)
	}
	parsed, err := Parse(one, TOML)
	if err != nil {
		t.Fatalf("encoded TOML does not parse: %v\n%s", err, one)
	}
	got, _ := parsed.Values.Get("items")
	items, ok := got.([]any)
	if !ok || len(items) != 2 {
		t.Fatalf("semantic round trip = %#v; encoded %s; root keys %#v", got, one, parsed.Values.Keys())
	}
	for i, item := range items {
		m, ok := item.(*jsonx.OrderedMap)
		want := []string{"a", "z"}
		if i == 1 {
			want = []string{"z", "a"}
		}
		if !ok || !reflect.DeepEqual(m.Keys(), want) {
			t.Fatalf("item %d ordering = %#v, want %v", i, item, want)
		}
	}
}

func TestTOMLIndexIndexesArrayTablesAndMultilineStrings(t *testing.T) {
	source := []byte("[[program]]\r\nname = \"first\"\r\n[program.details]\r\nvalue = 1\r\n\r\n[[program]]\r\nname = '''multi\nline'''\r\n")
	doc, err := Parse(source, TOML)
	if err != nil {
		t.Fatal(err)
	}
	path := []Step{{Key: "program"}, {Index: 1, IsIndex: true}, {Key: "name"}}
	span, ok, err := doc.Index.Value(path...)
	if err != nil || !ok || string(source[span.Start:span.End]) != "'''multi\nline'''" {
		t.Fatalf("multiline value %q, %v, %v", source[span.Start:span.End], ok, err)
	}
	table, ok, err := doc.Index.Table(Step{Key: "program"}, Step{Index: 1, IsIndex: true})
	if err != nil || !ok || !bytes.Contains(source[table.Start:table.End], []byte("name =")) {
		t.Fatalf("array table %q, %v, %v", source[table.Start:table.End], ok, err)
	}
	nested := []Step{{Key: "program"}, {Index: 0, IsIndex: true}, {Key: "details"}, {Key: "value"}}
	span, ok, err = doc.Index.Value(nested...)
	if err != nil || !ok || string(source[span.Start:span.End]) != "1" {
		t.Fatalf("nested array-table value %q, %v, %v", source[span.Start:span.End], ok, err)
	}
	nestedTable, ok, err := doc.Index.Table(nested[:3]...)
	if err != nil || !ok || !bytes.Contains(source[nestedTable.Start:nestedTable.End], []byte("value = 1")) {
		t.Fatalf("nested array-table boundary %q, %v, %v", source[nestedTable.Start:nestedTable.End], ok, err)
	}
}

func TestTOMLArrayTablesRetainNestedParentContext(t *testing.T) {
	source := []byte("[[fruit]]\nname = \"apple\"\n[[fruit.variety]]\nname = \"red\"\n[[fruit.variety]]\nname = \"green\"\n[[fruit]]\nname = \"banana\"\n[[fruit.variety]]\nname = \"yellow\"\n")
	doc, err := Parse(source, TOML)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		path []Step
		want string
	}{
		{[]Step{{Key: "fruit"}, {Index: 0, IsIndex: true}, {Key: "variety"}, {Index: 1, IsIndex: true}, {Key: "name"}}, `"green"`},
		{[]Step{{Key: "fruit"}, {Index: 1, IsIndex: true}, {Key: "variety"}, {Index: 0, IsIndex: true}, {Key: "name"}}, `"yellow"`},
	}
	for _, tc := range cases {
		span, ok, err := doc.Index.Value(tc.path...)
		if err != nil || !ok || string(source[span.Start:span.End]) != tc.want {
			t.Errorf("nested array-table value %q, %v, %v; want %s", source[span.Start:span.End], ok, err, tc.want)
		}
	}
}

func TestTOMLIndexScansFourAndFiveQuoteMultilineStrings(t *testing.T) {
	cases := []struct{ sourceValue, want string }{
		{`""""body"""`, `"body`},
		{`"""""body"""`, `""body`},
		{`"""body""""`, `body"`},
		{`"""body"""""`, `body""`},
	}
	for _, tc := range cases {
		source := []byte("value = " + tc.sourceValue + "\n")
		doc, err := Parse(source, TOML)
		if err != nil {
			t.Fatalf("Parse(%q): %v", source, err)
		}
		span, ok, err := doc.Index.Value(Step{Key: "value"})
		if err != nil || !ok || string(source[span.Start:span.End]) != tc.sourceValue {
			t.Errorf("quote value span %q, %v, %v; want %q", source[span.Start:span.End], ok, err, tc.sourceValue)
		}
		value, _ := doc.Values.Get("value")
		if value != tc.want {
			t.Errorf("decoded quote value = %q, want %q", value, tc.want)
		}
	}
}

func TestParseRejectsTOMLDateTimeValuesRecursively(t *testing.T) {
	for _, source := range []string{
		"date = 1979-05-27\n",
		"[nested]\nclock = 07:32:00\n",
		"events = [{ at = 1979-05-27T07:32:00Z }]\n",
	} {
		if _, err := Parse([]byte(source), TOML); err == nil {
			t.Errorf("Parse(%q) accepted a TOML date/time config scalar", source)
		}
	}
}

func TestEncodeTOMLFloatsKeepTheirType(t *testing.T) {
	root := jsonx.NewOrderedMap()
	root.Set("fraction", float64(1))
	encoded, err := Encode(root, TOML)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := Parse(encoded, TOML)
	if err != nil {
		t.Fatal(err)
	}
	value, _ := parsed.Values.Get("fraction")
	if _, ok := value.(float64); !ok {
		t.Fatalf("encoded 1.0 decoded as %T (%v)", value, value)
	}
}

func TestPositionUsesUTF8CharacterColumns(t *testing.T) {
	doc, err := Parse([]byte("name = \"é\"\n"), TOML)
	if err != nil {
		t.Fatal(err)
	}
	span, ok, err := doc.Index.Value(Step{Key: "name"})
	if err != nil || !ok {
		t.Fatalf("value: %v %v", ok, err)
	}
	line, col := doc.Index.Position(span.Start + 3)
	if line != 1 || col != 10 {
		t.Fatalf("position after UTF-8 rune = %d:%d", line, col)
	}
	line, col = doc.Index.Position(len(doc.Bytes) + 20)
	if line != 2 || col != 1 {
		t.Fatalf("clamped position = %d:%d, want 2:1", line, col)
	}
	line, col = doc.Index.Position(-1)
	if line != 1 || col != 1 {
		t.Fatalf("negative position = %d:%d, want 1:1", line, col)
	}
}

func TestEncodeValueSupportsTOMLAndJSONC(t *testing.T) {
	for _, format := range []Format{TOML, JSONC} {
		encoded, err := EncodeValue("3.12", format)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := Parse(valueDocument(format, encoded), format); err != nil {
			t.Fatalf("encoded value did not parse for %v: %v", format, err)
		}
	}
}

func valueDocument(format Format, value []byte) []byte {
	if format == TOML {
		return append(append([]byte("value = "), value...), '\n')
	}
	return append(append([]byte(`{"value":`), value...), '}')
}
