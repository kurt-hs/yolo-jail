package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

const (
	floorArmPath   = "/nix/store/c4xkimsmafy3as7qyq6nli4qx3m4xwvp-yolo-noncontainer-profile"
	floorIntelPath = "/nix/store/4ir7jys56ds80ph8sn5kg749km8l0y9w-yolo-noncontainer-profile"
)

var floorPathsEnv = "DARWIN_FLOOR_PATHS=" + floorArmPath + " " + floorIntelPath

// The request runs the darwin floor gate before the final preflight (exact-SHA
// CI, version order, tag identity), so that proof stays the last read before
// the tag write however long the floor wait or build took; a failure leaves no tag, a fork without Cachix skips with a
// line saying so, and an unset or malformed hand-off refuses rather than skips.
func TestRequestRunsTheDarwinFloorGateBeforeTheTag(t *testing.T) {
	root := repositoryRoot(t)
	script := filepath.Join(root, "tools", "release-wiring", "request.sh")
	for _, tc := range []struct {
		name      string
		extra     []string
		wantError string
		wantGate  bool
		wantOut   string
	}{
		{name: "cached profiles let the tag through", extra: []string{"CACHIX_CONFIGURED=true", floorPathsEnv}, wantGate: true},
		{name: "a floor build that does not land fails before the tag", extra: []string{"CACHIX_CONFIGURED=true", floorPathsEnv, "FAIL_FLOOR_GATE=1"}, wantGate: true,
			wantError: "request this release again"},
		{name: "a fork without Cachix skips and says so", extra: []string{"CACHIX_CONFIGURED=false"}, wantOut: "skipping the darwin floor cache gate"},
		{name: "an unset Cachix flag refuses", extra: []string{"CACHIX_CONFIGURED="}, wantError: "CACHIX_CONFIGURED must be true or false"},
		{name: "a missing path hand-off refuses", extra: []string{"CACHIX_CONFIGURED=true", "DARWIN_FLOOR_PATHS=" + floorArmPath}, wantError: "Expected the two darwin floor profile paths"},
		{name: "an unsafe path refuses", extra: []string{"CACHIX_CONFIGURED=true", "DARWIN_FLOOR_PATHS=" + floorArmPath + " /tmp/evil"}, wantError: "invalid store path"},
		{name: "a bad cache name refuses", extra: []string{"CACHIX_CONFIGURED=true", floorPathsEnv, "CACHIX_CACHE=Bad/Name"}, wantError: "Invalid Cachix cache name"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bin, trace := fakeCommands(t)
			out, err := runScript(t, root, script, bin, trace,
				append([]string{"RELEASE_VERSION=9.8.7", "RELEASE_SHA=" + testSHA, "GITHUB_RUN_ID=101", "RELEASE_WAIT_SECONDS=1"}, tc.extra...)...)
			lines := readTrace(t, trace)
			ciGate, floorGate, tag := traceIndex(lines, "tools/release-gate --repo"), traceIndex(lines, "tools/release-gate darwin-floor"), traceIndex(lines, "git/tags")
			if (floorGate >= 0) != tc.wantGate {
				t.Fatalf("floor gate called=%v, want %v: %v", floorGate >= 0, tc.wantGate, lines)
			}
			if tc.wantError != "" {
				if err == nil || !strings.Contains(out, tc.wantError) || !strings.Contains(out, "No tag was created") {
					t.Fatalf("request did not refuse with the next step: err=%v output=%s", err, out)
				}
				if tag >= 0 || traceIndex(lines, "workflow run") >= 0 {
					t.Fatalf("a floor failure still wrote a tag or dispatched a release: %v", lines)
				}
				return
			}
			if err != nil {
				t.Fatalf("request failed: %v output=%s", err, out)
			}
			if tc.wantOut != "" && !strings.Contains(out, tc.wantOut) {
				t.Fatalf("output lacks %q: %s", tc.wantOut, out)
			}
			if tag < 0 || ciGate < 0 || (tc.wantGate && !(floorGate < ciGate && ciGate < tag)) {
				t.Fatalf("want the floor gate, then the CI/preflight gate, then the tag: %v", lines)
			}
			if tc.wantGate {
				call := lines[floorGate]
				for _, want := range []string{"--repo owner/repo", "--cache yolo-jail", "--path " + floorArmPath, "--path " + floorIntelPath, "--timeout 180m", "--sha " + testSHA} {
					if !strings.Contains(call, want) {
						t.Fatalf("floor gate call %q lacks %q", call, want)
					}
				}
			}
		})
	}
}

