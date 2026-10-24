package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	floorArm   = "/nix/store/c4xkimsmafy3as7qyq6nli4qx3m4xwvp-yolo-noncontainer-profile"
	floorIntel = "/nix/store/4ir7jys56ds80ph8sn5kg749km8l0y9w-yolo-noncontainer-profile"
	flakeSHA   = "fedcba9876543210fedcba9876543210fedcba98"
	otherSHA   = "1111111111111111111111111111111111111111"
)

// fakeFloor stubs both the Cachix narinfo endpoint and the Actions API.
type fakeFloor struct {
	mu sync.Mutex
	// present maps a store path to the poll count (cache checks of that path)
	// after which the cache serves it; 0 means from the start, absent never.
	present       map[string]int
	checks        map[string]int
	narinfoPath   map[string]string // override the StorePath a narinfo names
	cacheFailures int               // 503s before the cache answers
	cacheStatus   int               // a fixed non-200/404 answer
	noWorkflow    bool
	runs          []run
	runsCalls     int
	server        *httptest.Server
}

func newFakeFloor(t *testing.T) *fakeFloor {
	t.Helper()
	f := &fakeFloor{present: map[string]int{}, checks: map[string]int{}, narinfoPath: map[string]string{}}
	f.server = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.server.Close)
	return f
}

func (f *fakeFloor) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case strings.HasPrefix(r.URL.Path, "/cache/") && strings.HasSuffix(r.URL.Path, ".narinfo"):
		if f.cacheFailures > 0 {
			f.cacheFailures--
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		if f.cacheStatus != 0 {
			w.WriteHeader(f.cacheStatus)
			return
		}
		hash := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/cache/"), ".narinfo")
		for path, after := range f.present {
			if !strings.Contains(path, "/"+hash+"-") {
				continue
			}
			f.checks[path]++
			if f.checks[path] > after {
				named := path
				if override, ok := f.narinfoPath[path]; ok {
					named = override
				}
				fmt.Fprintf(w, "StorePath: %s\nURL: nar/x.nar.zst\nCompression: zstd\n", named)
				return
			}
		}
		http.NotFound(w, r)
	case r.URL.Path == "/repos/owner/project/actions/workflows/"+floorWorkflowFile:
		if f.noWorkflow {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(workflow{ID: 77, Path: floorWorkflowPath})
	case r.URL.Path == "/repos/owner/project/actions/workflows/77/runs":
		f.runsCalls++
		if r.URL.Query().Get("branch") != "main" {
			http.Error(w, "branch filter missing", http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(runList{TotalCount: len(f.runs), WorkflowRuns: f.runs})
	default:
		http.Error(w, "unexpected "+r.URL.Path, http.StatusTeapot)
	}
}

func floorRun(id int64, sha, status, conclusion string) run {
	return run{
		ID: id, RunNumber: id, RunAttempt: 1, WorkflowID: 77, Path: floorWorkflowPath, HeadSHA: sha,
		HeadBranch: "main", Event: "push", Status: status, Conclusion: conclusion,
		HTMLURL:    fmt.Sprintf("https://github.com/owner/project/actions/runs/%d", id),
		Repository: repository{FullName: "owner/project"}, HeadRepository: repository{FullName: "owner/project"},
	}
}

func newTestFloorGate(t *testing.T, f *fakeFloor) (*floorGate, *fakeClock) {
	t.Helper()
	clock := &fakeClock{current: time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC)}
	return &floorGate{
		api: gate{
			repo: "owner/project", client: f.server.Client(), baseURL: f.server.URL,
			now: clock.now, sleep: clock.sleep, timeout: 10 * time.Minute, pollEvery: 30 * time.Second, requestMax: time.Second,
		},
		cache: "yolo-jail", cacheURL: f.server.URL + "/cache", paths: []string{floorArm, floorIntel},
		waitSHAs: map[string]bool{testSHA: true, flakeSHA: true},
	}, clock
}

func TestFloorGatePassesWhenBothProfilesAreCached(t *testing.T) {
	f := newFakeFloor(t)
	f.present[floorArm], f.present[floorIntel] = 0, 0
	g, clock := newTestFloorGate(t, f)
	proof, err := g.wait(context.Background())
	if err != nil {
		t.Fatalf("wait: %v", err)
	}
	if proof.Cache != "yolo-jail" || len(proof.Paths) != 2 || clock.sleeps != 0 || f.runsCalls != 0 {
		t.Fatalf("proof=%+v sleeps=%d runsCalls=%d; want an immediate pass without asking Actions", proof, clock.sleeps, f.runsCalls)
	}
}

func TestFloorGateWaitsForAPendingRunForTheFlakeCommit(t *testing.T) {
	for _, sha := range []string{testSHA, flakeSHA} {
		t.Run(sha[:7], func(t *testing.T) {
			f := newFakeFloor(t)
			f.present[floorArm] = 0
			f.present[floorIntel] = 3 // pushed after three checks
			f.runs = []run{floorRun(5, sha, "in_progress", "")}
			g, clock := newTestFloorGate(t, f)
			if _, err := g.wait(context.Background()); err != nil {
				t.Fatalf("wait: %v", err)
			}
			if clock.sleeps != 3 {
				t.Fatalf("slept %d times, want 3 polls while the run pushed", clock.sleeps)
			}
		})
	}
}

func TestFloorGateRefusesAMissWithNoPendingRunAndNamesTheDispatch(t *testing.T) {
	for _, tc := range []struct {
		name string
		set  func(*fakeFloor)
		want string
	}{
		{"no runs at all", func(*fakeFloor) {}, ""},
		{"a pending run for another flake", func(f *fakeFloor) { f.runs = []run{floorRun(6, otherSHA, "in_progress", "")} }, ""},
		{"the relevant run failed", func(f *fakeFloor) { f.runs = []run{floorRun(7, flakeSHA, "completed", "failure")} }, `concluded "failure"`},
		{"the workflow is not on the default branch", func(f *fakeFloor) { f.noWorkflow = true }, ""},
		{"a fork's run with the same SHA", func(f *fakeFloor) {
			r := floorRun(8, testSHA, "in_progress", "")
			r.HeadRepository.FullName = "fork/project"
			f.runs = []run{r}
		}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeFloor(t)
			f.present[floorArm] = 0
			tc.set(f)
			g, clock := newTestFloorGate(t, f)
			_, err := g.wait(context.Background())
			var refusal floorRefusal
			if !errors.As(err, &refusal) {
				t.Fatalf("err = %v, want a refusal", err)
			}
			msg := err.Error()
			if !strings.Contains(msg, "gh workflow run darwin-floor-cache.yml --repo owner/project --ref main") ||
				!strings.Contains(msg, floorIntel) || strings.Contains(msg, floorArm) || clock.sleeps != 0 {
				t.Fatalf("refusal = %q (sleeps %d); want the missing path, no wait, and the dispatch step", msg, clock.sleeps)
			}
			if tc.want != "" && !strings.Contains(msg, tc.want) {
				t.Fatalf("refusal = %q, want %q", msg, tc.want)
			}
		})
	}
}

