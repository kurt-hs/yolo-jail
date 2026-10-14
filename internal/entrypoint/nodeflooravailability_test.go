package entrypoint

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/shquote"
)

func TestDescribeCaptureAvailableNodesValidatesActualVersionProbe(t *testing.T) {
	for _, version := range []string{"v24.21.0", "v24.21.0\nUNSAFE", "v24.21.0\x1b]52;c;UNSAFE\a", "UNSAFE", "+24.21.0"} {
		t.Run(version, func(t *testing.T) {
			fakeMiseStore(t)
			t.Setenv(DarwinLoginPathEnv, "")
			node := filepath.Join(t.TempDir(), "node")
			if err := os.WriteFile(node, []byte("#!/bin/sh\nprintf '%s\\n' "+shquote.Quote(version)+"\n"), 0755); err != nil {
				t.Fatal(err)
			}
			old := imageNodePath
			t.Cleanup(func() { imageNodePath = old })
			imageNodePath = node
			got, err := DescribeCaptureAvailableNodes()
			if version == "v24.21.0" {
				if err != nil || got != "24.21.0 at /bin/node" {
					t.Fatalf("actual numeric probe lost: %q %v", got, err)
				}
			} else if err == nil || got != "" {
				t.Fatalf("unsafe actual probe accepted: %q %v", got, err)
			}
			if strings.Contains(got, "UNSAFE") {
				t.Fatal("probe bytes disclosed")
			}
		})
	}
}

func TestDescribeCaptureAvailableNodesUsesFixedMiseLabels(t *testing.T) {
	stubImageNode(t, "")
	fakeMiseStore(t, "22.19.0", "24.21.0")
	t.Setenv(DarwinLoginPathEnv, "")
	got, err := DescribeCaptureAvailableNodes()
	if err != nil || got != "24.21.0 at mise node store; 22.19.0 at mise node store" {
		t.Fatalf("wrong observed version/labels: %q %v", got, err)
	}
}

func TestDescribeCaptureAvailableNodesRejectsUnconfinedSource(t *testing.T) {
	stubImageNode(t, "")
	fakeMiseStore(t)
	fakePackageFloor(t, "24.21.0")
	if got, err := DescribeCaptureAvailableNodes(); err == nil || got != "" {
		t.Fatalf("unconfined source accepted: %q %v", got, err)
	}
}
