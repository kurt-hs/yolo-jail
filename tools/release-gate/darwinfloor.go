package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// The darwin floor gate: before a release is tagged, both macOS floor profiles
// for the target commit must already be in the project's Cachix cache, or every
// macos-user user of the release compiles the rustls mise on first launch. The
// caller computes the store paths from the target flake (a nix eval, no build)
// and the set of main commits whose flake inputs equal the target's; this code
// asks the cache and, while a darwin-floor-cache.yml run for one of those
// commits is still going, waits for it. It never dispatches a workflow.

const (
	floorWorkflowFile = "darwin-floor-cache.yml"
	floorWorkflowPath = ".github/workflows/" + floorWorkflowFile
	floorRunPages     = 3
	maxWaitSHAs       = 5000
	maxNarinfoBytes   = 64 << 10
)

var (
	storePathPattern = regexp.MustCompile(`^/nix/store/([0-9a-df-np-sv-z]{32})-[A-Za-z0-9+._?=-]+$`)
	cacheNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)
)

type floorGate struct {
	api      gate // Actions API access: repo, token, baseURL, client, clock
	cache    string
	cacheURL string
	paths    []string
	waitSHAs map[string]bool
	// targetSHA is the commit being released, and laterFlakeChange, when set,
	// the first main commit after it that changes the flake inputs. With one,
	// a dispatch on main builds a different flake, so the next step names a
	// dispatch on a ref at the target instead.
	targetSHA        string
	laterFlakeChange string
	// progress receives one line per wait, so a log shows which run it is on.
	progress io.Writer
}

type floorProof struct {
	Cache string   `json:"cache"`
	Paths []string `json:"paths"`
}

// floorRefusal is a final answer the release request must stop on; its text
// names the next step.
type floorRefusal struct{ msg string }

func (r floorRefusal) Error() string { return r.msg }

func (f *floorGate) dispatchStep() string {
	if f.laterFlakeChange == "" {
		return fmt.Sprintf("gh workflow run %s --repo %s --ref main", floorWorkflowFile, f.api.repo)
	}
	short := f.targetSHA
	if len(short) > 12 {
		short = short[:12]
	}
	branch := "darwin-floor-" + short
	return fmt.Sprintf("main has changed the flake inputs since %s (at %s), so a dispatch on main builds a different floor; "+
		"push a branch at the target and dispatch there: git push origin %s:refs/heads/%s && gh workflow run %s --repo %s --ref %s "+
		"(delete the branch once it has pushed)",
		f.targetSHA, f.laterFlakeChange, f.targetSHA, branch, floorWorkflowFile, f.api.repo, branch)
}

func (f *floorGate) wait(parent context.Context) (floorProof, error) {
	g := f.api
	if g.client == nil || g.now == nil || g.sleep == nil || g.timeout <= 0 || g.pollEvery <= 0 || g.requestMax <= 0 {
		return floorProof{}, errors.New("darwin floor gate is missing valid runtime configuration")
	}
	if len(f.paths) == 0 {
		return floorProof{}, errors.New("darwin floor gate was given no store paths")
	}
	ctx, cancel := context.WithTimeout(parent, g.timeout)
	defer cancel()
	deadline := g.now().Add(g.timeout)
	g.deadline = deadline
	g.client = safeHTTPClient(g.client)

	var watched *run
	for {
		if err := g.checkDeadline(ctx, deadline); err != nil {
			return floorProof{}, f.timedOut(watched)
		}
		pending, latest, err := f.relevantRuns(ctx, &g)
		if err != nil {
			if g.checkDeadline(ctx, deadline) != nil {
				return floorProof{}, f.timedOut(watched)
			}
			return floorProof{}, err
		}
		missing, err := f.missingPaths(ctx, &g)
		if err != nil {
			if g.checkDeadline(ctx, deadline) != nil {
				return floorProof{}, f.timedOut(watched)
			}
			return floorProof{}, err
		}
		if pending == nil && len(missing) > 0 {
			// The run may have finished its push between the two reads above:
			// read the cache once more before refusing.
			if missing, err = f.missingPaths(ctx, &g); err != nil {
				if g.checkDeadline(ctx, deadline) != nil {
					return floorProof{}, f.timedOut(watched)
				}
				return floorProof{}, err
			}
		}
		if pending == nil && len(missing) == 0 {
			return floorProof{Cache: f.cache, Paths: append([]string(nil), f.paths...)}, nil
		}
		if pending == nil {
			msg := fmt.Sprintf("darwin floor profile(s) %s are not in https://%s.cachix.org, and no %s run for this commit's flake inputs is queued or running",
				strings.Join(missing, ", "), f.cache, floorWorkflowFile)
			if latest != nil {
				msg += fmt.Sprintf(" (the newest one, %s, concluded %q)", latest.HTMLURL, latest.Conclusion)
			}
			return floorProof{}, floorRefusal{msg + fmt.Sprintf(". Next step: %s; when it has pushed both profiles, request this release again", f.dispatchStep())}
		}
		watched = pending
		if f.progress != nil {
			if len(missing) == 0 {
				fmt.Fprintf(f.progress, "The floor profiles are cached; waiting for %s run %d (%s) to finish its push\n", floorWorkflowFile, pending.ID, pending.Status)
			} else {
				fmt.Fprintf(f.progress, "Waiting for %s run %d (%s) to push %s\n", floorWorkflowFile, pending.ID, pending.Status, strings.Join(missing, ", "))
			}
		}
		remaining := deadline.Sub(g.now())
		pause := g.pollEvery
		if remaining < pause {
			pause = remaining
		}
		if pause <= 0 {
			return floorProof{}, f.timedOut(watched)
		}
		if err := g.sleep(ctx, pause); err != nil {
			if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return floorProof{}, f.timedOut(watched)
			}
			return floorProof{}, fmt.Errorf("waiting for the darwin floor cache was interrupted: %w", err)
		}
	}
}

