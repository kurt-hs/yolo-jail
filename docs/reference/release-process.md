# Release process: the exact-commit pre-tag release gate

`just release` submits the current clean, full `HEAD` SHA as data to `release-request.yml` on `main`; it does not create a local tag or substitute a newer main commit. It takes no version: it releases CHANGELOG.md's newest dated section, the `## [X.Y.Z] - YYYY-MM-DD` heading the release-prep commit renamed from `[Unreleased]`, and prints the version it chose before dispatching ([PTG-D3](#PTG-D3)). The host checks the notes, the version's tag and main ancestry. Dispatch acceptance is not publication success.

## What `just release` refuses

Each refusal names the step that fixes it, and none dispatches anything:

| Refused | Next step it names |
| :--- | :--- |
| A version argument | Run `just release` with none; the message names the version CHANGELOG.md gives |
| No dated section (with notes under `[Unreleased]`) | Rename `## [Unreleased]` to `## [X.Y.Z] - <today>` and commit |
| A newest dated version that is not `X.Y.Z` or `X.Y.Z-pre` | Fix the heading and commit |
| A newest dated section dated after today (UTC) | Set the date to the release day and commit |
| A dirty tree, or a `HEAD` origin's `main` does not contain | Commit, or push this exact commit |
| The version already tagged at another commit, with notes under `[Unreleased]` | Rename `[Unreleased]` for the next version and commit |
| The version already tagged at another commit, and nothing under `[Unreleased]` | Nothing new to release |
| A section `scripts/changelog-section.sh` rejects | Write the section's prose and commit |

