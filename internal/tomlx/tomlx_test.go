package tomlx

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
)

func TestDecodeOrderedPreservesNestedArrayTableHeaderSegmentsRegardlessOfInlineSibling(t *testing.T) {
	inline := `[[fruit]]
name = "inline"
variety = [{ z = 10, a = 11 }, { y = 12, x = 13 }]
`
	headers := `[[fruit]]
name = "headers"
[[fruit.variety]]
z = 20
a = 21
[[fruit.variety]]
y = 22
x = 23
`
	for _, tc := range []struct {
		name   string
		source string
	}{
		{name: "inline parent before header parent", source: inline + headers},
		{name: "header parent before inline parent", source: headers + inline},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ordered, err := DecodeOrdered([]byte(tc.source))
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := Decode([]byte(tc.source))
			if err != nil {
				t.Fatal(err)
			}
			if got := normalizeDecodedValue(ordered); !reflect.DeepEqual(got, normalizeDecodedValue(decoded)) {
				t.Fatalf("ordered semantic value = %#v, Decode = %#v", got, normalizeDecodedValue(decoded))
			}
			parents, _ := ordered.Get("fruit")
			rows := parents.([]any)
			for i, row := range rows {
				parent := row.(*jsonx.OrderedMap)
				name, _ := parent.Get("name")
				varieties, _ := parent.Get("variety")
				children := varieties.([]any)
				wantKeys := [][]string{{"z", "a"}, {"y", "x"}}
				wantValues := [][]int64{{10, 11}, {12, 13}}
				wantName := "inline"
				if name == "headers" {
					wantValues = [][]int64{{20, 21}, {22, 23}}
					wantName = "headers"
				}
				if name != wantName || len(children) != 2 {
					t.Fatalf("fruit[%d] = %#v, want %q parent with two children", i, parent, wantName)
				}
				for j, child := range children {
					childMap := child.(*jsonx.OrderedMap)
					if got := childMap.Keys(); !reflect.DeepEqual(got, wantKeys[j]) {
						t.Errorf("%s child %d keys = %v, want %v", name, j, got, wantKeys[j])
					}
					for k, key := range wantKeys[j] {
						if got, _ := childMap.Get(key); got != wantValues[j][k] {
							t.Errorf("%s child %d.%s = %#v, want %d", name, j, key, got, wantValues[j][k])
						}
					}
				}
			}
		})
	}
}

