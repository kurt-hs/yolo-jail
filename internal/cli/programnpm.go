package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/mschulkind-oss/yolo-jail/internal/capture"
	"github.com/mschulkind-oss/yolo-jail/internal/entrypoint"
	"github.com/mschulkind-oss/yolo-jail/internal/packdecl"
)

// npmCaptureInput is the entire installation authority passed into the sealed runtime.
// In particular it contains no server table, launch flags, environment or pack directory.
type npmCaptureInput struct {
	Owner     string
	Bin       string
	Package   string
	NodeFloor string
}

type npmCaptureRuntime struct {
	Image, Node, NodeDigest, NPM, Platform, ABI string
	TrustStore                                  string `json:"TrustStore,omitempty"`
}
type npmCaptureResolution struct {
	Unsupported              string `json:"unsupported,omitempty"`
	Input                    npmCaptureInput
	Runtime                  npmCaptureRuntime
	Lock                     json.RawMessage
	Release, Source, BinPath string
	Fingerprint              string
	Dependencies             []string
}

type npmCaptureLock struct {
	LockfileVersion int `json:"lockfileVersion"`
	Packages        map[string]struct {
		Version   string            `json:"version"`
		Resolved  string            `json:"resolved"`
		Integrity string            `json:"integrity"`
		Link      bool              `json:"link"`
		Bin       map[string]string `json:"bin"`
	} `json:"packages"`
}

func npmCaptureHash(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }
func (in npmCaptureInput) declarationIdentity() string {
	b, _ := json.Marshal(in)
	return npmCaptureHash(b)
}

func npmCaptureInputFor(owner string, inst packdecl.Install) (npmCaptureInput, bool) {
	// A self-update/refresh recipe is still executed by its established launcher.
	if inst.Kind != "npm" || len(inst.Flags) > 0 || len(inst.UpdateVerb) > 0 || inst.Refresh != nil || !packdecl.ValidBinName(inst.Bin) {
		return npmCaptureInput{}, false
	}
	in := npmCaptureInput{Owner: owner, Bin: inst.Bin, Package: inst.Package, NodeFloor: inst.NodeFloor}
	if _, _, err := npmCaptureSpec(in.Package); err != nil {
		return npmCaptureInput{}, false
	}
	return in, true
}

func npmCaptureSpec(spec string) (name, selector string, err error) {
	// Accept registry declarations only, not a path/URL/alias whose resolver differs.
	if spec == "" || strings.ContainsAny(spec, " \t\r\n\\:%") {
		return "", "", fmt.Errorf("unsupported npm registry declaration %q — use its existing launcher", spec)
	}
	i := strings.LastIndex(spec, "@")
	name, selector = spec, "latest"
	if i > 0 {
		name, selector = spec[:i], spec[i+1:]
	}
	if selector == "" || strings.Contains(selector, "/") {
		return "", "", fmt.Errorf("invalid npm selector — repair the package declaration")
	}
	parts := strings.Split(name, "/")
	if strings.HasPrefix(name, "@") {
		if len(parts) != 2 || len(parts[0]) < 2 {
			return "", "", fmt.Errorf("invalid scoped npm package — repair the package declaration")
		}
	} else if len(parts) != 1 {
		return "", "", fmt.Errorf("unsupported npm source — use its existing launcher")
	}
	for _, p := range parts {
		if p == "" || p == "." || p == ".." {
			return "", "", fmt.Errorf("invalid npm package — repair the package declaration")
		}
	}
	return name, selector, nil
}

var errNPMCaptureUnsupported = errors.New("npm dependency graph is unsupported for sealed capture")