The version is read by [`scripts/release-version.sh`](../../scripts/release-version.sh); `scripts/test-changelog-section.sh` runs it over fixture changelogs, including every heading refusal, and runs the recipe in a scratch repository for the argument, the missing dated section, the future date, an origin that lacks `HEAD`, both tagged-elsewhere rows, a rejected section, the resume and a dispatch failure. A version already tagged **at `HEAD`** is not refused: the request goes on, for the [tag-only resume](#resuming-a-tag-only-release), which the workflow verifies.

## Writing a changelog section

CHANGELOG.md's section for a version is its GitHub release body, word for word.
[`scripts/changelog-section.sh`](../../scripts/changelog-section.sh) is the gate everywhere — `just release`, `release.yml` and `publish.yml` all run it, and `just check-ci` runs its tests — and it refuses a missing, empty or placeholder section, filler ("various improvements", "under the hood") and a list of commit subjects. There is no hatch, because a tag is never moved: a bad release is fixed by the next one. What the script cannot judge is who the section is written for:

- **A line needs two yeses**: could a reader who never opens the repository notice it, and did what it
  fixes ship in a published version? A fix to something added in the same cycle folds into that
  feature's paragraph. Refactors, tests, dependency bumps, CI and internal docs never get a line.
- **Write to the user, in their terms**: what they can now do, see or stop working around. No commit
  hashes, PR numbers, package names or file paths; a repository-relative doc link is fine, since
  `release.yml` pins it to the tag.
- **Short, by the maintainer's ruling (2026-10-01: ours had grown "nigh unreadable", ten times
  Vantage's).** The standard is the `open-source-project` skill's `references/changelog.md`, kept
  tighter than its ceilings:
  - a release opens with ONE sentence naming its two or three biggest changes;
  - a feature is one `**Bold lead.**` paragraph of at most two or three short sentences, then
    `See [doc](link)`;
  - a fix is one bullet line;
  - a section stays at or under about 650 words.

  Never say how a change works, why, or how it was found. `### Changed` keeps every upgrade-breaking
  change, each with the one action the user takes. When you land other work, rewrite its lines to
  this shape: a union merge keeps every rewording.
- `### Added`, `### Changed`, `### Fixed`, then `### Contributors`, thanking each outside
  contributor by public GitHub name and linked handle (never an email). A feature an outside
  contributor built also names them inline on its own entry ("Contributed by …"). A number goes in
  only where a test or a constant pins it.
- An old minor line's retrospective section is headed `## 0.9.x`. A heading that spells a full version
  is one the extractor will publish.

## Trusted source and capability boundaries

Each workflow checks out its own main dispatch SHA for orchestration, with checkout credentials unpersisted. `preflight.sh` requires a main branch context, exact commit identity, refreshed main ancestry and the regular `ci.yml` main/push run for the exact repository/SHA. The CI gate checks workflow identity, attempt-specific required jobs and architectures/shards, and refreshes its proof. Missing, failed, cancelled or timed-out proof refuses.

Only after eligibility does a separate **read-only** job check out target source under `target/`. Its notes, source bundle and wheels are checked/built without publication secrets. A new trusted-main job, which checks out no target source, refreshes exact CI, main/tag identity and version order immediately before creating the annotated tag and its create-only ref. A preparation or final-CI failure leaves the version unreserved. An existing ref refuses, except for the [tag-only resume](#resuming-a-tag-only-release) below; no force, delete or move operation is provided.

The request concurrency group is per version, without cancelling a running request. GitHub can replace a pending request with another pending request; this is not durable deduplication. The immutable ref creation is the final conflict boundary. Release and registry workflows share a global concurrency group to serialize release-order checks and `latest` writes. That group also has only one retained pending run, not a durable queue. Different-version requests can still leave a reserved tag if subsequent publication refuses or a pending publisher is replaced; the system is not transactional.

## Release assets and the original successful run

The request dispatches `release.yml` on main with the frozen version/SHA and originating request run ID. A real first, in-progress main release-request run with the exact request title must authorize initial Release creation. A matching tag or existing release object alone is not authorization.

GoReleaser **v2.18.2** prepares target builds in a read-only job using `release --clean --skip=publish --release-notes FILE`. The target config must byte-match the trusted config. Target hooks never receive release, tap, registry or OIDC publication credentials. There is no later GoReleaser invocation in a write-capable job and no assumed `continue`, artifact reuse or replay contract.

The official [quick start](https://goreleaser.com/quick-start/#a-note-about-the-flags) documents `--skip=publish`; the pinned CLI's help and [release implementation](https://github.com/goreleaser/goreleaser/blob/v2.18.2/cmd/release.go) supply the version-specific command contract. `TestPinnedRealGoReleaserReadonlyPreparationFeedsActualPackagingCaller`, enabled with `GORELEASER_TEST_BINARY`, runs that actual CLI offline against a tiny Go project using the production archive matrix, source-bundle layout and checksum settings. It replaces expensive project hooks with inert fixture inputs. The production packaging step consumes the observed `dist/artifacts.json` records, archives and checksums, rejecting unknown/missing paths, unsafe file kinds, duplicate records, a checksummed file other than the four archives, and checksum disagreement. This fixture is not a full production release build or a live upload proof.

The write job runs trusted code only. It re-renders notes from the target Git blob as inert text, verifies the complete expected asset names/types/digests against trusted config, refuses an existing release, creates an initial draft, uploads all files without clobber and finalizes that draft only after every upload succeeds. The four platform archives retain the host binary and prebuilt source bundle, and `checksums.txt` is the one other asset: a release publishes no pack binary ([`OQ-BP8`](pack-binaries.md#OQ-BP8)). The body is the exact target changelog section with tag-pinned links; prerelease versions retain the prerelease flag. Initial draft finalization is allowed, but an existing complete or partial release is never replayed or overwritten. Homebrew's source-build formula is updated by trusted main code.

The normal Release `run-name` is exactly `Release vVERSION @ FULL_SHA / request POSITIVE_RUN_ID`. The request waits for exactly one original, first-attempt matching Release run to complete successfully before dispatching `publish.yml` on main. Failed, rerun, multiple, unknown or timed-out runs refuse; neither parallel dispatch acceptance nor release-object presence substitutes for success. A run started with `GITHUB_TOKEN` fires no `workflow_run` event, so after dispatching the publisher the request dispatches `tap-install.yml` on main with the released version as its input, and the tap checker enforces that version. A human-started Release run (the Homebrew-only backfill) still fires `workflow_run`, and the checker reads its inert `display_title`. Missing/malformed normal main metadata refuses, rather than comparing the tap against itself. Historical tag-trigger metadata remains decodable.

## One-shot registry publication

`publish.yml` keeps its filename, direct `uv publish ... --trusted-publishing always` call, `pypi` environment and `id-token: write` identity. Before any build, trusted preflight verifies exact CI/tag/main identity, monotonic published-release version order and the exact original request/successful Release run IDs and titles. A legacy release without this request provenance cannot use this route.

A separate trusted write job repeats these checks and creates the fixed-name `yolo-publication-claim.json` asset on the already-published release. It binds tag, SHA and request/Release/publisher run IDs. The [asset upload API](https://docs.github.com/en/rest/releases/assets#upload-a-release-asset) refuses duplicate names; the caller never deletes, renames, clobbers, retries an ambiguous upload or silently recreates the claim. Duplicate, rerun, partial, older-version and unknown-state attempts fail closed before target builds or registry writes. The claim is workflow-retained, **not administrator-immutable**, and it does not prove registry completion.

Target wheels, Nix closures and builder images are built in separate read-only jobs without publishing secrets. Publishing jobs execute trusted tools on inert handoffs only: wheels undergo exact platform/name/version/ZIP checks before direct uv upload; Nix store path names are validated, then the closure is imported with `--no-check-sigs`, because runner-built paths carry no signature. Its contents are therefore trusted as the output of the exact, CI-gated target build, not checked against upstream signatures; image archives are checked for regular-file and platform identity before trusted registry copy/index commands. Publication remains nontransactional across PyPI, Cachix and GHCR. The uv `--check-url` option is not a claim of wheel hash equality or general recovery; the create-only claim forbids cross-run replay of this route. A later failed job leaves the claim and all partial publication state for owner inspection. Each registry-writing job independently refuses any Actions attempt other than 1 at its first step, before credential actions or writes; a failed-jobs-only rerun cannot reuse cached successful preflight/claim prerequisites to publish again. The normal release-upload and claim jobs have the same entry refusal. The separate Homebrew-only backfill contract is unchanged.

## Main-scoped Homebrew-only backfill

The retained version-only `release.yml` dispatch defaults to `homebrew-only` on **main**, labeled `Homebrew-only vVERSION`. Trusted current tools resolve the immutable old tag and require a non-draft published release, exact main/CI/tag proof and read-only target notes checks. The old source need not contain the new release-wiring tools. The formula write runs from a separate trusted main checkout; GoReleaser and the registry publisher are skipped. This explicit legacy backfill preserves the tap checker's versionless behavior; only normal Release titles carry its required expected version. A historical target still must satisfy the current eligibility gate; this is not a waiver for missing CI or unreadable release state.

## Resuming a tag-only release

A request whose tag was created but whose publication never started may **resume** under a fresh request run. "Tag-only" means all of these hold, each proved read-only, and any other state refuses as before:

1. `vVERSION` is an annotated tag whose commit is exactly the requested SHA and whose message is the `yolo-jail VERSION` a request writes. The eligibility preflight checks this from `git ls-remote` (`RELEASE_ALLOW_RESUME=1`), and the write job checks it again through the API. A lightweight tag, or a tag at any other commit, refuses.
2. No GitHub Release exists for the tag, draft or published. The write job's token lists drafts. Release assets and the publication claim exist only on a release, so their absence follows.
3. Every earlier Release run for this version targeted this SHA, has completed and did not succeed. No `Homebrew-only vVERSION` run and no tag-push run exists for it.
4. No `publish.yml` run exists for this version, and no other release request for it is still running.
5. PyPI lists no `yolo-jail` release equal to the version, with PEP 440's pre-release spellings folded together. An unreadable answer refuses.
6. The Homebrew tap's formula does not name the tag, and an unreadable formula refuses.
7. The requested SHA's regular `ci.yml` push proof passes, as for any request.

A resume never creates, moves or deletes the tag. It skips tag creation and goes on from the Release dispatch. The new request's run ID goes into the Release `run-name`, and the Release run checks that this request is still in progress, exactly as for a first request. The publisher and claim bind that same request ID. The trigger is `just release` run from a checkout whose `HEAD` is the tagged commit and whose newest dated CHANGELOG.md section names that version. Orchestration is always the `main` workflow source, and every target build checks out the requested SHA, so the published content is the tagged commit whatever `main` holds. The write job's timeout covers a resume's waits as well as a first request's (`TestRequestJobTimeoutExceedsItsWaits`).

**Ledger.** PTG-D1 (2026-10-08, maintainer ruling: *"let's rollback and redo 12.2 add the resume now"*). 0.12.2's first live request created the tag and then failed before any Release run did anything. The version is not abandoned. *Superseded 2026-10-09 by [PTG-D2](#PTG-D2): 0.12.2 was abandoned instead.* A request may resume exactly that tag-only state, under the preconditions above. Everything else stays fail-closed.

<a id="PTG-D3"></a>**PTG-D3** (2026-10-18, maintainer ruling: *"why do I have to specify a version if we've already agreed and the gate will fail without it anyway from the changelog?"*). `just release` takes no version argument; it releases CHANGELOG.md's newest dated section. *Implementation decision under it:* an argument is **refused** rather than accepted when it matches, naming the version the changelog gives. One spelling of the command is simpler to document and test than two, and a matching argument would add nothing the changelog does not already say. The tag check moved into the recipe so that "already released" is refused locally with its next step; a version tagged at `HEAD` still goes on, so the tag-only resume keeps working.

<a id="PTG-D2"></a>**PTG-D2** (2026-10-09, maintainer ruling: *"can we just cancle 0.12.2 entirelyi and move to 0.13.0 like it never happened?"*). 0.12.2 is abandoned. It was never published: no GitHub Release, no PyPI upload, and the Homebrew formula stayed at 0.12.1. The owner deletes its tag on the host, and its notes are folded into the next release, 0.13.0. This is a ruling about one version, not a new path: the workflows still provide no delete or move, and the [tag-only resume](#resuming-a-tag-only-release) stays the only recovery they automate.

## Partial state and validation limits

After tag creation, any dispatch, build, upload, claim or registry failure can leave partial state. Apart from a [tag-only resume](#resuming-a-tag-only-release), do not rerun the request, delete/move the tag or claim, clobber assets, or use generic `gh run rerun --failed`. Inspect the immutable SHA and original runs read-only, then ask the owner to review the outcome before any further write:

```console
git ls-remote origin refs/tags/vVERSION
gh run list --repo OWNER/REPO --workflow release-request.yml --limit 20
gh run list --repo OWNER/REPO --workflow release.yml --limit 20
gh run list --repo OWNER/REPO --workflow publish.yml --limit 20
gh run view RUN_ID --repo OWNER/REPO
gh release view vVERSION --repo OWNER/REPO
```

There is no tag-push or release-event publisher trigger in the new workflow source. Human-created tags have already reserved a version and do not acquire normal publication authorization by object presence. The Homebrew-only route is the sole retained explicit backfill; there is no generic automatic publication recovery.

Tests execute actual workflow shell callers with local Git objects, stateful fake APIs/registries, and mutation regressions, plus the optional pinned real CLI fixture. They do not run GitHub Actions or establish live permissions, display-title transport, OIDC exchange, PyPI/registry acceptance or administrator behavior. A read-only inspection of the PyPI environment policy supports main-source identity compatibility, not live OIDC proof.
