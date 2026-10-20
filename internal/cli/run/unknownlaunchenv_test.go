package run

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
	"github.com/mschulkind-oss/yolo-jail/internal/macosuser"
	"github.com/mschulkind-oss/yolo-jail/internal/packdecl"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

func TestLaunchYOLOEnvRecognitionCoversDirectReaders(t *testing.T) {
	known := knownLaunchYOLOEnvNames(nil, nil)
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	readers := 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Clean(name), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) != 1 {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || selector.Sel.Name != "Getenv" {
				return true
			}
			base, ok := selector.X.(*ast.Ident)
			if !ok || base.Name != "o" && base.Name != "os" {
				return true
			}
			literal, ok := call.Args[0].(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				return true
			}
			key, err := strconv.Unquote(literal.Value)
			if err != nil || !strings.HasPrefix(key, "YOLO_") {
				return true
			}
			readers++
			if !slices.Contains(known, key) {
				t.Errorf("direct environment reader in %s uses unrecognized %s", name, key)
			}
			return true
		})
	}
	if readers == 0 {
		t.Fatal("no direct YOLO environment readers found; census no longer measures the package")
	}
}

func TestKnownLaunchYOLOEnvNamesUseSelectedDeclarations(t *testing.T) {
	selected := &packload.Pack{Name: "selected", Decl: &packdecl.Manifest{Contributes: []packdecl.Contribution{
		{Kind: packdecl.KindEnv, Vars: map[string]string{"YOLO_SELECTED_PACK_SETTING": "not-used"}},
		{Kind: packdecl.KindService, Name: "selected-service"},
		{Kind: packdecl.KindProgram, Bin: "selected-tool"},
	}}}
	known := knownLaunchYOLOEnvNames([]*packload.Pack{selected}, &packChannel{
		callerTokens: map[string]string{"YOLO_SERVICE_SELECTED_SERVICE_TOKEN": "secret-not-read"},
	})
	for _, name := range []string{
		"YOLO_SELECTED_PACK_SETTING",
		paths.ServiceEnvVarPrefix + "SELECTED_SERVICE" + paths.ServiceEnvVarSuffix,
		"YOLO_SERVICE_SELECTED_SERVICE_TOKEN",
		"YOLO_AUTH_PRELAUNCH_SELECTED_TOOL_FLAG",
		"YOLO_AUTH_PRELAUNCH_SELECTED_TOOL_PATH",
		"YOLO_AUTH_PRELAUNCH_SELECTED_TOOL_LOGIN",
	} {
		if !slices.Contains(known, name) {
			t.Errorf("selected declaration name %s not recognized", name)
		}
	}
	for _, name := range []string{"YOLO_BACKEND", "YOLO_SELECTED_OTHER_PACK_SETTING", "YOLO_AUTH_PRELAUNCH_UNSELECTED_TOOL_FLAG"} {
		if slices.Contains(known, name) {
			t.Errorf("unselected or unrecognized name %s was accepted", name)
		}
	}
	if !slices.IsSorted(known) {
		t.Errorf("known names are not deterministic/sorted: %v", known)
	}
}

func TestKnownLaunchYOLOEnvNamesIncludeHydratedChannelNames(t *testing.T) {
	userEnv := jsonx.NewOrderedMap()
	userEnv.Set("YOLO_CONFIGURED_CHANNEL_NAME", "secret-not-read")
	known := knownLaunchYOLOEnvNames(nil, &packChannel{userEnv: userEnv})
	if !slices.Contains(known, "YOLO_CONFIGURED_CHANNEL_NAME") {
		t.Fatalf("hydrated channel key was not recognized: %v", known)
	}
}

func TestRunInvokesUnknownYOLOEnvironmentWarningBeforeBackend(t *testing.T) {
	var stdout, stderr bytes.Buffer
	o := dispatchOptions(t, t.TempDir(), "macos-user", &stdout, &stderr, nil)
	called := 0
	o.WarnUnknownYOLOEnv = func(names []string) {
		called++
		for _, name := range []string{"YOLO_RUNTIME", "YOLO_PACK_ROOT", "YOLO_CTX_ROOT", "YOLO_MCP_SERVERS", "YOLO_MODEL_FRONTIER"} {
			if !slices.Contains(names, name) {
				t.Errorf("recognized name %s missing from callback input %v", name, names)
			}
		}
		if slices.Contains(names, "YOLO_BACKEND") {
			t.Error("YOLO_BACKEND must remain unrecognized")
		}
	}
	o.MacosUserRun = func(*jsonx.OrderedMap, string, []string, []string, string, string, macosuser.HomeOverlay, macosuser.HostContext, bool, *jsonx.OrderedMap, []packload.BlockedTool, macosuser.JailDaemons) int {
		if called != 1 {
			t.Errorf("warning callback calls before backend = %d, want exactly one", called)
		}
		return 0
	}
	if rc := Run(*o); rc != 0 {
		t.Fatalf("Run() = %d\nstdout:\n%s\nstderr:\n%s", rc, stdout.String(), stderr.String())
	}
	if called != 1 {
		t.Fatalf("warning callback calls = %d, want exactly one", called)
	}
}

func TestUnknownYOLOBackendDoesNotSelectRuntime(t *testing.T) {
	var stdout, stderr bytes.Buffer
	o := dispatchOptions(t, t.TempDir(), "", &stdout, &stderr, nil)
	o.Getenv = func(name string) string {
		if name == "YOLO_BACKEND" {
			return "podman"
		}
		return ""
	}
	if rt, ok := o.resolveRuntime(jsonx.NewOrderedMap()); ok {
		t.Fatalf("YOLO_BACKEND selected runtime %q", rt)
	}
}

func TestRecognizedYOLORuntimeStillSelectsPodman(t *testing.T) {
	var stdout, stderr bytes.Buffer
	o := dispatchOptions(t, t.TempDir(), "podman", &stdout, &stderr, nil)
	if rt, ok := o.resolveRuntime(jsonx.NewOrderedMap()); !ok || rt != "podman" {
		t.Fatalf("YOLO_RUNTIME=podman selection = %q, %v; want podman, true", rt, ok)
	}
}