func (f *floorGate) timedOut(watched *run) error {
	if watched == nil {
		return floorRefusal{fmt.Sprintf("timed out waiting for the darwin floor profiles in https://%s.cachix.org. Next step: %s, then request this release again", f.cache, f.dispatchStep())}
	}
	return floorRefusal{fmt.Sprintf("timed out waiting for %s run %d to push the darwin floor profiles. Next step: inspect it with gh run view %d --repo %s; once it has pushed them, request this release again (or %s if it failed)",
		floorWorkflowFile, watched.ID, watched.ID, f.api.repo, f.dispatchStep())}
}

// missingPaths returns the store paths the cache does not serve: each profile
// whose narinfo is absent, and, for a profile that is present, each path its
// narinfo's References line names that is absent (the floor's own packages,
// mise among them). Each path is asked once per call.
func (f *floorGate) missingPaths(ctx context.Context, g *gate) ([]string, error) {
	var missing []string
	seen := map[string]bool{}
	check := func(path string) ([]string, error) {
		if seen[path] {
			return nil, nil
		}
		seen[path] = true
		hit, refs, err := f.narinfoHit(ctx, g, path)
		if err != nil {
			return nil, err
		}
		if !hit {
			missing = append(missing, path)
		}
		return refs, nil
	}
	for _, path := range f.paths {
		refs, err := check(path)
		if err != nil {
			return nil, err
		}
		for _, ref := range refs {
			if _, err := check(ref); err != nil {
				return nil, err
			}
		}
	}
	return missing, nil
}

// narinfoHit reports whether the cache serves path's narinfo and, when it
// does, the other store paths that narinfo's References line names.
func (f *floorGate) narinfoHit(ctx context.Context, g *gate, path string) (bool, []string, error) {
	m := storePathPattern.FindStringSubmatch(path)
	if m == nil {
		return false, nil, fmt.Errorf("invalid store path %q", path)
	}
	endpoint := strings.TrimRight(f.cacheURL, "/") + "/" + m[1] + ".narinfo"
	for attempt := 0; attempt <= 3; attempt++ {
		if err := g.checkDeadline(ctx, g.deadline); err != nil {
			return false, nil, err
		}
		requestContext, cancel := context.WithTimeout(ctx, g.requestMax)
		req, err := http.NewRequestWithContext(requestContext, http.MethodGet, endpoint, nil)
		if err != nil {
			cancel()
			return false, nil, errors.New("could not construct the Cachix narinfo request")
		}
		response, err := g.client.Do(req)
		if err != nil {
			cancel()
			return false, nil, fmt.Errorf("the Cachix narinfo request for %s failed; check https://%s.cachix.org is reachable and request again", path, f.cache)
		}
		switch {
		case response.StatusCode == http.StatusNotFound:
			response.Body.Close()
			cancel()
			return false, nil, nil
		case response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500:
			delay := retryDelay(response.Header.Get("Retry-After"), attempt, g.now())
			response.Body.Close()
			cancel()
			if attempt == 3 {
				return false, nil, fmt.Errorf("the Cachix cache remained unavailable (HTTP %d) for %s; request this release again once https://%s.cachix.org answers", response.StatusCode, path, f.cache)
			}
			if remaining := g.deadline.Sub(g.now()); delay > remaining {
				delay = remaining
			}
			if err := g.sleep(ctx, delay); err != nil {
				return false, nil, errors.New("the Cachix narinfo retry was interrupted")
			}
			continue
		case response.StatusCode != http.StatusOK:
			response.Body.Close()
			cancel()
			return false, nil, fmt.Errorf("the Cachix cache returned HTTP %d for %s; this gate reads only a public cache, so check https://%s.cachix.org is public and is the one the CACHIX_CACHE variable names, then request again", response.StatusCode, path, f.cache)
		}
		body, err := io.ReadAll(io.LimitReader(response.Body, maxNarinfoBytes+1))
		response.Body.Close()
		cancel()
		if err != nil || len(body) > maxNarinfoBytes {
			return false, nil, fmt.Errorf("could not read the Cachix narinfo for %s; request this release again", path)
		}
		if !narinfoNames(body, path) {
			return false, nil, fmt.Errorf("the Cachix narinfo for %s names a different store path; inspect https://%s.cachix.org before releasing", path, f.cache)
		}
		refs, err := narinfoReferences(body, path)
		if err != nil {
			return false, nil, fmt.Errorf("the Cachix narinfo for %s %s; inspect https://%s.cachix.org before releasing", path, err, f.cache)
		}
		return true, refs, nil
	}
	return false, nil, errors.New("the Cachix narinfo retry limit was reached")
}

