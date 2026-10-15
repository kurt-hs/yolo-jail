package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/entrypoint"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
)

// TestHostOrdinarySelectionDoesNotReadTheHostsBinAsTheImages pins the CALL SITE: the host's
// ordinary preparation must not skip a declared program because the HOST's /bin has the name.
// The host's /bin says nothing about the image's. Selecting through the in-jail selection
// made the result depend on the machine: a CI runner with no fzf prepared a pack's
// `program fzf` that the image bakes, and refused the launch when the npm package had no fzf
// bin (TestPackProgramDoesNotShadowABakedBinary, integration). Developing inside a yolo jail,
// whose /bin is the image's, hid that. The image half of the check runs in the sealed producer
// (npmImageProvidedResolution).
func TestHostOrdinarySelectionDoesNotReadTheHostsBinAsTheImages(t *testing.T) {
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("needs a host /bin/sh to stand for a name the host provides")
	}
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "pack.json"),
		`{"name":"shadowy","contributes":[{"kind":"program","bin":"sh","via":"npm","package":"sh-pkg"}]}`)
	p, problems := packload.LoadDir(dir, "shadowy")
	if len(problems) != 0 || p == nil {
		t.Fatalf("fixture pack: %v", problems)
	}
	vector, err := selectedOrdinaryInputs(entrypoint.NewEnv(map[string]string{}), []*packload.Pack{p}, entrypoint.ProgramInstallOnly, "linux/amd64")
	if err != nil {
		t.Fatal(err)
	}
	if len(vector) != 1 || vector[0].Bin != "sh" {
		t.Fatalf("the host skipped preparing `sh` because the HOST's /bin has it: %+v", vector)
	}
}

// TestSealedProducerAnswersForAProgramTheImageProvides is the producer's half: inside the
// image, a declared program the image already provides is answered as Unsupported with the
// image-provides marker, which survives the text round trip through the shared-demand result
// and is told apart from an unsupported dependency graph.
func TestSealedProducerAnswersForAProgramTheImageProvides(t *testing.T) {
	in := npmCaptureInput{Owner: "fzf-dep", Bin: "fzf", Package: "fzf"}
	if r := npmImageProvidedResolution(in, func(string) string { return "" }); r != nil {
		t.Fatalf("a name the image does not provide must go on to npm resolution, got %+v", r)
	}
	r := npmImageProvidedResolution(in, func(bin string) string { return "/bin/" + bin })
	if r == nil || r.Input != in {
		t.Fatalf("a name the image provides must be answered without npm: %+v", r)
	}
	_, err := r.inputs()
	if !errors.Is(err, errNPMCaptureUnsupported) {
		t.Fatalf("the answer must take every requester's keep-the-jail's-decision path, got %v", err)
	}
	if !npmCaptureImageProvided(err) {
		t.Fatalf("the requester cannot recognize the image-provides answer: %v", err)
	}
	// waitOrdinaryPreparation and the timer rebuild the error from the result's text.
	if relayed := fmt.Errorf("%w: %s", errNPMCaptureUnsupported, err.Error()); !npmCaptureImageProvided(relayed) {
		t.Fatalf("the marker did not survive the shared-demand relay: %v", relayed)
	}
	graph := npmCaptureResolution{Input: in, Unsupported: "unresolved dependency"}
	if _, err := graph.inputs(); npmCaptureImageProvided(err) {
		t.Fatalf("an unsupported dependency graph was read as image-provided: %v", err)
	}
}

// TestProgramNPMResolveAsksTheImageFirst pins the producer's call site: `yolo internal
// program-npm resolve`, the command the sealed producer runs in the image, answers for a
// program the image provides before any npm work and exits 0 with the recorded answer.
func TestProgramNPMResolveAsksTheImageFirst(t *testing.T) {
	bins := t.TempDir()
	writeFile(t, filepath.Join(bins, "fzf"), "#!/bin/sh\n")
	if err := os.Chmod(filepath.Join(bins, "fzf"), 0o755); err != nil {
		t.Fatal(err)
	}
	defer entrypoint.OverrideImageProbeBase(bins)()
	t.Setenv(entrypoint.SealedBuildEnv, "1")
	t.Setenv("HOME", t.TempDir())
	work := t.TempDir()
	input := filepath.Join(work, "input.json")
	writeFile(t, input, `{"owner":"fzf-dep","bin":"fzf","package":"fzf"}`)
	out := filepath.Join(work, "resolution.json")
	var log strings.Builder
	if rc := runProgramNPM([]string{"resolve", input, out}, &log); rc != 0 {
		t.Fatalf("resolve rc=%d: %s", rc, log.String())
	}
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	var r npmCaptureResolution
	if err := json.Unmarshal(raw, &r); err != nil {
		t.Fatal(err)
	}
	if _, err := r.inputs(); !npmCaptureImageProvided(err) {
		t.Fatalf("the producer did not answer that the image provides fzf: %+v (%v)", r, err)
	}
}
