package run

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/entrypoint"
	"github.com/mschulkind-oss/yolo-jail/internal/image"
	"github.com/mschulkind-oss/yolo-jail/internal/provision"
	"github.com/mschulkind-oss/yolo-jail/internal/shquote"
)

func TestRunOrdinaryNodeFloorRefusalStatusBeforeTarget(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		status int
	}{
		{"unavailable", fmt.Errorf("preparation: %w", &NodeFloorUnavailable{Owner: "floor-fixture", Bin: "probe", Floor: "99", Available: "24.21.0 at /bin/node"}), provision.RefusedStatus},
		{"install failure", errors.New("transient install failure"), 1},
		{"cancel", context.Canceled, 1},
		{"timeout", context.DeadlineExceeded, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bin, targetMarker := t.TempDir(), filepath.Join(t.TempDir(), "target-ran")
			if err := os.WriteFile(filepath.Join(bin, "podman"), []byte("#!/bin/sh\nif [ \"$1\" = run ]; then touch "+shquote.Quote(targetMarker)+"; fi\nexit 3\n"), 0755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", bin+":/bin:/usr/bin")
			var out, diag bytes.Buffer
			o := dispatchOptions(t, t.TempDir(), "podman", &out, &diag, nil)
			repo, _ := o.RepoRoot()
			for _, name := range []string{"flake.nix", "flake.lock"} {
				if err := os.WriteFile(filepath.Join(repo.Root, name), []byte("{}"), 0644); err != nil {
					t.Fatal(err)
				}
			}
			o.PathExists = func(p string) bool { return p == filepath.Join(prebuiltBinDir(repo.Root), "yolo-entrypoint") }
			targetRan := false
			o.Exec = func(argv []string, _ string, _ []string, _ time.Duration) ExecResult {
				for _, arg := range argv {
					if arg == "run" {
						targetRan = true
					}
				}
				return ExecResult{Ran: true, RC: 0}
			}
			o.autoLoad = func(image.AutoLoadOptions) image.LoadResult { return image.LoadResult{OK: true, Ref: goldenImageRef} }
			store := t.TempDir()
			o.CapturesDir = func() string { return store }
			calls := 0
			o.PreparePrograms = func(ProgramCaptureRequest) (map[string]entrypoint.ProgramCaptureDelivery, error) {
				calls++
				return nil, tc.err
			}
			status := Run(*o)
			_, markerErr := os.Lstat(targetMarker)
			if !os.IsNotExist(markerErr) {
				targetRan = true
			}
			if status != tc.status || calls != 1 || targetRan || !os.IsNotExist(markerErr) {
				t.Fatalf("status=%d want %d caller=%d target=%v\n%s", status, tc.status, calls, targetRan, diag.String())
			}
			if tc.status == 78 {
				for _, want := range []string{"REFUSING to start this jail", "pack floor-fixture", "program probe", "Node >=99", "Available: 24.21.0 at /bin/node"} {
					if !strings.Contains(diag.String(), want) {
						t.Fatalf("missing %q: %s", want, diag.String())
					}
				}
			} else if strings.Contains(diag.String(), "REFUSING") {
				t.Fatal("generic error classified unavailable")
			}
		})
	}
}