// narinfoReferences returns the store paths a narinfo's References line names,
// without path itself. Each entry is a store path's base name.
func narinfoReferences(body []byte, path string) ([]string, error) {
	scanner := bufio.NewScanner(strings.NewReader(string(body)))
	for scanner.Scan() {
		key, value, ok := strings.Cut(scanner.Text(), ":")
		if !ok || key != "References" {
			continue
		}
		var refs []string
		for _, name := range strings.Fields(value) {
			ref := "/nix/store/" + name
			if !storePathPattern.MatchString(ref) {
				return nil, fmt.Errorf("names an invalid reference %q", name)
			}
			if ref != path {
				refs = append(refs, ref)
			}
		}
		return refs, nil
	}
	return nil, nil
}

func narinfoNames(body []byte, path string) bool {
	scanner := bufio.NewScanner(strings.NewReader(string(body)))
	for scanner.Scan() {
		if key, value, ok := strings.Cut(scanner.Text(), ":"); ok && key == "StorePath" {
			return strings.TrimSpace(value) == path
		}
	}
	return false
}

// relevantRuns returns the newest still-pending darwin-floor-cache run for one
// of the wait commits, and the newest relevant run of any status. A workflow
// the default branch does not have yet has no runs.
func (f *floorGate) relevantRuns(ctx context.Context, g *gate) (pending, latest *run, err error) {
	var wf workflow
	if _, err := g.getJSON(ctx, "/repos/"+g.repo+"/actions/workflows/"+floorWorkflowFile, nil, &wf); err != nil {
		var status statusError
		if errors.As(err, &status) && status.code == http.StatusNotFound {
			return nil, nil, nil
		}
		return nil, nil, err
	}
	if wf.ID <= 0 || wf.Path != floorWorkflowPath {
		return nil, nil, errors.New("the Actions workflow identity did not match " + floorWorkflowPath)
	}
	endpoint := fmt.Sprintf("/repos/%s/actions/workflows/%d/runs", g.repo, wf.ID)
	query := url.Values{"per_page": {strconv.Itoa(pageSize)}}
	var matches []run
	for page := 1; page <= floorRunPages; page++ {
		query.Set("page", strconv.Itoa(page))
		var result runList
		next, err := g.getJSON(ctx, endpoint, query, &result)
		if err != nil {
			return nil, nil, err
		}
		for _, candidate := range result.WorkflowRuns {
			if f.relevant(candidate, wf.ID) {
				matches = append(matches, candidate)
			}
		}
		if next == nil {
			break
		}
		query = next
	}
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].RunNumber == matches[j].RunNumber {
			return matches[i].ID > matches[j].ID
		}
		return matches[i].RunNumber > matches[j].RunNumber
	})
	for i := range matches {
		if latest == nil {
			latest = &matches[i]
		}
		switch matches[i].Status {
		case "queued", "in_progress", "waiting", "pending", "requested":
			return &matches[i], latest, nil
		}
	}
	return nil, latest, nil
}

// relevant: a run of this repository's workflow at one of the wait commits. A
// push counts only on main; a dispatch on any branch, because when main has
// moved its flake on, the next step is a dispatch on a branch at the target.
func (f *floorGate) relevant(candidate run, workflowID int64) bool {
	return candidate.ID > 0 && candidate.WorkflowID == workflowID &&
		(candidate.Path == floorWorkflowPath || strings.HasPrefix(candidate.Path, floorWorkflowPath+"@")) &&
		((candidate.Event == "push" && candidate.HeadBranch == branch) || (candidate.Event == "workflow_dispatch" && candidate.HeadBranch != "")) &&
		candidate.Repository.FullName == f.api.repo && candidate.HeadRepository.FullName == f.api.repo &&
		f.waitSHAs[candidate.HeadSHA]
}

// parseWaitSHAs reads one lowercase full commit SHA per line.
func parseWaitSHAs(r io.Reader) (map[string]bool, error) {
	out := map[string]bool{}
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if !shaPattern.MatchString(line) {
			return nil, fmt.Errorf("invalid commit SHA %q in the wait list", line)
		}
		out[strings.ToLower(line)] = true
		if len(out) > maxWaitSHAs {
			return nil, fmt.Errorf("the wait list exceeds %d commits", maxWaitSHAs)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, errors.New("the wait list is empty")
	}
	return out, nil
}
