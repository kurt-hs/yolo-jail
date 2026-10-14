package cli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/capture"
	"github.com/mschulkind-oss/yolo-jail/internal/cli/run"
	"github.com/mschulkind-oss/yolo-jail/internal/config"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

// This exact tarball's entire 52-file source was audited: local semver mathematics,
// static relative requires, console/argv only; no runtime/optional/bundled deps,
// install lifecycle scripts, binding.gyp or native binaries. Mutable metadata is
// never installation authority here: both fetched bytes and the real resolver lock
// must match this pre-audited digest before the unchanged npm ci producer runs.
const auditedSemverIntegrity = "sha512-RF0Fw+rO5AMf9MAyaRXI4AV0Ulj5lMHqVxxdSgiVbixSCXoEmmX/jk0CuJw4+3SqroYO9VoUh+HcuJivvtJemA=="
const auditedSemverURL = "https://registry.npmjs.org/semver/-/semver-7.7.2.tgz"

func auditSealedSemverTarball(raw []byte) error {
	digest := sha512.Sum512(raw)
	if "sha512-"+base64.StdEncoding.EncodeToString(digest[:]) != auditedSemverIntegrity {
		return fmt.Errorf("unaudited semver tarball digest")
	}
	gz, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	count := 0
	var manifest []byte
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if h.Typeflag != tar.TypeReg || !strings.HasPrefix(h.Name, "package/") || !filepath.IsLocal(h.Name) || strings.Contains(h.Name, "\\") || filepath.Base(h.Name) == "binding.gyp" {
			return fmt.Errorf("unsafe tarball entry %q", h.Name)
		}
		b, err := io.ReadAll(tr)
		if err != nil {
			return err
		}
		count++
		if h.Name == "package/package.json" {
			manifest = b
		}
	}
	var p struct {
		Name, Version                                        string
		Bin                                                  map[string]string
		Scripts                                              map[string]string
		Dependencies, OptionalDependencies, PeerDependencies map[string]any
		BundleDependencies, BundledDependencies              json.RawMessage
	}
	if err := json.Unmarshal(manifest, &p); err != nil {
		return err
	}
	if count != 52 || p.Name != "semver" || p.Version != "7.7.2" || len(p.Bin) != 1 || p.Bin["semver"] != "bin/semver.js" || len(p.Dependencies)+len(p.OptionalDependencies)+len(p.PeerDependencies) != 0 || len(p.BundleDependencies)+len(p.BundledDependencies) != 0 {
		return fmt.Errorf("unaudited package graph or bin")
	}
	for _, hook := range []string{"preinstall", "install", "postinstall", "prepublish", "preprepare", "prepare", "postprepare", "prepack", "postpack"} {
		if _, ok := p.Scripts[hook]; ok {
			return fmt.Errorf("unaudited lifecycle %s", hook)
		}
	}
	return nil
}

func auditSealedSemverResolution(raw []byte) error {
	var r npmCaptureResolution
	if err := json.Unmarshal(raw, &r); err != nil {
		return err
	}
	if r.Input.Package != "semver@7.7.2" || r.Input.Bin != "semver" || r.Release != "7.7.2" || r.Source != auditedSemverURL+"|"+auditedSemverIntegrity || r.BinPath != "bin/semver.js" || r.Runtime.TrustStore != npmCaptureTrustStore {
		return fmt.Errorf("resolver changed audited source")
	}
	if _, err := r.inputs(); err != nil {
		return err
	}
	var lock struct {
		LockfileVersion int
		Packages        map[string]struct {
			Version, Resolved, Integrity                         string
			Bin                                                  map[string]string
			Dependencies, OptionalDependencies, PeerDependencies map[string]any
			HasInstallScript, Link                               bool
		}
	}
	if err := json.Unmarshal(r.Lock, &lock); err != nil {
		return err
	}
	if lock.LockfileVersion != 3 || len(lock.Packages) != 2 {
		return fmt.Errorf("unaudited resolver graph")
	}
	declaration, present := lock.Packages[""]
	if !present || len(declaration.Dependencies) != 1 || declaration.Dependencies["semver"] != "7.7.2" || len(declaration.OptionalDependencies)+len(declaration.PeerDependencies) != 0 || declaration.HasInstallScript || declaration.Link {
		return fmt.Errorf("unaudited root declaration")
	}
	root, ok := lock.Packages["node_modules/semver"]
	if !ok || root.Version != "7.7.2" || root.Resolved != auditedSemverURL || root.Integrity != auditedSemverIntegrity || root.HasInstallScript || root.Link || len(root.Dependencies)+len(root.OptionalDependencies)+len(root.PeerDependencies) != 0 || len(root.Bin) != 1 || root.Bin["semver"] != "bin/semver.js" {
		return fmt.Errorf("unaudited resolver package")
	}
	return nil
}

