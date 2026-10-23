//go:build !linux && !darwin

package hostcas

// DefaultProbe off Linux and darwin reports "nothing here", which is inert: no
// backend yolo runs on such a host can alias anything, so no decision reaches
// this. It exists so the package builds for `go build ./...` on any GOOS, not as a
// second code path — see storepackages.go's own non-Linux stubs for the same
// shape. Linux and darwin share probe_unix.go.
func DefaultProbe(string) Presence { return Presence{} }
