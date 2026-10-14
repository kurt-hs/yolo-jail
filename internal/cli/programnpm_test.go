package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/capture"
	"github.com/mschulkind-oss/yolo-jail/internal/cli/run"
	"github.com/mschulkind-oss/yolo-jail/internal/entrypoint"
	"github.com/mschulkind-oss/yolo-jail/internal/shquote"
)

type selectedNPMFixture struct {
	store              *capture.Store
	in                 npmCaptureInput
	root, tools, node  string
	resolves, installs atomic.Int32
}

func init() {
	if len(os.Args) > 5 && os.Args[1] == "npm-capture-runtime-test" && os.Args[3] == "internal" && os.Args[4] == programCaptureJailVerb {
		npmCaptureImagePath = filepath.Join(os.Args[2], "image")
		captureRunPipeline = func(o run.Options) int {
			// Actual seal caller and owned child; only the outer container is substituted.
			if !o.Sealed || o.OnlyPacks == nil || len(o.OnlyPacks) != 0 || !o.NeverAttach || !o.NoProgramReadiness || o.CapturesDir() != "" || o.OnRuntimeResolved == nil {
				fmt.Fprintln(os.Stderr, "synthetic runtime received unsealed capture options")
				return 1
			}
			if err := o.OnRuntimeResolved("podman"); err != nil {
				return 1
			}
			home, err := os.MkdirTemp("", "npm-private-runtime-")
			if err != nil {
				return 1
			}
			defer os.RemoveAll(home)
			previousHome, previousSeal := os.Getenv("HOME"), os.Getenv(entrypoint.SealedBuildEnv)
			_ = os.Setenv("HOME", home)
			_ = os.Setenv(entrypoint.SealedBuildEnv, "1")
			defer func() { _ = os.Setenv("HOME", previousHome); _ = os.Setenv(entrypoint.SealedBuildEnv, previousSeal) }()
			argv := append([]string(nil), o.Args[2:]...)
			for i, a := range argv {
				argv[i] = strings.ReplaceAll(a, "/workspace", o.Workspace)
			}
			return runInternal(argv)
		}
		os.Exit(runInternal(os.Args[4:]))
	}
	if len(os.Args) > 5 && os.Args[1] == "npm-capture-test-image" && os.Args[3] == "internal" &&
		(os.Args[4] == "program-npm" || os.Args[4] == "capture-materialize" || os.Args[4] == "log-append") {
		npmCaptureImagePath = os.Args[2]
		os.Exit(runInternal(os.Args[4:]))
	}
}

