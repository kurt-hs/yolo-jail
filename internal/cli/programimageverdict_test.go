package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/capture"
)

// TestImageProvidedVerdictAsksTheProducerOncePerImage runs the real launch preparation
// (prepareOrdinaryForLaunch) through the fixture's sealed producer, with a stand-in image /bin
// that provides the declared program. The first launch on an image asks the producer; a second
// launch on the same image identity starts no producer at all; a changed identity asks again,
// once.
func TestImageProvidedVerdictAsksTheProducerOncePerImage(t *testing.T) {
	f := newSelectedNPMFixture(t)
	bins := filepath.Join(f.root, "image-bins")
	if err := os.MkdirAll(bins, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(bins, f.in.Bin), "#!/bin/sh\n")
	if err := os.Chmod(filepath.Join(bins, f.in.Bin), 0o755); err != nil {
		t.Fatal(err)
	}
	idA, idB := "sha256:"+strings.Repeat("a", 64), "sha256:"+strings.Repeat("b", 64)
	current := idA
	writeFile(t, npmCaptureImagePath, current+"\n")
	req := selectedNPMUser(t, f, false)
	stubPreparationWorkerStart(t)
	selectedNPMSharedWorker(t)
	asked := 0
	req.ImageIdentity = func() (string, bool) { asked++; return current, true }
	launch := func(wantResolves int32) string {
		t.Helper()
		var out bytes.Buffer
		deliveries, err := prepareOrdinaryForLaunch(req, &out)
		if err != nil {
			t.Fatalf("launch preparation: %v\n%s", err, out.String())
		}
		if _, ok := deliveries[f.in.Bin]; ok {
			t.Fatalf("a program the image provides was delivered: %+v", deliveries)
		}
		if got := f.resolves.Load(); got != wantResolves {
			t.Fatalf("producer jails started = %d, want %d\n%s", got, wantResolves, out.String())
		}
		if !strings.Contains(out.String(), "already provides") {
			t.Fatalf("the launch did not say the image provides %s:\n%s", f.in.Bin, out.String())
		}
		return out.String()
	}

	launch(1)
	if path, ok := imageProvidesVerdictFor(&capture.Store{Dir: f.store.Dir}, idA, f.in.Bin); !ok || path != filepath.Join(bins, f.in.Bin) {
		t.Fatalf("the producer's verdict was not kept for image %s: %q %v", idA, path, ok)
	}
	launch(1) // same image identity: no producer
	if asked == 0 {
		t.Fatal("the launch never asked its image identity")
	}

	current = idB
	writeFile(t, npmCaptureImagePath, current+"\n")
	launch(2) // a new image identity asks again
	launch(2) // and only once
}

// TestImageProvidedVerdictNeedsAKnownIdentity: a launch that cannot name its image keeps every
// program, and a producer whose image names no valid identity keeps no verdict.
func TestImageProvidedVerdictNeedsAKnownIdentity(t *testing.T) {
	store := &capture.Store{Dir: t.TempDir()}
	id := "sha256:" + strings.Repeat("c", 64)
	if err := recordImageProvides(store, "private-synthetic-runtime-v1", "fzf", "/bin/fzf"); err == nil {
		t.Error("a verdict was kept under an image that names no valid identity")
	}
	if err := recordImageProvides(store, id, "fzf", "/bin/fzf"); err != nil {
		t.Fatal(err)
	}
	vector := []npmCaptureInput{{Owner: "o", Bin: "fzf", Package: "fzf"}}
	var out bytes.Buffer
	if got := skipImageProvidedPrograms(vector, func() (string, bool) { return "", false }, store, &out); len(got) != 1 {
		t.Errorf("an unknown launch identity dropped a program: %+v", got)
	}
	if got := skipImageProvidedPrograms(vector, nil, store, &out); len(got) != 1 {
		t.Errorf("no identity source dropped a program: %+v", got)
	}
	if got := skipImageProvidedPrograms(vector, func() (string, bool) { return id, true }, store, &out); len(got) != 0 {
		t.Errorf("a kept verdict for this image did not drop the program: %+v", got)
	}
	if got := skipImageProvidedPrograms(vector, func() (string, bool) { return "sha256:" + strings.Repeat("d", 64), true }, store, &out); len(got) != 1 {
		t.Errorf("another image's verdict dropped the program: %+v", got)
	}
}