func TestFloorGateTimesOutOnARunThatNeverPushes(t *testing.T) {
	f := newFakeFloor(t)
	f.runs = []run{floorRun(9, testSHA, "queued", "")}
	g, _ := newTestFloorGate(t, f)
	_, err := g.wait(context.Background())
	if err == nil || !strings.Contains(err.Error(), "timed out") || !strings.Contains(err.Error(), "gh run view 9 --repo owner/project") {
		t.Fatalf("err = %v, want a timeout naming the run to inspect", err)
	}
}

func TestFloorGateRejectsANarinfoForAnotherPath(t *testing.T) {
	f := newFakeFloor(t)
	f.present[floorArm], f.present[floorIntel] = 0, 0
	f.narinfoPath[floorIntel] = "/nix/store/4ir7jys56ds80ph8sn5kg749km8l0y9w-something-else"
	g, _ := newTestFloorGate(t, f)
	if _, err := g.wait(context.Background()); err == nil || !strings.Contains(err.Error(), "names a different store path") {
		t.Fatalf("err = %v, want a mismatched narinfo refused", err)
	}
}

func TestFloorGateRetriesATransientCacheFailure(t *testing.T) {
	f := newFakeFloor(t)
	f.present[floorArm], f.present[floorIntel] = 0, 0
	f.cacheFailures = 2
	g, _ := newTestFloorGate(t, f)
	if _, err := g.wait(context.Background()); err != nil {
		t.Fatalf("wait: %v", err)
	}
}

