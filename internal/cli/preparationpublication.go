package cli

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// Control credentials and owner records must never live in a shared directory.
func ensurePreparationRoot(root string) error {
	if err := os.MkdirAll(root, 0700); err != nil {
		return err
	}
	info, err := os.Lstat(root)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return errors.New("preparation state directory is not private; select a private scheduler directory before starting")
	}
	return nil
}

// Seams pin durable publication ordering and failure retention.
var preparationFileSync = func(f *os.File) error { return f.Sync() }
var preparationRename = os.Rename

func writePreparationJSON(path string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".preparation-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(append(data, '\n')); err != nil {
		f.Close()
		return err
	}
	if err = preparationFileSync(f); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = preparationRename(f.Name(), path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	// A sync error retains the published evidence but never reports durable success.
	return preparationFileSync(dir)
}