// The wait list is every main commit whose flake inputs equal the target's:
// from the last flake change at or before it to just before the next one.
// Real Git, so the history walk cannot be stubbed into agreeing.
func TestDarwinFloorGateWaitsOnlyForRunsOfTheTargetsFlakeInputs(t *testing.T) {
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		c := exec.Command(realGit, args...)
		c.Dir = root
		out, e := c.CombinedOutput()
		if e != nil {
			t.Fatalf("git %v: %v: %s", args, e, out)
		}
		return strings.TrimSpace(string(out))
	}
	commit := func(file, content string) string {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, file), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		git("add", file)
		git("commit", "-q", "-m", file+" "+content)
		return git("rev-parse", "HEAD")
	}
	git("init", "-q", "-b", "main")
	git("config", "user.email", "fixture@example.invalid")
	git("config", "user.name", "fixture")
	c0 := commit("flake.nix", "v0")
	flake := commit("flake.lock", "v1")
	c2 := commit("README", "a")
	release := commit("README", "b")
	c4 := commit("README", "c")
	nextFlake := commit("flake.nix", "v2")
	c6 := commit("README", "d")
	origin := filepath.Join(t.TempDir(), "origin.git")
	git("clone", "-q", "--bare", root, origin)
	git("remote", "add", "origin", origin)

	bin, trace := fakeCommands(t)
	if err := os.Remove(filepath.Join(bin, "git")); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(repositoryRoot(t), "tools", "release-wiring", "darwin-floor-gate.sh")
	for _, tc := range []struct {
		name, sha string
		want      []string
		notWant   []string
	}{
		{"between two flake changes", release, []string{flake, c2, release, c4}, []string{c0, nextFlake, c6}},
		{"the flake change itself", flake, []string{flake, c2, release, c4}, []string{c0, nextFlake, c6}},
		{"after the last flake change", c6, []string{nextFlake, c6}, []string{c0, flake, release, c4}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_ = os.Remove(trace)
			cmd := exec.Command("bash", script)
			cmd.Dir = root
			cmd.Env = commandEnv(bin, trace, "RELEASE_SHA="+tc.sha, "CACHIX_CONFIGURED=true", floorPathsEnv, "RUNNER_TEMP="+t.TempDir())
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("gate failed: %v: %s", err, out)
			}
			lines := readTrace(t, trace)
			call := lines[traceIndex(lines, "tools/release-gate darwin-floor")]
			// The gate dispatches a build of --sha itself, so it needs no
			// later flake change to name a branch dispatch.
			if !strings.Contains(call, "--sha "+tc.sha) || strings.Contains(call, "--later-flake-change") {
				t.Errorf("gate call %q, want --sha %s and no --later-flake-change", call, tc.sha)
			}
			i := traceIndex(lines, "wait-shas:")
			if i < 0 {
				t.Fatalf("the gate passed no wait list: %v", lines)
			}
			for _, sha := range tc.want {
				if !strings.Contains(lines[i], sha) {
					t.Errorf("wait list %q lacks %s", lines[i], sha)
				}
			}
			for _, sha := range tc.notWant {
				if strings.Contains(lines[i], sha) {
					t.Errorf("wait list %q includes %s, whose flake inputs differ", lines[i], sha)
				}
			}
		})
	}
}