func TestFloorGateNamesAPrivateCacheAnswer(t *testing.T) {
	f := newFakeFloor(t)
	f.cacheStatus = http.StatusUnauthorized
	g, _ := newTestFloorGate(t, f)
	if _, err := g.wait(context.Background()); err == nil || !strings.Contains(err.Error(), "HTTP 401") || !strings.Contains(err.Error(), "public") {
		t.Fatalf("err = %v, want the 401 explained", err)
	}
}

func TestParseWaitSHAsRefusesBadInput(t *testing.T) {
	for _, input := range []string{"", "\n\n", "not-a-sha\n", testSHA + "\n$(touch x)\n"} {
		if _, err := parseWaitSHAs(strings.NewReader(input)); err == nil {
			t.Errorf("parseWaitSHAs(%q) accepted bad input", input)
		}
	}
	got, err := parseWaitSHAs(strings.NewReader(testSHA + "\n\n" + strings.ToUpper(flakeSHA) + "\n"))
	if err != nil || !got[testSHA] || !got[flakeSHA] || len(got) != 2 {
		t.Fatalf("parseWaitSHAs = %v, %v", got, err)
	}
}

func floorCLIArgs(t *testing.T, extra ...string) []string {
	t.Helper()
	waitFile := filepath.Join(t.TempDir(), "wait")
	if err := os.WriteFile(waitFile, []byte(testSHA+"\n"+flakeSHA+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return append([]string{"--repo", "owner/project", "--cache", "yolo-jail", "--path", floorArm, "--path", floorIntel,
		"--wait-shas", waitFile, "--timeout", "1m", "--poll-interval", "1s"}, extra...)
}

func TestFloorCLIRefusesInvalidArguments(t *testing.T) {
	for _, args := range [][]string{
		{},
		{"--repo", "owner/project", "--cache", "yolo-jail", "--wait-shas", "x"},
		{"--repo", "owner/project", "--cache", "Bad Name", "--path", floorArm, "--wait-shas", "x"},
		{"--repo", "owner/project", "--cache", "yolo-jail", "--path", "/tmp/evil", "--wait-shas", "x"},
		{"--repo", "owner/project", "--cache", "yolo-jail", "--path", floorArm, "--wait-shas", "/nonexistent/list"},
	} {
		var stderr bytes.Buffer
		if code := runFloorCLI(args, &bytes.Buffer{}, &stderr, nil); code != 2 {
			t.Errorf("runFloorCLI(%q) = %d, want 2; stderr=%s", args, code, stderr.String())
		}
	}
}

func TestFloorCLIExitsOneWithTheNextStepOnAMiss(t *testing.T) {
	f := newFakeFloor(t)
	var stdout, stderr bytes.Buffer
	code := runFloorCLI(floorCLIArgs(t), &stdout, &stderr, func(g *floorGate) {
		g.api.client, g.api.baseURL, g.cacheURL = f.server.Client(), f.server.URL, f.server.URL+"/cache"
	})
	if code != 1 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "Next step: gh workflow run darwin-floor-cache.yml --repo owner/project --ref main") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestFloorCLIPrintsProofOnAHit(t *testing.T) {
	f := newFakeFloor(t)
	f.present[floorArm], f.present[floorIntel] = 0, 0
	var stdout, stderr bytes.Buffer
	code := runFloorCLI(floorCLIArgs(t), &stdout, &stderr, func(g *floorGate) {
		g.api.client, g.api.baseURL, g.cacheURL = f.server.Client(), f.server.URL, f.server.URL+"/cache"
	})
	var proof floorProof
	if code != 0 || json.Unmarshal(stdout.Bytes(), &proof) != nil || len(proof.Paths) != 2 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestCLIDispatchesTheDarwinFloorSubcommand(t *testing.T) {
	var stderr bytes.Buffer
	if code := runCLI([]string{"darwin-floor", "--cache", "x"}, &bytes.Buffer{}, &stderr); code != 2 || !strings.Contains(stderr.String(), "release-gate darwin-floor") {
		t.Fatalf("code=%d stderr=%q; want the darwin-floor argument refusal", code, stderr.String())
	}
}
