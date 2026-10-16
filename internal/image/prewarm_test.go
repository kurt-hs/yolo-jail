package image

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/shquote"
	"github.com/mschulkind-oss/yolo-jail/internal/testsupport"
)

// PREWARM IS THE BUILD'S FIRST HALF AND NOTHING ELSE (prewarm.go): with the runtime holding this
// tree's stock image it builds nothing; without it, it builds the derivation, and in both it prints
// nothing and loads, tags and copies nothing, which the image step's report is the one disclosure
// of. Red with Prewarm's build deleted, or its stock check.
func TestPrewarmBuildsTheDerivationAndNothingElse(t *testing.T) {
	withBuildDir(t)
	var out bytes.Buffer
	stockRef := StockImageRef("podman", testIdentity)

	loaded := newFakeRuntime(stockRef)
	o := stockOpts(t, loaded, &out, testIdentity)
	o.BuildStorePath = func(string, []any, string) (string, []string) {
		t.Error("the prewarm built an image the runtime already holds")
		return "", nil
	}
	Prewarm(o)

	missing := newFakeRuntime(JailImage("podman"))
	o = stockOpts(t, missing, &out, testIdentity)
	built := 0
	o.BuildStorePath = func(string, []any, string) (string, []string) {
		built++
		return storeManifest(t, "prewarmed"), nil
	}
	Prewarm(o)
	if built != 1 {
		t.Errorf("the prewarm built %d times, want the derivation once", built)
	}
	if out.Len() != 0 || missing.loads != 0 || len(missing.copiedDests) != 0 {
		t.Errorf("the prewarm printed %q and delivered %d loads, %d copies; it must do neither", out.String(),
			missing.loads, len(missing.copiedDests))
	}
}

// A PREWARM WHOSE CONTEXT HAS ENDED BUILDS NOTHING (prewarm.go): the launch returned while its eval
// ran, and a build started then would outlive it. Red with Prewarm's context check deleted.
func TestAPrewarmWhoseContextEndedBuildsNothing(t *testing.T) {
	withBuildDir(t)
	var out bytes.Buffer
	o := stockOpts(t, newFakeRuntime(JailImage("podman")), &out, testIdentity)
	ctx, cancel := context.WithCancel(context.Background())
	o.Context = ctx
	o.EvalIdentity = func(string) (string, bool) {
		cancel() // the launch returns while the eval runs
		return "", false
	}
	o.BuildStorePath = func(string, []any, string) (string, []string) {
		t.Error("the prewarm started a build after its launch had returned")
		return "", nil
	}
	Prewarm(o)
}

// AN ENDED CONTEXT INTERRUPTS THE IDENTITY EVAL'S NIX (EvalImageIdentityContext): as a Ctrl-C would,
// and the eval answers ok=false once it has exited. Red with the eval's interruptOnCancel deleted,
// which leaves exec's default kill, which the stand-in's trap never sees.
func TestAnEndedContextInterruptsTheIdentityEvalsNix(t *testing.T) {
	s := freshNixChildren(t)
	started, interrupted := startMark(t), filepath.Join(t.TempDir(), "interrupted")
	standInNix(t, testsupport.UntilInterrupted(": >"+shquote.Quote(interrupted), started))
	ctx, cancel := context.WithCancel(context.Background())
	type evaluated struct {
		id string
		ok bool
	}
	done := make(chan evaluated, 1)
	go func() {
		id, ok := EvalImageIdentityContext(ctx, t.TempDir())
		done <- evaluated{id, ok}
	}()
	awaitTracked(t, s, 1)
	awaitStarted(t, started)
	cancel()
	select {
	case e := <-done:
		if e.ok {
			t.Errorf("an interrupted eval answered identity %q", e.id)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the eval did not return after its context ended")
	}
	if _, err := os.Stat(interrupted); err != nil {
		t.Errorf("the eval's nix was not interrupted: %v", err)
	}
	awaitTracked(t, s, 0)
}

// AN ENDED CONTEXT INTERRUPTS THE BUILD'S NIX (runNixBuildContext): as a Ctrl-C would, and the
// build returns failed once it has exited. Red with interruptOnCancel's interrupt replaced by exec's
// default kill, which the stand-in's trap never sees.
func TestAnEndedContextInterruptsTheBuildsNix(t *testing.T) {
	s := freshNixChildren(t)
	started := startMark(t)
	ctx, cancel := context.WithCancel(context.Background())
	type built struct {
		path string
		tail []string
	}
	done := make(chan built, 1)
	go func() {
		p, tail := runNixBuildContext(ctx, []string{"sh", "-c", trapsInterrupt(started)}, t.TempDir(), os.Environ(),
			filepath.Join(t.TempDir(), "out"), io.Discard)
		done <- built{p, tail}
	}()
	awaitTracked(t, s, 1)
	awaitStarted(t, started)
	cancel()
	select {
	case b := <-done:
		if b.path != "" {
			t.Errorf("an interrupted build returned store path %q", b.path)
		}
		if !strings.Contains(strings.Join(b.tail, "\n"), "interrupted") {
			t.Errorf("the build's nix was not interrupted; its stderr: %q", b.tail)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the build did not return after its context ended")
	}
	awaitTracked(t, s, 0)
}