func (r npmCaptureResolution) inputs() (capture.ProgramInputs, error) {
	if r.Unsupported != "" {
		return capture.ProgramInputs{}, fmt.Errorf("%w: %s", errNPMCaptureUnsupported, r.Unsupported)
	}
	name, _, err := npmCaptureSpec(r.Input.Package)
	if err != nil {
		return capture.ProgramInputs{}, err
	}
	var lock npmCaptureLock
	if err = json.Unmarshal(r.Lock, &lock); err != nil {
		return capture.ProgramInputs{}, err
	}
	if lock.LockfileVersion < 2 {
		return capture.ProgramInputs{}, fmt.Errorf("%w: complete dependency lock requires supported npm — retain the existing launcher", errNPMCaptureUnsupported)
	}
	root, ok := lock.Packages["node_modules/"+name]
	if !ok || root.Version == "" || root.Resolved == "" || root.Integrity == "" || root.Link {
		return capture.ProgramInputs{}, fmt.Errorf("%w: exact registry source/integrity is unavailable — use the existing launcher", errNPMCaptureUnsupported)
	}
	bin := root.Bin[r.Input.Bin]
	if !filepath.IsLocal(bin) || strings.Contains(bin, "\\") {
		return capture.ProgramInputs{}, fmt.Errorf("npm package has no confined declared bin %s — repair its declaration", r.Input.Bin)
	}
	deps := make([]string, 0, len(lock.Packages))
	for p, e := range lock.Packages {
		if p == "" {
			continue
		}
		if !strings.HasPrefix(p, "node_modules/") || !filepath.IsLocal(p) || e.Link || e.Version == "" || e.Resolved == "" || e.Integrity == "" {
			return capture.ProgramInputs{}, fmt.Errorf("%w: unresolved dependency %q — use the existing launcher", errNPMCaptureUnsupported, p)
		}
		deps = append(deps, p+"="+e.Version+"|"+e.Resolved+"|"+e.Integrity)
	}
	sort.Strings(deps)
	if r.Release != root.Version || r.Source != root.Resolved+"|"+root.Integrity || r.BinPath != bin || strings.Join(deps, "\n") != strings.Join(r.Dependencies, "\n") {
		return capture.ProgramInputs{}, fmt.Errorf("npm resolution does not match its dependency lock — retry preparation")
	}
	observed, _ := json.Marshal(r.Runtime)
	if r.Runtime.Image == "" || r.Runtime.Node == "" || r.Runtime.NodeDigest == "" || r.Runtime.NPM == "" || r.Runtime.Platform == "" || r.Runtime.ABI == "" || r.Runtime.TrustStore != "" && r.Runtime.TrustStore != npmCaptureTrustStore {
		return capture.ProgramInputs{}, fmt.Errorf("npm capture has no observed runtime — retry in the sealed runtime")
	}
	recipe := []byte("npm-ci-local-v1\x00" + r.BinPath + "\x00" + npmCaptureHash(r.Lock) + "\x00" + r.Fingerprint)
	return capture.ProgramInputs{Owner: r.Input.Owner, Bin: r.Input.Bin, Declared: r.Input.Package, Resolver: "npm", Release: r.Release, Source: r.Source, Recipe: npmCaptureHash(recipe), Dependencies: deps, Runtime: string(observed), Platform: r.Runtime.Platform, NodeABI: r.Runtime.ABI}, nil
}

// npmCaptureEnvironment is explicit even under the outer seal. npm configuration and
// credential variables inherited from another command must not retarget dependency resolution.
const npmCaptureTrustStorePath = "/etc/ssl/certs/ca-bundle.crt"
const npmCaptureTrustStore = "openssl:file=" + npmCaptureTrustStorePath + ";dir=disabled"

func npmCaptureEnvironment(home, node string) []string {
	// Disable OpenSSL's compiled-in directory so only the immutable image bundle is trusted.
	return []string{"HOME=" + home, "PATH=" + filepath.Dir(node) + ":/bin:/usr/bin", "LANG=C.UTF-8", "SSL_CERT_FILE=" + npmCaptureTrustStorePath, "SSL_CERT_DIR=", "NODE_OPTIONS=--use-openssl-ca", "NPM_CONFIG_CACHE=" + filepath.Join(home, ".cache/npm"), "NPM_CONFIG_USERCONFIG=/dev/null", "NPM_CONFIG_GLOBALCONFIG=/dev/null", "NPM_CONFIG_UPDATE_NOTIFIER=false", "NPM_CONFIG_FUND=false", "MISE_DATA_DIR=" + filepath.Join(home, ".local/share/mise"), "MISE_CONFIG_DIR=" + filepath.Join(home, ".config/mise"), "MISE_YES=1"}
}

