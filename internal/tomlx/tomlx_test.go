package tomlx

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
)

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
