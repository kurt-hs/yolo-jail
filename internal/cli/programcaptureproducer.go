package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/mschulkind-oss/yolo-jail/internal/capture"
	"github.com/mschulkind-oss/yolo-jail/internal/runtime"
)

const programCaptureJailVerb = "program-capture-jail"

type ordinaryCaptureRuntimeKey struct{}

func withOrdinaryCaptureRuntime(ctx context.Context, rt string) context.Context {
	return context.WithValue(ctx, ordinaryCaptureRuntimeKey{}, rt)
}

// programCaptureChild uses the existing bounded owned-child lifecycle, not a second builder.
var programCaptureChild = func(ctx context.Context, workspace string, argv []string, w io.Writer) error {
	child := []string{"internal", programCaptureJailVerb, "--workspace=" + workspace, forkBuildJailStreamsFlag}
	if rt, ok := ctx.Value(ordinaryCaptureRuntimeKey{}).(string); ok && rt != "" {
		child = append(child, "--runtime="+rt)
	}
	child = append(child, "--")
	child = append(child, argv...)
	rc, bound := runCaptureChild(ctx, forkBuildWaitBound, workspace, "ordinary npm", child, captureStreams{out: w, errw: w, jailOut: w, jailErr: w}, programCaptureEnvironment(os.Environ()))
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if rc != 0 || bound {
		return fmt.Errorf("selected sealed npm producer failed (status %d) — retry preparation", rc)
	}
	return nil
}

func programCaptureEnvironment(environ []string) []string {
	allowed := map[string]bool{"HOME": true, "PATH": true, "TMPDIR": true, "LANG": true, "NO_COLOR": true, "USER": true, "LOGNAME": true, "XDG_CONFIG_HOME": true, "XDG_DATA_HOME": true, "XDG_CACHE_HOME": true, "XDG_RUNTIME_DIR": true, "YOLO_REPO_ROOT": true, "YOLO_RUNTIME": true}
	var out []string
	for _, item := range environ {
		name, value, ok := strings.Cut(item, "=")
		if ok && (allowed[name] || name == "NIX_REMOTE" && value == "daemon") {
			out = append(out, item)
		}
	}
	return out
}

func runProgramCaptureJail(args []string, out, w io.Writer) int {
	workspace, rt := "", ""
	streams := false
	var argv []string
	for i, a := range args {
		if a == "--" {
			argv = args[i+1:]
			break
		}
		switch {
		case strings.HasPrefix(a, "--workspace="):
			workspace = strings.TrimPrefix(a, "--workspace=")
		case strings.HasPrefix(a, "--runtime="):
			rt = strings.TrimPrefix(a, "--runtime=")
		case a == forkBuildJailStreamsFlag:
			streams = true
		default:
			fmt.Fprintf(w, "program-capture-jail: unexpected argument %q — retry selected preparation\n", a)
			return 2
		}
	}
	if workspace == "" || len(argv) == 0 {
		fmt.Fprintln(w, "usage: yolo internal program-capture-jail --workspace=DIR [--jail-streams] -- COMMAND")
		return 2
	}
	s := captureStreams{out: out, errw: w}
	if streams {
		jo, je, jr, ok := forkBuildJailStreams()
		if !ok {
			fmt.Fprintln(w, "program capture lost its owned streams — retry preparation")
			return 1
		}
		defer jo.Close()
		defer je.Close()
		defer jr.Close()
		s.jailOut, s.jailErr = jo, je
		s.jailReady = func() { _, _ = jr.Write([]byte{'R'}); _ = jr.Close() }
	}
	// An empty, NON-NIL selection is essential: even the owner's pack can contain
	// literal provider credentials, hooks or unrelated installation declarations.
	return runCaptureJail(workspace, "ordinary-npm", argv, &captureSeal{only: []string{}}, s, false, captureAct{runtime: rt})
}