func validateNPMCaptureTrustStore(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("sealed npm producer image CA bundle %s is unavailable — repair the selected image, then retry preparation: %w", path, err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return fmt.Errorf("sealed npm producer image CA bundle %s cannot be inspected — repair the selected image, then retry preparation: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("sealed npm producer image CA bundle %s is not a regular file — repair the selected image, then retry preparation", path)
	}
	return nil
}

func npmCaptureNode(ctx context.Context, home, floor string, w io.Writer) (string, error) {
	if err := validateNPMCaptureTrustStore(npmCaptureTrustStorePath); err != nil {
		return "", err
	}
	if floor == "" {
		return exec.LookPath("node")
	}
	if node := entrypoint.ResolveNodeForFloor(floor); node != "" {
		return node, nil
	}
	if !packdecl.ValidNodeFloor(floor) {
		return "", fmt.Errorf("invalid Node floor — repair the selected declaration")
	}
	cmd := exec.CommandContext(ctx, "mise", "install", "node@"+floor)
	cmd.Env = npmCaptureEnvironment(home, "/bin/node")
	cmd.Stdout, cmd.Stderr = w, w
	if err := cmd.Run(); err != nil {
		return "", err
	}
	// Resolve the actual installed path, never infer an ABI from the requested floor.
	cmd = exec.CommandContext(ctx, "mise", "where", "node@"+floor)
	cmd.Env = npmCaptureEnvironment(home, "/bin/node")
	b, err := cmd.Output()
	if err != nil {
		return "", err
	}
	node := filepath.Join(strings.TrimSpace(string(b)), "bin/node")
	if _, err := os.Stat(node); err != nil {
		return "", err
	}
	return node, nil
}

var npmCaptureImagePath = "/etc/yolo-jail-image-identity"

func observeNPMCaptureRuntime(ctx context.Context, home, node string) (npmCaptureRuntime, error) {
	image, err := os.ReadFile(npmCaptureImagePath)
	if err != nil {
		return npmCaptureRuntime{}, err
	}
	cmd := exec.CommandContext(ctx, node, "-p", "JSON.stringify({node:process.version,abi:process.versions.modules,platform:process.platform,arch:process.arch})")
	cmd.Env = npmCaptureEnvironment(home, node)
	b, err := cmd.Output()
	if err != nil {
		return npmCaptureRuntime{}, err
	}
	var p struct{ Node, ABI, Platform, Arch string }
	if err = json.Unmarshal(b, &p); err != nil {
		return npmCaptureRuntime{}, err
	}
	binary, err := os.ReadFile(node)
	if err != nil {
		return npmCaptureRuntime{}, err
	}
	cmd = exec.CommandContext(ctx, filepath.Join(filepath.Dir(node), "npm"), "--version")
	cmd.Env = npmCaptureEnvironment(home, node)
	npm, err := cmd.Output()
	if err != nil {
		return npmCaptureRuntime{}, err
	}
	arch := p.Arch
	if arch == "x64" {
		arch = "amd64"
	}
	return npmCaptureRuntime{Image: strings.TrimSpace(string(image)), Node: p.Node, NodeDigest: npmCaptureHash(binary), NPM: strings.TrimSpace(string(npm)), Platform: p.Platform + "/" + arch, ABI: p.ABI, TrustStore: npmCaptureTrustStore}, nil
}

func resolveNPMCapture(ctx context.Context, in npmCaptureInput, dir, home string, w io.Writer) (*npmCaptureResolution, error) {
	name, selector, err := npmCaptureSpec(in.Package)
	if err != nil {
		return nil, err
	}
	node, err := npmCaptureNode(ctx, home, in.NodeFloor, w)
	if err != nil {
		return nil, err
	}
	observed, err := observeNPMCaptureRuntime(ctx, home, node)
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}
	pkg, _ := json.Marshal(map[string]any{"private": true, "dependencies": map[string]string{name: selector}})
	if err = os.WriteFile(filepath.Join(dir, "package.json"), pkg, 0644); err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, filepath.Join(filepath.Dir(node), "npm"), "install", "--package-lock-only", "--ignore-scripts", "--prefer-online", "--prefix", dir)
	cmd.Env = npmCaptureEnvironment(home, node)
	cmd.Stdout, cmd.Stderr = w, w
	if err = cmd.Run(); err != nil {
		return nil, err
	}
	lockBytes, err := os.ReadFile(filepath.Join(dir, "package-lock.json"))
	if err != nil {
		return nil, err
	}
	var lock npmCaptureLock
	if err = json.Unmarshal(lockBytes, &lock); err != nil {
		return nil, err
	}
	root := lock.Packages["node_modules/"+name]
	r := &npmCaptureResolution{Input: in, Runtime: observed, Lock: lockBytes, Release: root.Version, Source: root.Resolved + "|" + root.Integrity, BinPath: root.Bin[in.Bin]}
	for p, e := range lock.Packages {
		if p != "" {
			r.Dependencies = append(r.Dependencies, p+"="+e.Version+"|"+e.Resolved+"|"+e.Integrity)
		}
	}
	sort.Strings(r.Dependencies)
	if _, err = r.inputs(); err != nil {
		if errors.Is(err, errNPMCaptureUnsupported) {
			return &npmCaptureResolution{Input: in, Unsupported: err.Error()}, nil
		}
		return nil, err
	}
	return r, nil
}

func npmCaptureArtifacts(in npmCaptureInput, identity string) []string {
	name, _, _ := npmCaptureSpec(in.Package)
	return []string{".local/lib/yolo-npm/" + identity, ".npm-global/bin/" + in.Bin, ".npm-global/lib/node_modules/" + name}
}

