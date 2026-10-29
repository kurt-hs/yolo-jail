---
status: accepted
stage: DECIDED
next: "Execute the local real-Git witness matrix; do not repair production without a reproduced false fast-forward rejection."
---

# Issue 3: real-Git rejection phase witness

Written against `662d9103b677594de38e6f96f7a07ec693fbf28a`, 2026-10-29.
Owner-authorized item19 bug diagnosis/repair; branch approval feature issue2 is deferred.
Behavior authority: `docs/design/git-broker.md`, GB-D12/13/15 and failure paths.
Precedence: authority wins behavior; current tree wins facts; this plan is advice.

## Observed evidence

- `internal/gitremote/helper.go` Run advertises only connect; connect relays the original URL and real Git protocol to the broker.
- `internal/gitbroker/push.go` push relays upstream advertisement unchanged; readPush/judge only produce needs approval for uncovered heads/tags. No needs-force literal occurs there.
- Real caller baseline command `go test -v ./internal/gitbroker -run '^Test(PushRefusedNothingForwarded|MainStillRefused|ConfigBranchUpdatesMain|ConfigBranchForceUpdate|HTTPSPushRefusedNothingForwarded)$' -count=1 -timeout=120s` passed. Ordinary fast-forward reaches needs approval; existing declared branch FF and force updates succeed. Thus no false-FF RED is known yet; do not manufacture one or guess a source fix.
- Prior `8fa41b4` URL repair and `docs/research/git-broker-url-coverage.md` distinguish transport recognition/repository identity from exact endpoint approval. Do not change them.

## Map and fence

| File | Stage change |
| --- | --- |
| `internal/gitbroker/force_test.go` (new) | Permanent production-caller local-Git matrix and witness logging |
| `docs/research/git-force-disposition-plan.md` | This bounded planning record; no stage writer edits |

Exclusive authoring fence: new force_test.go only. Existing broker_test.go, https_test.go, gitremote/helper.go, push.go, policy.go, gitproto files, pack manifests, integration files and scope checks are READ ONLY. If a genuine false FF RED identifies a production repair, stop and report exact witness to lead; lead reserves overlap and issues a subsequent bounded repair brief. This tier-3 authoring fence is backed by separate manually locked worktree and commit-range inspection.

## Reuse

- `newFixture(t, "tee")`, `fixture.git`, `mustGit`, `gitEnv`, `auditEvents`, `lastPush` in broker_test.go: local bare repo, guarded SSH, real helper subprocess and production pack rewrite. No copied parser or new helper protocol.
- `newHTTPSFixture(t, httpsOpts{})`, `httpsFixture.pushPosts`, `credCalls`, server requests: local TLS/git-http-backend and fake fixture credentials only.
- Existing newFixture changes HOME within the test process for fixture-owned config, not the Go command's HOME. Keep original HOME/GOCACHE/GOMODCACHE/GOENV for Go commands. Baseline: GOCACHE=/home/agent/.cache/go-build, GOMODCACHE=/home/agent/go/pkg/mod, GOENV=/home/agent/.config/go/env.

## Task 1 — witness matrix, independent

Settled (must, do not reopen): normal known commit ancestry FF reaches broker policy, uncovered branch remains needs approval; legitimate client safety checks are not bypassed; --force changes Git client checks but never broker authority. No new grant, actual remote, capability, approval UX or production branch operations.

Preconditions: clean exact manually locked implementation tree at committed planning ref. Never mutate Main or integration worker tree. No reset/rebase/amend/stash/global config/cache changes. Stop if baseline tests fail unexpectedly or fence expansion is needed.

Tests first: add TestRealGitPushRejectionPhases in new force_test.go, table subtests with fresh fixture, SSH and HTTPS variants where applicable. Reuse the existing fixture's real `git push origin HEAD:refs/heads/main` and `--force` form. Cases:

1. Known FF: record bare main old; commit allow-empty child in workspace; assert cat-file old/new commit and merge-base --is-ancestor success; normal and force both print remote rejected needs approval, never needs force. Assert audit refs old/new/ref exact, upstream main unchanged, SSH upstream stdin exactly 0000 or TLS receive-pack POST count zero. Include original URL audit.
2. Genuine divergence: create sibling commit from parent in workspace; prove merge-base ancestry false. Normal Git must reject locally (non-fast-forward), forced Git reaches unchanged needs approval. Separate audit before/after and upstream side effects.
3. Noncommit source: use blob or tag-object refspec updating main. Record cat-file types, exact client stderr and broker command count. Do not assume this is FF or repair Git's checks. Forced form remains broker refusal and upstream unchanged if client sends commands.
4. Missing old object: upstream advances in fixture seed while workspace has not fetched; verify advertised bare old absent via cat-file in workspace. Normal may say fetch first rather than needs force; forced must reach broker refusal. No object deletion needed.
5. Shallow/missing graph: make a fixture-local shallow workspace using file:// clone --depth=1 (never network), preserve helper routing to fixture origin; show shallow flag and available OIDs/ancestry. Assert valid known FF still reaches broker refusal, or record legitimate unavailable graph rejection precisely. No assumption that conceptual FF is client-provable FF.

Log real `git --version`, old/new/refspec, object types, ancestry result, shallow status, helper capabilities (production connect-only; existing helper tests validate), and stderr phase. Negative controls must record broker audit command counts, empty forwarded bytes/zero POST and unchanged bare ref. Use real caller, not judge/readPush direct calls. No t.Parallel.

RED rule: run newly added test before any production source change and preserve full output. If all matrix tests GREEN this is a NONREPRODUCED disposition, not RED/GREEN repair proof. If false FF fails, preserve failure and stop with minimal cause witness; do not implement speculative source fix.

Gates: `just format`; focused matrix `go test -v ./internal/gitbroker -run '^TestRealGitPushRejectionPhases$' -count=1 -timeout=120s`; affected packages `go test -short ./internal/gitbroker ./internal/gitremote ./internal/gitproto -count=1 -timeout=180s`; focused race matrix `go test -race ./internal/gitbroker -run '^TestRealGitPushRejectionPhases$' -count=1 -timeout=180s`. Since this test calls fixture.git reading packs/git/pack.json, run `python3 scripts/completion-census.py`; report any output modification and STOP before committing outside fence (lead will coordinate). No per-lane full suite/check-ci. No runtime source edit means nested launch not automatically required; root may request matched smoke/integration disposition separately.

Commit: `test(git-broker): distinguish client force checks from broker refusals` (only if tests match expected disposition; no false repair claim).
Report durable receipts under `.yolo/durable/git-scope-force-20261029`, actual commit/ref/tree, diffstat, no RED if unreproduced, exact cases and emitted phase, residual native/source/install/version witness gaps. Retain locked worktree until Root cleanup.

## External witness only after local actions

If matrix sustains legitimate checks/nonreproduction, next unlock is smallest redacted original stderr (including rejected vs remote rejected), client Git version, installed/source yolo ref, helper capabilities, refspec/force mode, advertised old/new IDs, object types, shallow flag and local ancestry exit status. Do not solicit URL/credentials, public GitHub data, API calls or live pushes. Reproduction is not closed as fixed merely because local tests pass.