func stageProgramCapture(store *capture.Store, id string) (string, error) {
	staging := store.StagingDir(id)
	cname := runtime.FromWorkspace(staging)
	if captureWorkspaceArtifactsExist(staging, cname) {
		release, safe := forkBuildWorkspaceReclaimable(staging, cname)
		if !safe {
			return "", forkBuildRetained{msg: "selected npm runtime is not known gone — stop its owned jail, then retry preparation"}
		}
		cleanupForkBuildWorkspace(staging, cname)
		release()
	}
	return store.Stage(id)
}

func programCaptureRuntimeGone(workspace string) (func(), error) {
	if !forkBuildRunReturned(workspace) {
		return nil, forkBuildRetained{msg: "selected npm runtime has no return witness — stop its owned jail before retrying"}
	}
	release, safe := forkBuildWorkspaceReclaimable(workspace, runtime.FromWorkspace(workspace))
	if !safe {
		return nil, forkBuildRetained{msg: "selected npm runtime teardown is unknown — wait for its owned jail to stop before retrying"}
	}
	return release, nil
}

func resolveSelectedNPMCapture(ctx context.Context, store *capture.Store, in npmCaptureInput, w io.Writer) (*npmCaptureResolution, error) {
	staging, err := stageProgramCapture(store, "npm-resolve-"+in.declarationIdentity())
	if err != nil {
		return nil, err
	}
	b, _ := json.Marshal(in)
	if err = os.WriteFile(filepath.Join(staging, "input.json"), b, 0644); err != nil {
		return nil, err
	}
	argv := []string{"yolo", "internal", "program-npm", "resolve", path.Join(containerWorkspace, "input.json"), path.Join(containerWorkspace, "resolution.json")}
	if err = programCaptureChild(ctx, staging, argv, w); err != nil {
		return nil, err
	}
	release, err := programCaptureRuntimeGone(staging)
	if err != nil {
		return nil, err
	}
	defer release()
	defer cleanupForkBuildWorkspace(staging, runtime.FromWorkspace(staging))
	raw, err := os.ReadFile(filepath.Join(staging, "resolution.json"))
	if err != nil {
		return nil, err
	}
	var r npmCaptureResolution
	if err = json.Unmarshal(raw, &r); err != nil {
		return nil, err
	}
	if r.Input != in {
		return nil, fmt.Errorf("selected npm resolver changed installation authority — retry preparation")
	}
	if _, err = r.inputs(); err != nil {
		return nil, err
	}
	return &r, nil
}

func produceSelectedNPMCapture(ctx context.Context, staging string, r npmCaptureResolution, w io.Writer) error {
	inputs, err := r.inputs()
	if err != nil {
		return err
	}
	identity, err := inputs.Identity()
	if err != nil {
		return err
	}
	b, _ := json.Marshal(r)
	if err = os.WriteFile(filepath.Join(staging, "resolution.json"), b, 0644); err != nil {
		return err
	}
	argv := []string{"yolo", "internal", "capture-run", "--out=" + path.Join(containerWorkspace, captureOutLeaf), "--scan-content-refs"}
	for _, artifact := range npmCaptureArtifacts(r.Input, identity) {
		argv = append(argv, "--artifact="+artifact)
	}
	argv = append(argv, "--", "yolo", "internal", "program-npm", "install", path.Join(containerWorkspace, "resolution.json"), identity)
	return programCaptureChild(ctx, staging, argv, w)
}

// prepareSelectedNPMCapture is the resolved ordinary producer's admission callsite.
// The original runtime witness is checked by the established admission adapter.
func prepareSelectedNPMCapture(ctx context.Context, store *capture.Store, r npmCaptureResolution, w io.Writer, guard func(func() error) error) (*capture.Entry, error) {
	inputs, err := r.inputs()
	if err != nil {
		return nil, err
	}
	identity, err := inputs.Identity()
	if err != nil {
		return nil, err
	}
	return prepareProgramCapture(ctx, store, inputs, npmCaptureArtifacts(r.Input, identity), func(ctx context.Context, staging string) error { return produceSelectedNPMCapture(ctx, staging, r, w) }, guard)
}