func installNPMCapture(ctx context.Context, r npmCaptureResolution, identity, home string, w io.Writer) error {
	inputs, err := r.inputs()
	if err != nil {
		return err
	}
	want, err := inputs.Identity()
	if err != nil {
		return err
	}
	if want != identity {
		return fmt.Errorf("npm capture identity changed — retry preparation")
	}
	node, err := npmCaptureNode(ctx, home, r.Input.NodeFloor, w)
	if err != nil {
		return err
	}
	actual, err := observeNPMCaptureRuntime(ctx, home, node)
	if err != nil {
		return err
	}
	if actual != r.Runtime || actual.Platform != runtime.GOOS+"/"+runtime.GOARCH {
		return fmt.Errorf("npm producer runtime changed after resolution — retry preparation")
	}
	name, _, _ := npmCaptureSpec(r.Input.Package)
	root := filepath.Join(home, ".local/lib/yolo-npm", identity)
	if err = os.MkdirAll(root, 0755); err != nil {
		return err
	}
	pkg, _ := json.Marshal(map[string]any{"private": true, "dependencies": map[string]string{name: r.Release}})
	// Preserve the root selector from the lock: npm ci checks it before installing.
	var lock struct {
		Packages map[string]json.RawMessage `json:"packages"`
	}
	_ = json.Unmarshal(r.Lock, &lock)
	if raw := lock.Packages[""]; len(raw) > 0 {
		var p map[string]any
		if json.Unmarshal(raw, &p) == nil {
			p["private"] = true
			pkg, _ = json.Marshal(p)
		}
	}
	if err = os.WriteFile(filepath.Join(root, "package.json"), pkg, 0644); err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(root, "package-lock.json"), r.Lock, 0644); err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, filepath.Join(filepath.Dir(node), "npm"), "ci", "--prefer-online", "--prefix", root)
	cmd.Env = npmCaptureEnvironment(home, node)
	cmd.Stdout, cmd.Stderr = w, w
	if err = cmd.Run(); err != nil {
		return err
	}
	pkgBytes, err := os.ReadFile(filepath.Join(root, "node_modules", name, "package.json"))
	if err != nil {
		return err
	}
	var installed struct{ Version string }
	if err := json.Unmarshal(pkgBytes, &installed); err != nil || installed.Version != r.Release {
		return fmt.Errorf("installed npm package does not match the resolved release — retry preparation without publishing it")
	}
	target := filepath.Join(root, "node_modules", name, filepath.FromSlash(r.BinPath))
	fi, err := os.Stat(target)
	if err != nil || !fi.Mode().IsRegular() {
		return fmt.Errorf("npm did not install the selected binary — repair the package declaration")
	}
	for link, to := range map[string]string{filepath.Join(home, ".npm-global/bin", r.Input.Bin): target, filepath.Join(home, ".npm-global/lib/node_modules", name): filepath.Join(root, "node_modules", name)} {
		if err = os.MkdirAll(filepath.Dir(link), 0755); err != nil {
			return err
		}
		relative, err := filepath.Rel(filepath.Dir(link), to)
		if err != nil {
			return err
		}
		if err = os.Symlink(filepath.ToSlash(relative), link); err != nil {
			return err
		}
	}
	return nil
}

// runProgramNPM is hidden; only the sealed producer emits this pure-input command.
func runProgramNPM(args []string, w io.Writer) int {
	if len(args) != 3 || (args[0] != "resolve" && args[0] != "install") {
		fmt.Fprintln(w, "usage: yolo internal program-npm resolve|install INPUT OUTPUT-OR-IDENTITY")
		return 2
	}
	raw, err := os.ReadFile(args[1])
	if err != nil {
		fmt.Fprintln(w, err)
		return 1
	}
	ctx := context.Background()
	home := os.Getenv("HOME")
	if args[0] == "resolve" {
		if os.Getenv(entrypoint.SealedBuildEnv) != "1" {
			fmt.Fprintln(w, "npm resolution requires its sealed producer — use the existing launcher")
			return 1
		}
		var in npmCaptureInput
		err = json.Unmarshal(raw, &in)
		var r *npmCaptureResolution
		if err == nil {
			r, err = resolveNPMCapture(ctx, in, path.Join(filepath.Dir(args[2]), "lock"), home, w)
		}
		if err == nil {
			var b []byte
			b, err = json.Marshal(r)
			if err == nil {
				err = os.WriteFile(args[2], b, 0644)
			}
		}
	} else {
		var r npmCaptureResolution
		err = json.Unmarshal(raw, &r)
		if err == nil {
			err = installNPMCapture(ctx, r, args[2], home, w)
		}
	}
	if err != nil {
		fmt.Fprintf(w, "npm capture: %v — retry the selected preparation\n", err)
		return 1
	}
	return 0
}