func newSelectedNPMFixture(t *testing.T) *selectedNPMFixture {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("Linux artifact ownership")
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("local Node runtime fixture unavailable")
	}
	node, err = filepath.EvalSymlinks(node)
	if err != nil {
		t.Fatal(err)
	}
	f := &selectedNPMFixture{store: &capture.Store{Dir: t.TempDir()}, in: npmCaptureInput{Owner: "fixture", Bin: "probe", Package: "probe-pkg"}, root: t.TempDir(), tools: t.TempDir(), node: node}
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PATH", f.tools+":"+os.Getenv("PATH"))
	if err := os.Symlink(f.node, filepath.Join(f.tools, "node")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NPM_TOKEN", "must-not-cross")
	t.Setenv("SYNTHETIC_PROVIDER_KEY", "must-not-cross")
	oldImage := npmCaptureImagePath
	npmCaptureImagePath = filepath.Join(f.root, "image")
	t.Cleanup(func() { npmCaptureImagePath = oldImage })
	if err = os.WriteFile(npmCaptureImagePath, []byte("private-synthetic-runtime-v1"), 0644); err != nil {
		t.Fatal(err)
	}
	f.version(t, "1.0.0")
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	shim := "#!/bin/sh\nexec " + shquote.Quote(self) + " npm-capture-test-image " + shquote.Quote(npmCaptureImagePath) + " \"$@\"\n"
	if err := os.WriteFile(filepath.Join(f.tools, "yolo"), []byte(shim), 0o755); err != nil {
		t.Fatal(err)
	}
	script := `#!/bin/bash
set -eu
ROOT=` + shquote.Quote(f.root) + `
if [ "$1" = --version ]; then echo 10.0.0; exit 0; fi
phase=$1; shift
if [ "$phase" = view ]; then
 printf 'legacy-view\n' >> "$ROOT/counts"
 [ ! -f "$ROOT/legacy-fail" ] || exit 39
 cat "$ROOT/version"; exit 0
fi
if [ "$phase" = install ] && [[ " $* " == *' -g '* ]]; then
 printf 'legacy-install\n' >> "$ROOT/counts"
 [ ! -f "$ROOT/legacy-fail" ] || exit 39
 package=${!#}; bin=probe
 if [[ "$package" == @*/*@* ]] || [[ "$package" != @* && "$package" == *@* ]]; then package=${package%@*}; fi
 if [ "$package" = @modelcontextprotocol/server-sequential-thinking ]; then bin=mcp-server-sequential-thinking; fi
 prefix=$NPM_CONFIG_PREFIX;version=$(cat "$ROOT/version")
 mkdir -p "$prefix/bin" "$prefix/lib/node_modules/$package/bin"
 printf '{"version":"%s"}\n' "$version" > "$prefix/lib/node_modules/$package/package.json"
 printf '#!/bin/sh\nprintf "%%s\\n" "%s"\n' "$version" > "$prefix/lib/node_modules/$package/bin/$bin"
 chmod 755 "$prefix/lib/node_modules/$package/bin/$bin"
 ln -sf "../lib/node_modules/$package/bin/$bin" "$prefix/bin/$bin"
 exit 0
fi
[ -z "${NPM_TOKEN:-}" ] && [ -z "${SYNTHETIC_PROVIDER_KEY:-}" ]
[ "$NPM_CONFIG_USERCONFIG" = /dev/null ] && [ "$NPM_CONFIG_GLOBALCONFIG" = /dev/null ]
prefix=''
previous=''
for arg in "$@"; do
 if [ "$previous" = --prefix ]; then prefix=$arg; fi
 previous=$arg
done
[ -n "$prefix" ]
if [ "$phase" = install ]; then
 printf 'resolve\n' >> "$ROOT/counts"
 printf started > "$ROOT/resolve-started"
 while [ -f "$ROOT/resolve-wait" ]; do sleep 0.02; done
 case " $* " in *' --package-lock-only '*) ;; *) exit 34;; esac
 case " $* " in *' --ignore-scripts '*) ;; *) exit 34;; esac
 [ ! -f "$ROOT/fail" ] || exit 35
 version=$(cat "$ROOT/version")
 package=$(jq -r '.dependencies | keys[0]' "$prefix/package.json")
 selector=$(jq -r --arg pkg "$package" '.dependencies[$pkg]' "$prefix/package.json")
 if [ "$selector" != latest ]; then version=$selector; fi
 cat > "$prefix/package-lock.json" <<EOF
{"lockfileVersion":3,"packages":{"":{"dependencies":{"$package":"$selector"}},"node_modules/$package":{"version":"$version","resolved":"https://synthetic.invalid/probe-$version.tgz","integrity":"sha512-synthetic-$version","bin":{"probe":"bin/probe","mcp-server-sequential-thinking":"bin/probe"}}}}
EOF
 if [ -f "$ROOT/unsupported-lock" ]; then
  jq '.packages["node_modules/synthetic-git-dependency"]={"version":"1.0.0","resolved":"git+https://synthetic.invalid/dep.git#fixed"}' "$prefix/package-lock.json" > "$prefix/unsupported.json"
  mv "$prefix/unsupported.json" "$prefix/package-lock.json"
 fi
elif [ "$phase" = ci ]; then
 printf 'install\n' >> "$ROOT/counts"
 package=$(jq -r '.packages[""].dependencies | keys[0]' "$prefix/package-lock.json")
 version=$(jq -r --arg pkg "$package" '.packages["node_modules/"+$pkg].version' "$prefix/package-lock.json")
 mkdir -p "$prefix/node_modules/$package/bin" "$HOME/.config/login"
 printf secret > "$HOME/.config/login/token"
 printf '{"version":"%s"}\n' "$version" > "$prefix/node_modules/$package/package.json"
 printf '#!/bin/sh\nprintf "%%s\\n" "%s"\n' "$version" > "$prefix/node_modules/$package/bin/probe"
 chmod 755 "$prefix/node_modules/$package/bin/probe"
else exit 36; fi
`
	if err = os.WriteFile(filepath.Join(f.tools, "npm"), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}

	oldChild, oldCmd, oldProbe := programCaptureChild, forkBuildChildCommand, probeForkBuildContainer
	t.Cleanup(func() {
		programCaptureChild = oldChild
		forkBuildChildCommand = oldCmd
		probeForkBuildContainer = oldProbe
	})
	probeForkBuildContainer = func(string, string, time.Duration) (bool, bool) { return false, true }
	forkBuildChildCommand = func(args []string) (*exec.Cmd, error) {
		return exec.Command(self, append([]string{"npm-capture-runtime-test", f.root}, args...)...), nil
	}
	programCaptureChild = func(ctx context.Context, workspace string, argv []string, w io.Writer) error {
		if len(argv) > 2 && argv[2] == "program-npm" {
			f.resolves.Add(1)
		} else {
			f.installs.Add(1)
		}
		return oldChild(ctx, workspace, argv, w)
	}

	return f
}
func (f *selectedNPMFixture) version(t *testing.T, v string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(f.root, "version"), []byte(v), 0644); err != nil {
		t.Fatal(err)
	}
}
func (f *selectedNPMFixture) advance(t *testing.T, fresh bool) npmCaptureRecord {
	t.Helper()
	boundary, err := registerNPMCaptureRequest(context.Background(), f.store, f.in)
	if err != nil {
		t.Fatal(err)
	}
	r, err := advanceNPMCapture(context.Background(), f.store, f.in, "recipe-v1", fresh, boundary, io.Discard, func(commit func() error) error { return commit() })
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestSelectedNPMProducerActualColdWarmFreshAndFixedPin(t *testing.T) {
	f := newSelectedNPMFixture(t)
	cold := f.advance(t, false)
	if f.resolves.Load() != 1 || f.installs.Load() != 1 || cold.Identity == "" {
		t.Fatalf("cold counts %d/%d %+v", f.resolves.Load(), f.installs.Load(), cold)
	}
	for _, home := range []string{t.TempDir(), t.TempDir()} {
		if rc := runInternal([]string{"capture-materialize", "--store=" + f.store.Dir, "--home=" + home, "--bin=probe", "--compatibility=" + cold.Identity}); rc != 0 {
			t.Fatal(rc)
		}
		cmd := exec.Command(filepath.Join(home, ".npm-global/bin/probe"))
		got, err := cmd.Output()
		if err != nil || string(got) != "1.0.0\n" {
			t.Fatalf("%q %v", got, err)
		}
		if _, err := os.Stat(filepath.Join(home, ".config/login/token")); !os.IsNotExist(err) {
			t.Fatal("installer login/config bytes reached consumer")
		}
	}
	warm := f.advance(t, false)
	if warm.Entry != cold.Entry || f.resolves.Load() != 1 || f.installs.Load() != 1 {
		t.Fatal("warm path contacted resolver or installer")
	}
	unchanged := f.advance(t, true)
	if unchanged.Entry != cold.Entry || f.resolves.Load() != 2 || f.installs.Load() != 1 {
		t.Fatal("fresh unchanged rebuilt instead of checking")
	}
	f.version(t, "2.0.0")
	newer := f.advance(t, true)
	if newer.Identity == cold.Identity || f.resolves.Load() != 3 || f.installs.Load() != 2 {
		t.Fatal("fresh update did not prepare newer exact release")
	}
	f.in.Package = "probe-pkg@1.0.0"
	fixed := f.advance(t, true)
	if fixed.Resolution.Release != "1.0.0" {
		t.Fatal("fixed version advanced to registry latest")
	}
}

func TestSelectedNPMFreshFailureCancelAndStaleDoNotPublish(t *testing.T) {
	f := newSelectedNPMFixture(t)
	good := f.advance(t, false)
	if err := os.WriteFile(filepath.Join(f.root, "fail"), nil, 0644); err != nil {
		t.Fatal(err)
	}
	boundary, _ := registerNPMCaptureRequest(context.Background(), f.store, f.in)
	_, err := advanceNPMCapture(context.Background(), f.store, f.in, "recipe-v1", true, boundary, io.Discard, func(commit func() error) error { return commit() })
	if err == nil {
		t.Fatal("failed fresh check served old good as successful freshness")
	}
	r, _ := readNPMCaptureRecord(f.store, f.in)
	if r.Entry != good.Entry || r.Failure == "" {
		t.Fatal("failure replaced retained serving entry")
	}
	_ = os.Remove(filepath.Join(f.root, "fail"))
	f.version(t, "2.0.0")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = advanceNPMCapture(ctx, f.store, f.in, "recipe-v1", true, boundary, io.Discard, func(commit func() error) error { return commit() })
	if err == nil {
		t.Fatal("cancelled request completed")
	}
	var calls int
	stale := errors.New("selected declaration changed")
	_, err = advanceNPMCapture(context.Background(), f.store, f.in, "recipe-v1", true, boundary, io.Discard, func(commit func() error) error {
		calls++
		if calls > 1 {
			return stale
		}
		return commit()
	})
	if !errors.Is(err, stale) {
		t.Fatalf("stale publication: %v", err)
	}
	r, _ = readNPMCaptureRecord(f.store, f.in)
	if r.Entry != good.Entry {
		t.Fatal("stale generation published")
	}
}

func TestSelectedNPMResolutionObservesRuntimeAndRejectsLockConflict(t *testing.T) {
	f := newSelectedNPMFixture(t)
	r, err := resolveSelectedNPMCapture(context.Background(), f.store, f.in, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	inputs, err := r.inputs()
	if err != nil {
		t.Fatal(err)
	}
	if inputs.Release == "latest" || inputs.NodeABI == "" || len(inputs.Dependencies) != 1 || r.Runtime.NodeDigest == "" {
		t.Fatal("fabricated resolution facts")
	}
	original, _ := inputs.Identity()
	copy := *r
	copy.Runtime.ABI += "-other"
	other, _ := copy.inputs()
	identity, _ := other.Identity()
	if identity == original {
		t.Fatal("ABI conflict reused identity")
	}
	copy = *r
	copy.Runtime.Platform = "darwin/arm64"
	other, _ = copy.inputs()
	identity, _ = other.Identity()
	if identity == original {
		t.Fatal("platform conflict reused identity")
	}
	copy = *r
	copy.Release = "99"
	if _, err := copy.inputs(); err == nil {
		t.Fatal("resolution disagreed with lock")
	}
	var b bytes.Buffer
	raw, _ := json.Marshal(r)
	file := filepath.Join(t.TempDir(), "resolution.json")
	_ = os.WriteFile(file, raw, 0644)
	old := npmCaptureImagePath
	npmCaptureImagePath = filepath.Join(t.TempDir(), "missing")
	defer func() { npmCaptureImagePath = old }()
	if rc := runProgramNPM([]string{"install", file, original}, &b); rc == 0 {
		t.Fatal("unobserved installer runtime accepted")
	}
}
