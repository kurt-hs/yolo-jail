package brokercore

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

const lifetimeLeaseVersion = 1

// Known metadata proves ownership; successful exclusive flock proves inactivity.
// Neither a recorded PID nor file age is evidence for collecting a lease.
type lifetimeLease struct {
	Version  int    `json:"version"`
	Source   string `json:"source"`
	Start    string `json:"start"`
	LaunchID string `json:"launch_id"`
}

// ValidLifetimeToken says s is a broker start or launch id a lease file name may carry.
func ValidLifetimeToken(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
			return false
		}
	}
	return true
}

// collectInertLeases is called by the next real broker binding under the shared
// source transaction. Creation/lease acquisition/binding use that same lock, so
// a just-created file cannot be mistaken for an abandoned lifetime.
func collectInertLeases(source, dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		start, ok := strings.CutPrefix(entry.Name(), ".lifetime-")
		if !ok || !ValidLifetimeToken(start) {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
		if err != nil {
			continue
		}
		err = collectInertLease(source, f, path, start)
		f.Close()
		if err != nil {
			return err
		}
	}
	return nil
}
func collectInertLease(source string, f *os.File, path, start string) error {
	fi, err := f.Stat()
	if err != nil || !fi.Mode().IsRegular() {
		return nil
	}
	if err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return nil
	} // live OR unknown stays
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	data, err := io.ReadAll(io.LimitReader(f, 4097))
	if err != nil || len(data) > 4096 {
		return nil
	}
	var lease lifetimeLease
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err = dec.Decode(&lease); err != nil {
		return nil
	}
	var trailing any
	if dec.Decode(&trailing) != io.EOF {
		return nil
	}
	if lease.Version != lifetimeLeaseVersion || lease.Source != source || lease.Start != start || !ValidLifetimeToken(lease.LaunchID) {
		return nil
	}
	current, err := os.Lstat(path)
	if err != nil || !os.SameFile(fi, current) {
		return nil
	}
	return os.Remove(path)
}