// The path computation evaluates the target flake exactly as a macos-user
// launch builds it, with no credential and no YOLO_EXTRA_PACKAGES, and hands
// over only validated store paths.
func TestDarwinFloorPathsEvaluatesTheFloorWithoutCredentials(t *testing.T) {
	root := repositoryRoot(t)
	script := filepath.Join(root, "tools", "release-wiring", "darwin-floor-paths.sh")
	target := t.TempDir()
	for _, tc := range []struct {
		name, nixOut, wantError string
		extra                   []string
	}{
		{name: "valid paths reach the job output"},
		{name: "an invalid evaluation refuses", nixOut: "/tmp/not-a-store-path", wantError: "invalid store path"},
		{name: "a failed evaluation refuses", nixOut: "FAIL", wantError: "Could not evaluate"},
		{name: "a wrong checkout refuses", extra: []string{"TARGET_SHA=" + strings.Repeat("f", 40)}, wantError: "does not match frozen SHA"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bin, trace := fakeCommands(t)
			nix := `echo "nix $* cwd=$(pwd) token=${GITHUB_TOKEN:-} gh=${GH_TOKEN:-} cachix=${CACHIX_AUTH_TOKEN:-} extra=${YOLO_EXTRA_PACKAGES:-}" >> "$TRACE"
case "${FAKE_NIX_OUT:-}" in
  FAIL) exit 1 ;;
  "") case "$*" in *aarch64-darwin*) printf %s ` + floorArmPath + ` ;; *) printf %s ` + floorIntelPath + ` ;; esac ;;
  *) printf %s "$FAKE_NIX_OUT" ;;
esac
`
			if err := os.WriteFile(filepath.Join(bin, "nix"), []byte("#!/bin/sh\n"+nix), 0o755); err != nil {
				t.Fatal(err)
			}
			output := filepath.Join(t.TempDir(), "github-output")
			out, err := runScript(t, root, script, bin, trace, append([]string{
				"RELEASE_SHA=" + testSHA, "TARGET_ROOT=" + target, "GITHUB_OUTPUT=" + output, "FAKE_NIX_OUT=" + tc.nixOut,
				"GITHUB_TOKEN=write-secret", "GH_TOKEN=write-secret", "CACHIX_AUTH_TOKEN=write-secret", `YOLO_EXTRA_PACKAGES=["x"]`,
			}, tc.extra...)...)
			if tc.wantError != "" {
				if err == nil || !strings.Contains(out, tc.wantError) || !strings.Contains(strings.ToLower(out), "no tag was created") {
					t.Fatalf("err=%v output=%s", err, out)
				}
				if b, _ := os.ReadFile(output); len(b) != 0 {
					t.Fatalf("a refusal still wrote a job output: %s", b)
				}
				return
			}
			if err != nil {
				t.Fatalf("err=%v output=%s", err, out)
			}
			got, _ := os.ReadFile(output)
			if strings.TrimSpace(string(got)) != "paths="+floorArmPath+" "+floorIntelPath {
				t.Fatalf("job output = %q", got)
			}
			lines := readTrace(t, trace)
			for _, system := range []string{"aarch64-darwin", "x86_64-darwin"} {
				i := traceIndex(lines, ".#packages."+system+".yoloNoncontainerProfile.outPath")
				if i < 0 {
					t.Fatalf("no %s evaluation: %v", system, lines)
				}
				call := lines[i]
				for _, want := range []string{"--accept-flake-config", "eval --impure --raw", "cwd=" + target, "token= gh= cachix= extra="} {
					if !strings.Contains(call, want) {
						t.Fatalf("evaluation %q lacks %q", call, want)
					}
				}
				if strings.Contains(call, "write-secret") || strings.Contains(call, " build ") {
					t.Fatalf("evaluation %q saw a credential or builds", call)
				}
			}
		})
	}
}