func normalizeDecodedValue(value any) any {
	switch v := value.(type) {
	case *jsonx.OrderedMap:
		out := make(map[string]any, v.Len())
		for _, key := range v.Keys() {
			child, _ := v.Get(key)
			out[key] = normalizeDecodedValue(child)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, child := range v {
			out[key] = normalizeDecodedValue(child)
		}
		return out
	case []map[string]any:
		out := make([]any, len(v))
		for i, child := range v {
			out[i] = normalizeDecodedValue(child)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, child := range v {
			out[i] = normalizeDecodedValue(child)
		}
		return out
	default:
		return value
	}
}

func TestDecodeOrderedPreservesSourceOrderRepeatedly(t *testing.T) {
	for range 2 {
		values, err := DecodeOrdered([]byte(`items = [{ z = "last", a = "first" }, { b = 2, a = 1 }]`))
		if err != nil {
			t.Fatal(err)
		}
		value, _ := values.Get("items")
		items, ok := value.([]any)
		if !ok || len(items) != 2 {
			t.Fatalf("items = %#v", value)
		}
		for i, item := range items {
			m, ok := item.(*jsonx.OrderedMap)
			if !ok {
				t.Fatalf("item %d = %T", i, item)
			}
			want := []string{"z", "a"}
			if i == 1 {
				want = []string{"b", "a"}
			}
			if !reflect.DeepEqual(m.Keys(), want) {
				t.Fatalf("item %d keys = %v, want %v", i, m.Keys(), want)
			}
		}
	}
}

func TestDecodeOrderedPreservesSourceOrderInMapsInsideArrays(t *testing.T) {
	values, err := DecodeOrdered([]byte(`items = [{ z = 1, a = 2 }, { m = 3, b = 4 }]`))
	if err != nil {
		t.Fatal(err)
	}
	value, _ := values.Get("items")
	items, ok := value.([]any)
	if !ok || len(items) != 2 {
		t.Fatalf("items = %#v", value)
	}
	for i, item := range items {
		m, ok := item.(*jsonx.OrderedMap)
		if !ok {
			t.Fatalf("item %d = %T", i, item)
		}
		want := []string{"z", "a"}
		if i == 1 {
			want = []string{"m", "b"}
		}
		if !reflect.DeepEqual(m.Keys(), want) {
			t.Errorf("item %d source order = %v, want %v", i, m.Keys(), want)
		}
	}
}

func TestDecodeOrderedPreservesNestedMapOrderInsideArrays(t *testing.T) {
	values, err := DecodeOrdered([]byte(`items = [{ outer = { z = 1, a = 2 }, tail = true }]`))
	if err != nil {
		t.Fatal(err)
	}
	value, _ := values.Get("items")
	items := value.([]any)
	row := items[0].(*jsonx.OrderedMap)
	if !reflect.DeepEqual(row.Keys(), []string{"outer", "tail"}) {
		t.Fatalf("row order = %v", row.Keys())
	}
	outer, _ := row.Get("outer")
	if got := outer.(*jsonx.OrderedMap).Keys(); !reflect.DeepEqual(got, []string{"z", "a"}) {
		t.Fatalf("nested row order = %v", got)
	}
}

func TestDecodeOrderedPreservesArrayTableSourceOrder(t *testing.T) {
	values, err := DecodeOrdered([]byte("[[program]]\nz = 1\na = 2\n[[program]]\nm = 3\nb = 4\n"))
	if err != nil {
		t.Fatal(err)
	}
	value, _ := values.Get("program")
	items, ok := value.([]any)
	if !ok || len(items) != 2 {
		t.Fatalf("program = %#v", value)
	}
	for i, item := range items {
		row, ok := item.(*jsonx.OrderedMap)
		if !ok {
			t.Fatalf("row %d = %T", i, item)
		}
		want := []string{"z", "a"}
		if i == 1 {
			want = []string{"m", "b"}
		}
		if !reflect.DeepEqual(row.Keys(), want) {
			t.Errorf("row %d order = %v, want %v", i, row.Keys(), want)
		}
	}
}

func TestDecodeOrderedPreservesOrderAfterEarlierTablesAndValues(t *testing.T) {
	source := []byte("title = \"x\"\n[settings]\nenabled = true\nvalues = [{ z = 1, a = 2 }, { b = 3, a = 4 }]\n[[program]]\nz = 1\na = 2\n[[program]]\nm = 3\nb = 4\n")
	values, err := DecodeOrdered(source)
	if err != nil {
		t.Fatal(err)
	}
	settings, _ := values.Get("settings")
	rows, _ := settings.(*jsonx.OrderedMap).Get("values")
	for i, row := range rows.([]any) {
		m := row.(*jsonx.OrderedMap)
		want := []string{"z", "a"}
		if i == 1 {
			want = []string{"b", "a"}
		}
		if !reflect.DeepEqual(m.Keys(), want) {
			t.Errorf("settings.values[%d] order = %v, want %v", i, m.Keys(), want)
		}
	}
	programs, _ := values.Get("program")
	for i, row := range programs.([]any) {
		m := row.(*jsonx.OrderedMap)
		want := []string{"z", "a"}
		if i == 1 {
			want = []string{"m", "b"}
		}
		if !reflect.DeepEqual(m.Keys(), want) {
			t.Errorf("program[%d] order = %v, want %v", i, m.Keys(), want)
		}
	}
}

func TestDecodeOrderedPreservesOrderInNestedArraysOfInlineTables(t *testing.T) {
	values, err := DecodeOrdered([]byte("title = \"x\"\nitems = [{ outer = { z = 1, a = 2 }, children = [{ y = 1, x = 2 }] }]"))
	if err != nil {
		t.Fatal(err)
	}
	items, _ := values.Get("items")
	row := items.([]any)[0].(*jsonx.OrderedMap)
	outer, _ := row.Get("outer")
	if got := outer.(*jsonx.OrderedMap).Keys(); !reflect.DeepEqual(got, []string{"z", "a"}) {
		t.Fatalf("outer order = %v", got)
	}
	children, _ := row.Get("children")
	child := children.([]any)[0].(*jsonx.OrderedMap)
	if got := child.Keys(); !reflect.DeepEqual(got, []string{"y", "x"}) {
		t.Fatalf("nested array order = %v", got)
	}
}

func TestDecodeOrderedPreservesMixedNestedArrayOrder(t *testing.T) {
	source := []byte("title = \"x\"\nmatrix = [[{ z = 1, a = 2 }, { y = 3, x = 4 }], [{ p = 5, o = 6 }]]\n")
	values, err := DecodeOrdered(source)
	if err != nil {
		t.Fatal(err)
	}
	matrix, _ := values.Get("matrix")
	rows := matrix.([]any)
	want := [][][]string{{{"z", "a"}, {"y", "x"}}, {{"p", "o"}}}
	for i, row := range rows {
		inner := row.([]any)
		for j, item := range inner {
			got := item.(*jsonx.OrderedMap).Keys()
			if !reflect.DeepEqual(got, want[i][j]) {
				t.Errorf("matrix[%d][%d] order = %v, want %v", i, j, got, want[i][j])
			}
		}
	}
}

func TestDecodeOrderedPreservesInlineTablesInsideArrayTables(t *testing.T) {
	source := []byte("title = \"x\"\n[[program]]\nconfig = { z = 1, a = 2 }\n[[program]]\nconfig = { y = 3, x = 4 }\n")
	values, err := DecodeOrdered(source)
	if err != nil {
		t.Fatal(err)
	}
	programs, _ := values.Get("program")
	for i, row := range programs.([]any) {
		ordered := row.(*jsonx.OrderedMap)
		config, _ := ordered.Get("config")
		want := []string{"z", "a"}
		if i == 1 {
			want = []string{"y", "x"}
		}
		if got := config.(*jsonx.OrderedMap).Keys(); !reflect.DeepEqual(got, want) {
			t.Errorf("program[%d].config = %v, want %v", i, got, want)
		}
	}
}

func TestDecodeOrderedPreservesRepeatedNestedArrayTableAuthoredOrder(t *testing.T) {
	source := []byte("[[fruit]]\nname = \"apple\"\n[[fruit.variety]]\nname = \"green\"\ntail = 1\n[[fruit.variety]]\nname = \"red\"\ntail = 2\n[[fruit]]\ntail = 3\nname = \"banana\"\n")
	values, err := DecodeOrdered(source)
	if err != nil {
		t.Fatal(err)
	}
	fruits, _ := values.Get("fruit")
	rows, ok := fruits.([]any)
	if !ok || len(rows) != 2 {
		t.Fatalf("fruit = %#v", fruits)
	}
	banana := rows[1].(*jsonx.OrderedMap)
	if got := banana.Keys(); !reflect.DeepEqual(got, []string{"tail", "name"}) {
		t.Fatalf("second fruit field order = %v, want [tail name]", got)
	}
	varieties, _ := rows[0].(*jsonx.OrderedMap).Get("variety")
	children, ok := varieties.([]any)
	if !ok || len(children) != 2 {
		t.Fatalf("first fruit varieties = %#v", varieties)
	}
	for i, child := range children {
		if got := child.(*jsonx.OrderedMap).Keys(); !reflect.DeepEqual(got, []string{"name", "tail"}) {
			t.Errorf("variety %d field order = %v, want [name tail]", i, got)
		}
	}

	const singletonSource = "[[fruit]]\nname = \"apple\"\n" +
		"[[fruit.variety]]\nz = 1\na = 2\n" +
		"[[fruit.variety]]\nz = 3\na = 4\n" +
		"[[fruit.variety]]\nz = 5\na = 6\n"
	singleton, err := DecodeOrdered([]byte(singletonSource))
	if err != nil {
		t.Fatal(err)
	}
	singletonFruits, _ := singleton.Get("fruit")
	singletonRows, ok := singletonFruits.([]any)
	if !ok || len(singletonRows) != 1 {
		t.Fatalf("singleton fruit = %#v, want one parent", singletonFruits)
	}
	parent := singletonRows[0].(*jsonx.OrderedMap)
	if got, _ := parent.Get("name"); got != "apple" {
		t.Fatalf("singleton fruit name = %#v, want apple", got)
	}
	singletonVarieties, _ := parent.Get("variety")
	singletonChildren, ok := singletonVarieties.([]any)
	if !ok || len(singletonChildren) != 3 {
		t.Fatalf("singleton varieties = %#v, want three rows", singletonVarieties)
	}
	for i, child := range singletonChildren {
		row := child.(*jsonx.OrderedMap)
		if got := row.Keys(); !reflect.DeepEqual(got, []string{"z", "a"}) {
			t.Errorf("singleton variety %d field order = %v, want [z a]", i, got)
		}
		for key, want := range map[string]int64{"z": int64(2*i + 1), "a": int64(2*i + 2)} {
			if got, _ := row.Get(key); got != want {
				t.Errorf("singleton variety %d.%s = %#v, want %d", i, key, got, want)
			}
		}
	}
}

func TestDecodeOrderedPreservesRepeatedParentAndThreeChildHeaderSegments(t *testing.T) {
	source := `[[fruit]]
name = "apple"
[[fruit.variety]]
z = 1
a = 2
[[fruit.variety]]
m = 3
b = 4
[[fruit.variety]]
y = 5
x = 6
[[fruit]]
name = "banana"
[[fruit.variety]]
a = 11
z = 12
[[fruit.variety]]
b = 13
m = 14
[[fruit.variety]]
x = 15
y = 16
`
	ordered, err := DecodeOrdered([]byte(source))
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := Decode([]byte(source))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := normalizeDecodedValue(ordered), normalizeDecodedValue(decoded); !reflect.DeepEqual(got, want) {
		t.Fatalf("ordered semantic value = %#v, Decode = %#v", got, want)
	}
	parents, _ := ordered.Get("fruit")
	rows := parents.([]any)
	wantNames := []string{"apple", "banana"}
	wantKeys := [][][]string{{{"z", "a"}, {"m", "b"}, {"y", "x"}}, {{"a", "z"}, {"b", "m"}, {"x", "y"}}}
	wantValues := [][][]int64{{{1, 2}, {3, 4}, {5, 6}}, {{11, 12}, {13, 14}, {15, 16}}}
	if len(rows) != len(wantNames) {
		t.Fatalf("fruit rows = %d, want %d", len(rows), len(wantNames))
	}
	for i, row := range rows {
		parent := row.(*jsonx.OrderedMap)
		name, _ := parent.Get("name")
		if name != wantNames[i] {
			t.Errorf("fruit[%d].name = %#v, want %q", i, name, wantNames[i])
		}
		childrenValue, _ := parent.Get("variety")
		children := childrenValue.([]any)
		if len(children) != 3 {
			t.Fatalf("fruit[%d] varieties = %d, want 3", i, len(children))
		}
		for j, child := range children {
			childMap := child.(*jsonx.OrderedMap)
			if got := childMap.Keys(); !reflect.DeepEqual(got, wantKeys[i][j]) {
				t.Errorf("fruit[%d].variety[%d] keys = %v, want %v", i, j, got, wantKeys[i][j])
			}
			for k, key := range wantKeys[i][j] {
				if got, _ := childMap.Get(key); got != wantValues[i][j][k] {
					t.Errorf("fruit[%d].variety[%d].%s = %#v, want %d", i, j, key, got, wantValues[i][j][k])
				}
			}
		}
	}
}

func TestDecodeOrderedNULKeyDoesNotCollideWithDottedPath(t *testing.T) {
	values, err := DecodeOrdered([]byte("\"a\\u0000b\" = 1\na.b = 2\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := values.Get("a\x00b"); !ok || got != int64(1) {
		t.Fatalf("quoted NUL key = %#v, %v; want 1", got, ok)
	}
	a, ok := values.Get("a")
	if !ok {
		t.Fatal("dotted path root a was lost")
	}
	b, ok := a.(*jsonx.OrderedMap).Get("b")
	if !ok || b != int64(2) {
		t.Fatalf("dotted value a.b = %#v, %v; want 2", b, ok)
	}
}
func TestDecodeBasic(t *testing.T) {
	m, err := Decode([]byte("[a]\nb = 1\nc = \"x\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	a, ok := m["a"].(map[string]any)
	if !ok {
		t.Fatalf("a is %T, want table", m["a"])
	}
	if a["c"] != "x" {
		t.Errorf("a.c = %v, want x", a["c"])
	}
}

func TestVenvValueStrForm(t *testing.T) {
	m, _ := Decode([]byte("[env._.python]\nvenv = \".venv\"\n"))
	v, ok := VenvValue(m)
	if !ok || v != ".venv" {
		t.Errorf("VenvValue = %v, %v; want .venv, true", v, ok)
	}
}

func TestVenvValueTableForm(t *testing.T) {
	m, _ := Decode([]byte("[env._.python.venv]\ncreate = true\npath = \"myenv\"\n"))
	v, ok := VenvValue(m)
	if !ok {
		t.Fatal("VenvValue not found")
	}
	tbl, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("venv is %T, want table", v)
	}
	if tbl["create"] != true || tbl["path"] != "myenv" {
		t.Errorf("venv table = %v", tbl)
	}
}

func TestVenvValueAbsent(t *testing.T) {
	m, _ := Decode([]byte("[tools]\nnode = \"22\"\n"))
	if _, ok := VenvValue(m); ok {
		t.Error("VenvValue should be absent")
	}
}

// TestMiseVenvPath exercises the full priority-ordered discovery + the
// str/table/create/path resolution, matching entrypoint/shell.py.
func TestMiseVenvPath(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) string {
		p := filepath.Join(dir, name)
		os.WriteFile(p, []byte(content), 0o644)
		return p
	}

	// String form -> path directly.
	strFile := write("str.toml", "[env._.python]\nvenv = \".venv\"\n")
	if path, ok := MiseVenvPath([]string{strFile}); !ok || path != ".venv" {
		t.Errorf("str MiseVenvPath = %q, %v; want .venv, true", path, ok)
	}

	// Table with create=true -> path.
	tblFile := write("tbl.toml", "[env._.python.venv]\ncreate = true\npath = \"myenv\"\n")
	if path, ok := MiseVenvPath([]string{tblFile}); !ok || path != "myenv" {
		t.Errorf("table MiseVenvPath = %q, %v; want myenv, true", path, ok)
	}

	// Table with create=false -> no venv.
	noCreate := write("nc.toml", "[env._.python.venv]\ncreate = false\n")
	if _, ok := MiseVenvPath([]string{noCreate}); ok {
		t.Error("create=false should yield no venv")
	}

	// Table with create=true, no path -> default .venv.
	defPath := write("def.toml", "[env._.python.venv]\ncreate = true\n")
	if path, ok := MiseVenvPath([]string{defPath}); !ok || path != ".venv" {
		t.Errorf("default path = %q, %v; want .venv, true", path, ok)
	}

	// Priority: first file WITH a value wins. A file without a venv is skipped.
	noVenv := write("nov.toml", "[tools]\nnode = \"22\"\n")
	if path, ok := MiseVenvPath([]string{noVenv, strFile}); !ok || path != ".venv" {
		t.Errorf("priority skip: %q, %v; want .venv from 2nd file", path, ok)
	}

	// No files have a venv -> not found.
	if _, ok := MiseVenvPath([]string{noVenv}); ok {
		t.Error("no venv anywhere should be not-found")
	}
}
