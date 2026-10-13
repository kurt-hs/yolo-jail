package cli

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestPreparationPublicationSyncsFileBeforeRenameThenDirectory(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "state.json")
	previousSync, previousRename := preparationFileSync, preparationRename
	defer func() { preparationFileSync = previousSync; preparationRename = previousRename }()
	var order []string
	preparationFileSync = func(f *os.File) error {
		info, err := f.Stat()
		if err != nil {
			return err
		}
		if info.IsDir() {
			order = append(order, "directory")
		} else {
			order = append(order, "file")
		}
		return f.Sync()
	}
	preparationRename = func(from, to string) error {
		// The file sync must finish before its name is published.
		if !reflect.DeepEqual(order, []string{"file"}) {
			t.Fatalf("rename before file sync: %v", order)
		}
		order = append(order, "rename")
		return os.Rename(from, to)
	}
	if err := writePreparationJSON(path, "new"); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(order, []string{"file", "rename", "directory"}) {
		t.Fatal(order)
	}
}

func TestPreparationPublicationFailuresRetainEvidence(t *testing.T) {
	for _, failure := range []string{"file", "rename", "directory"} {
		t.Run(failure, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "state.json")
			if err := os.WriteFile(path, []byte("old"), 0600); err != nil {
				t.Fatal(err)
			}
			previousSync, previousRename := preparationFileSync, preparationRename
			defer func() { preparationFileSync = previousSync; preparationRename = previousRename }()
			failed := errors.New("injected publication failure")
			preparationFileSync = func(f *os.File) error {
				info, err := f.Stat()
				if err != nil {
					return err
				}
				if (failure == "directory") == info.IsDir() && failure != "rename" {
					return failed
				}
				return f.Sync()
			}
			preparationRename = func(from, to string) error {
				if failure == "rename" {
					return failed
				}
				return os.Rename(from, to)
			}
			if err := writePreparationJSON(path, "new"); !errors.Is(err, failed) {
				t.Fatalf("%v", err)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			want := "old"
			if failure == "directory" {
				want = "\"new\"\n"
			}
			if string(data) != want {
				t.Fatalf("evidence %q want %q", data, want)
			}
			temps, err := filepath.Glob(filepath.Join(root, ".preparation-*"))
			if err != nil || len(temps) != 0 {
				t.Fatalf("temporary files %v %v", temps, err)
			}
		})
	}
}