// Explicit opt-in only, never under -short. Requires Linux Podman, an absolute
// freshly built candidate launcher and its exact source root. Each child is owned
// and bounded by the existing lifecycle; the whole test requires a <=5m deadline.
// Uses anonymous verified TLS with only the immutable image CA bundle. No agents,
// models, host services or fetched hooks run. The launch callback is the existing
// public seam; after its real preparation, a real candidate CLI consumes the archive.
func TestAuditedRegistrySealedPublicColdWarmAndFreshRefusal(t *testing.T) {
	if testing.Short() || os.Getenv("YOLO_TEST_AUDITED_REGISTRY") != "1" {
		t.Skip("explicit audited registry runtime opt-in; not a short test")
	}
	if runtime.GOOS != "linux" {
		t.Skip("Linux Podman proof")
	}
	deadline, ok := t.Deadline()
	if !ok || time.Until(deadline) > 5*time.Minute {
		t.Fatal("run with -timeout <=5m")
	}
	root, exe := os.Getenv("YOLO_REPO_ROOT"), os.Getenv("YOLO_TEST_AUDITED_LAUNCHER")
	if !filepath.IsAbs(root) || !filepath.IsAbs(exe) {
		t.Fatal("absolute source root and fresh launcher required")
	}
	if _, err := exec.LookPath("podman"); err != nil {
		t.Skip("Podman unavailable")
	}
	ctx, cancel := context.WithDeadline(context.Background(), deadline.Add(-10*time.Second))
	defer cancel()
	owned, err := os.MkdirTemp("", "audited-sealed-registry-")
	if err != nil {
		t.Fatal(err)
	}
	t.Log("retained owned evidence", owned)
	for key, sub := range map[string]string{"HOME": "home", "XDG_CONFIG_HOME": "config", "XDG_DATA_HOME": "data", "XDG_CACHE_HOME": "cache", "XDG_RUNTIME_DIR": "runtime"} {
		dir := filepath.Join(owned, sub)
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		t.Setenv(key, dir)
	}
	// Neither the public consumer nor the proof process inherits the live jail's
	// service endpoints, pack tree, provider table or workspace identities.
	for _, item := range os.Environ() {
		key, _, _ := strings.Cut(item, "=")
		if strings.HasPrefix(key, "YOLO_") {
			t.Setenv(key, "")
		}
	}
	t.Setenv("YOLO_REPO_ROOT", root)
	t.Setenv("YOLO_RUNTIME", "podman")
	t.Setenv("NO_COLOR", "1")
	// Discard caller trust/proxy/credential knobs, not the shared Go build cache.
	for _, item := range os.Environ() {
		key, _, _ := strings.Cut(item, "=")
		upper := strings.ToUpper(key)
		if strings.HasPrefix(upper, "NPM_") || strings.HasPrefix(upper, "NODE_") || strings.HasSuffix(upper, "_PROXY") || upper == "SSL_CERT_FILE" || upper == "SSL_CERT_DIR" || upper == "NIX_SSL_CERT_FILE" {
			t.Setenv(key, "")
		}
	}
	writeFile(t, paths.UserConfigPath(), `{}`)
	auditDir := filepath.Join(owned, "audit")
	if err := os.Mkdir(auditDir, 0700); err != nil {
		t.Fatal(err)
	}
	fetch := `const https=require('https'),fs=require('fs');(async()=>{for(const [file,url] of [['metadata.json','https://registry.npmjs.org/semver/7.7.2'],['semver.tgz','` + auditedSemverURL + `']]){await new Promise((resolve,reject)=>{https.get(url,r=>{if(r.statusCode!==200){r.resume();reject(Error('status '+r.statusCode));return;}const f=fs.createWriteStream('/workspace/'+file);r.pipe(f);f.on('finish',()=>{f.close();console.log('AUDIT_FETCH',file,r.statusCode);resolve();});f.on('error',reject);}).on('error',reject);});}})().catch(e=>{console.error(e);process.exitCode=1});`
	writeFile(t, filepath.Join(auditDir, "fetch.js"), fetch)
	var log auditedSealedLog
	cmd := exec.CommandContext(ctx, exe, "internal", programCaptureJailVerb, "--workspace="+auditDir, "--", "bash", "-c", `env -i HOME="$HOME" PATH="$PATH" SSL_CERT_FILE=/etc/ssl/certs/ca-bundle.crt SSL_CERT_DIR= NODE_OPTIONS=--use-openssl-ca node /workspace/fetch.js`)
	cmd.Env = programCaptureEnvironment(os.Environ())
	cmd.Stdout = &log
	cmd.Stderr = &log
	if err := cmd.Run(); err != nil {
		t.Fatalf("sealed audit fetch: %v\n%s", err, log.String())
	}
	raw, err := os.ReadFile(filepath.Join(auditDir, "semver.tgz"))
	if err != nil {
		t.Fatal(err)
	}
	if err := auditSealedSemverTarball(raw); err != nil {
		t.Fatal(err)
	}
	metadata, err := os.ReadFile(filepath.Join(auditDir, "metadata.json"))
	if err != nil {
		t.Fatal(err)
	}
	var meta struct {
		Dist struct{ Integrity, Tarball string }
	}
	if err := json.Unmarshal(metadata, &meta); err != nil || meta.Dist.Integrity != auditedSemverIntegrity || meta.Dist.Tarball != auditedSemverURL {
		t.Fatal("metadata disagrees with immutable audited bytes", err)
	}
	pack := filepath.Join(owned, "pack")
	workspace := filepath.Join(owned, "consumer")
	for _, p := range []string{pack, workspace} {
		if err := os.Mkdir(p, 0700); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(t, filepath.Join(pack, "pack.json"), `{"name":"audited-semver","contributes":[{"kind":"program","bin":"semver","via":"npm","package":"semver@7.7.2"}]}`)
	cfgJSON, _ := json.Marshal(map[string]any{"packs": []any{map[string]string{"source": pack, "name": "audited-semver"}}, "agent_updates": false})
	writeFile(t, paths.UserConfigPath(), string(cfgJSON))
	selected := selectConfiguredHostPacks()
	if len(selected.problems()) != 0 {
		t.Fatal(selected.problems())
	}
	cfg, err := config.LoadConfig(workspace, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	fingerprint, err := run.ProgramRuntimeFingerprint(root, cfg)
	if err != nil {
		t.Fatal(err)
	}
	req := run.ProgramCaptureRequest{Packs: selected.packs, Env: ordinarySelectionEnvironment(cfg, selected.packs), Runtime: "podman", Platform: captureJailPlatform(), Fingerprint: fingerprint, RepoRoot: root, Workspace: workspace}
	store := &capture.Store{Dir: paths.CapturesDir()}
	in := npmCaptureInput{Owner: "audited-semver", Bin: "semver", Package: "semver@7.7.2"}
	originalChild, originalCommand, originalSpawn, originalLaunch := programCaptureChild, forkBuildChildCommand, sharedPreparationSpawn, launchRunPipeline
	var workers sync.WaitGroup
	t.Cleanup(func() {
		workers.Wait()
		programCaptureChild = originalChild
		forkBuildChildCommand = originalCommand
		sharedPreparationSpawn = originalSpawn
		launchRunPipeline = originalLaunch
		writeFile(t, filepath.Join(owned, "runtime.log"), log.String())
	})
	forkBuildChildCommand = func(argv []string) (*exec.Cmd, error) { return exec.Command(exe, argv...), nil }
	sharedPreparationSpawn = func(_ context.Context, d preparationDemand) error {
		workers.Add(1)
		go func() { defer workers.Done(); _ = runSharedPreparation(ctx, d, &log) }()
		return nil
	}
	var resolves, installs atomic.Int32
	var denyFresh atomic.Bool
	programCaptureChild = func(childCtx context.Context, staging string, argv []string, w io.Writer) error {
		isResolve := len(argv) > 3 && argv[2] == "program-npm" && argv[3] == "resolve"
		if !isResolve {
			b, err := os.ReadFile(filepath.Join(staging, "resolution.json"))
			if err != nil {
				return err
			}
			if err := auditSealedSemverResolution(b); err != nil {
				return err
			}
			if err := auditSealedSemverTarball(raw); err != nil {
				return err
			}
			installs.Add(1)
		}
		err := originalChild(childCtx, staging, argv, w)
		if err != nil {
			return err
		}
		if isResolve {
			resolves.Add(1)
			b, err := os.ReadFile(filepath.Join(staging, "resolution.json"))
			if err != nil {
				return err
			}
			if err := auditSealedSemverResolution(b); err != nil {
				return err
			}
			if denyFresh.Load() {
				return fmt.Errorf("controlled fresh audit refusal — retry preparation")
			}
		}
		return nil
	}
	consumerRuns := 0
	launchRunPipeline = func(o run.Options) int {
		if o.PreparePrograms == nil {
			t.Error("public callback missing")
			return 1
		}
		req.Fresh = o.Update
		req.Act = &run.ActInterrupt{}
		req.Stderr = &log
		deliveries, err := o.PreparePrograms(req)
		workers.Wait()
		if err != nil {
			fmt.Fprintln(&log, "PUBLIC_REFUSAL", err)
			if len(deliveries) != 0 {
				t.Error("failed fresh check returned stale delivery")
			}
			return 1
		}
		if _, ok := deliveries["semver"]; !ok {
			t.Error("no real capture delivery")
			return 1
		}
		consumerRuns++
		command := exec.CommandContext(ctx, exe, "run", "--accept-config-changes", "--", "semver", "-r", "^1.0.0", "1.2.3", "2.0.0")
		command.Dir = workspace
		command.Env = programCaptureEnvironment(os.Environ())
		output, err := command.CombinedOutput()
		fmt.Fprintf(&log, "CONSUMER %d\n%s\n", consumerRuns, output)
		if err != nil || !strings.Contains(string(output), "\n1.2.3\n") {
			t.Errorf("real consumer: %v\n%s", err, output)
			return 1
		}
		return 0
	}
	call := func(label string, fresh bool, wantRC int) {
		t.Helper()
		start := time.Now()
		args := []string{"run"}
		if fresh {
			args = append(args, "--update")
		}
		args = append(args, "--", "semver", "1.2.3")
		rc := runRun(args)
		t.Logf("%s rc=%d elapsed=%s resolve=%d install=%d consumers=%d", label, rc, time.Since(start), resolves.Load(), installs.Load(), consumerRuns)
		if rc != wantRC {
			t.Fatalf("%s status %d, want %d\n%s", label, rc, wantRC, log.String())
		}
	}
	call("cold", false, 0)
	first, err := readNPMCaptureRecord(store, in)
	if err != nil || first.Entry == "" || first.Completed != 1 || resolves.Load() != 1 || installs.Load() != 1 {
		t.Fatalf("cold not admitted: %+v %v", first, err)
	}
	if err := auditSealedSemverResolution(auditedSealedJSON(first.Resolution)); err != nil {
		t.Fatal(err)
	}
	t.Logf("runtime=%+v identity=%s entry=%s", first.Resolution.Runtime, first.Identity, first.Entry)
	entryRoot := store.EntryDir(first.Entry)
	manifest, err := capture.ReadManifest(entryRoot)
	if err != nil || manifest.RefScan != capture.RefScanFull || !manifest.Relocatable {
		t.Fatalf("capture witness: %+v %v", manifest, err)
	}
	gz, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		want, err := io.ReadAll(tr)
		if err != nil {
			t.Fatal(err)
		}
		file := filepath.Join(entryRoot, "tree", ".local/lib/yolo-npm", first.Identity, "node_modules/semver", strings.TrimPrefix(h.Name, "package/"))
		actual, err := os.ReadFile(file)
		if err != nil || !bytes.Equal(actual, want) {
			t.Fatalf("installed source differs from audited tarball: %s %v", h.Name, err)
		}
	}
	t.Logf("artifact manifest entries=%d bytes=%d scan=%s relocatable=%t; all 52 installed files match audited tarball", len(manifest.Entries), manifest.TotalBytes(), manifest.RefScan, manifest.Relocatable)
	call("warm", false, 0)
	if resolves.Load() != 1 || installs.Load() != 1 {
		t.Fatal("warm contacted producer")
	}
	call("fixed-fresh", true, 0)
	fixed, err := readNPMCaptureRecord(store, in)
	if err != nil || fixed.Identity != first.Identity || fixed.Entry != first.Entry || fixed.Completed != 2 || resolves.Load() != 2 || installs.Load() != 1 {
		t.Fatalf("fixed selection changed or reinstalled: %+v %v", fixed, err)
	}
	denyFresh.Store(true)
	call("fresh-refusal", true, 1)
	failed, err := readNPMCaptureRecord(store, in)
	if err != nil || failed.Completed != fixed.Completed || failed.Started <= failed.Completed || failed.Failure == "" || failed.Entry != fixed.Entry || installs.Load() != 1 || consumerRuns != 3 {
		t.Fatalf("fresh failure served staleGood or published: %+v %v", failed, err)
	}
	t.Log("AUDITED_SUCCESS cold/warm/fixed/fresh-refusal; no stale delivery or target on failed fresh check")
}

func auditedSealedJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

type auditedSealedLog struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *auditedSealedLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}
func (l *auditedSealedLog) String() string { l.mu.Lock(); defer l.mu.Unlock(); return l.b.String() }