// The gate dispatches darwin-floor-cache.yml from main with -f sha=<target>
// and finds its run by title (PTG-D5), so the workflow must take that input,
// build exactly that commit, title the run with it in the spelling
// release-gate matches, and refuse a commit that is not on main before any
// step that could run it.
func TestDarwinFloorCacheBuildsTheDispatchedCommitAndTitlesItsRun(t *testing.T) {
	workflow := readWorkflow(t, ".github/workflows/darwin-floor-cache.yml")
	prefix := regexp.MustCompile(`floorRunTitlePrefix = "([^"]+)"`).FindSubmatch(mustRead(t, "tools/release-gate/darwinfloor.go"))
	if prefix == nil {
		t.Fatal("cannot find floorRunTitlePrefix in tools/release-gate/darwinfloor.go")
	}
	if want := string(prefix[1]) + "${{ inputs.sha || github.sha }}"; workflow.RunName != want {
		t.Fatalf("run-name = %q, want %q (the title release-gate finds its run by)", workflow.RunName, want)
	}
	var dispatch struct {
		Inputs map[string]struct {
			Type     string `yaml:"type"`
			Required bool   `yaml:"required"`
		} `yaml:"inputs"`
	}
	node := workflow.On["workflow_dispatch"]
	if err := node.Decode(&dispatch); err != nil || dispatch.Inputs["sha"].Type != "string" || dispatch.Inputs["sha"].Required {
		t.Fatalf("workflow_dispatch inputs = %+v (%v), want an optional string sha", dispatch.Inputs, err)
	}
	if _, ok := workflow.On["push"]; !ok {
		t.Fatal("the push trigger, which builds every flake change on main, is gone")
	}
	steps := workflow.Jobs["push-floor"].Steps
	checkout, onMain, nix := -1, -1, -1
	for i, step := range steps {
		switch {
		case strings.HasPrefix(step.Uses, "actions/checkout@"):
			checkout = i
			if step.With["ref"] != "${{ inputs.sha || github.sha }}" || step.With["fetch-depth"] != "0" {
				t.Fatalf("checkout with = %v, want the sha input (else the event's commit) and full history", step.With)
			}
		case strings.Contains(step.Run, `merge-base --is-ancestor "$SHA" refs/remotes/origin/main`):
			onMain = i
			if step.Env["SHA"] != "${{ inputs.sha }}" {
				t.Fatalf("the main-ancestry check reads SHA from %q", step.Env["SHA"])
			}
		case strings.HasPrefix(step.Uses, "cachix/install-nix-action@"):
			nix = i
		}
		if strings.Contains(step.Run, "${{ inputs.sha") {
			t.Fatalf("step %q expands the sha input into its script; pass it through env", step.Name)
		}
	}
	if checkout < 0 || onMain < 0 || nix < 0 || !(checkout < onMain && onMain < nix) {
		t.Fatalf("want checkout, then the main-ancestry check, then Nix; got indexes %d, %d, %d", checkout, onMain, nix)
	}
}

// The request job dispatches the floor build with its own token, so it needs
// actions: write, and its timeout cannot pass GitHub-hosted runners' ceiling.
func TestRequestJobCanDispatchTheFloorBuild(t *testing.T) {
	job := readWorkflow(t, ".github/workflows/release-request.yml").Jobs["create-tag-and-dispatch"]
	if job.Permissions["actions"] != "write" {
		t.Fatalf("create-tag-and-dispatch permissions = %v, want actions: write", job.Permissions)
	}
	if job.Timeout > 360 {
		t.Fatalf("create-tag-and-dispatch timeout-minutes = %d, over the 360-minute job ceiling", job.Timeout)
	}
}

// The workflow wires the read-only job's paths, and the Cachix configuration
// flag (never the token), into the write job that runs the gate.
func TestReleaseRequestHandsTheFloorPathsToTheWriteJob(t *testing.T) {
	workflow := readWorkflow(t, ".github/workflows/release-request.yml")
	step := namedStep(t, workflow, "prepare-target", "Compute the darwin floor profile paths")
	if step.Run != "tools/release-wiring/darwin-floor-paths.sh" || step.Env["TARGET_ROOT"] != "target" {
		t.Fatalf("path computation caller = %q env=%v", step.Run, step.Env)
	}
	if hasAnyWritePermission(workflow.Jobs["prepare-target"].Permissions) {
		t.Fatal("the job evaluating target Nix has write scope")
	}
	text := string(mustRead(t, ".github/workflows/release-request.yml"))
	if !strings.Contains(text, "darwin-floor-paths: ${{ steps.darwin-floor.outputs.paths }}") {
		t.Fatal("prepare-target does not export the floor paths")
	}
	for _, s := range workflow.Jobs["create-tag-and-dispatch"].Steps {
		if s.Run != "tools/release-wiring/request.sh" {
			continue
		}
		if s.Env["DARWIN_FLOOR_PATHS"] != "${{ needs.prepare-target.outputs.darwin-floor-paths }}" ||
			s.Env["CACHIX_CONFIGURED"] != "${{ secrets.CACHIX_AUTH_TOKEN != '' }}" ||
			s.Env["CACHIX_CACHE"] != "${{ vars.CACHIX_CACHE || 'yolo-jail' }}" {
			t.Fatalf("request step env = %v", s.Env)
		}
		for key, value := range s.Env {
			if strings.Contains(value, "secrets.") && key != "CACHIX_CONFIGURED" {
				t.Fatalf("request step passes a secret value in %s", key)
			}
		}
		return
	}
	t.Fatal("create-tag-and-dispatch has no request.sh step")
}
