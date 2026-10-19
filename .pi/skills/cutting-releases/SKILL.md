---
name: cutting-releases
description: Prepare, request, or verify a yolo-jail release. Use for release notes, version preparation, release gates, or publication follow-through.
---

# Cut a yolo-jail release

Use this workflow for yolo-jail releases. Read [the release-process reference](../../../docs/reference/release-process.md) when a request meets an existing tag, failed workflow, partial publication, or resume. Follow [the repository's changelog guidance](../../../AGENTS.md#the-changelog-is-the-release) when writing notes; it sets the project's user-facing rules and points to the shared changelog standard.

**A release request can create an immutable tag and publish artifacts.** Never run `just release` or dispatch a workflow unless the owner explicitly authorized publication. This skill prepares and verifies; it does not grant publication authority.

## Reuse evidence; do not repeat the full suite

Keep code landing, release preparation, and publication as separate stages:

- Code changes follow the normal testing and landing rules in [AGENTS.md](../../../AGENTS.md#testing). Normally, a launch-path landing includes one full integration suite. If the owner explicitly orders a particular code landing main-first, do not make a duplicate local suite a precondition to that already-authorized landing; report local and CI checks honestly as pending or canceled. This is a narrow sequencing instruction, not a general waiver of checks or hooks. The exact target SHA's regular main-push CI, including integration proof, is still required before tagging.
- Regular [`ci.yml`](../../../.github/workflows/ci.yml) push CI runs the complete integration suite across Linux architectures and shards, then checks that every listed test ran. Before tagging, the release gate requires a successful regular **main push** CI run for the exact target SHA, including all required jobs and shard-completeness proof. A green PR run alone is not enough. The gate is enforced by [`preflight.sh`](../../../tools/release-wiring/preflight.sh) and [`gate.go`](../../../tools/release-gate/gate.go).
- Release workflows consume that exact-SHA proof; release-request waits on it, and release/publish recheck it. They prepare and publish release outputs but do **not** rerun the integration suite. If only release notes, docs, or the version heading changed, do not add another full Go or integration run: validate the notes locally, then rely on the exact-SHA main-push CI gate. If code, tests, or release-gate inputs changed, run the relevant tests under [AGENTS.md](../../../AGENTS.md#testing) and require CI for the final target SHA.

Never treat a previously green run for a different SHA, a PR-only run, dispatch acceptance, or a local test as satisfying the exact-SHA release gate.

## Prepare the release commit

Run repository commands from the checkout root. First check the current branch and clean status; identify the newest release tag available in the checkout and review the changes since it, including commit bodies. Check what source CI has reported for the exact candidate SHA, but treat the release gate—not a PR result or manually selected run—as the authority. Confirm with the owner which version and UTC release date to use, and review any known issue's explicit disposition. Honor an owner decision that an issue is non-blocking—including the existing Pants DNS preference—instead of turning it into an all-platform hold; clearly disclose skipped or unproven coverage.

```bash
git status --short --branch
last_tag=$(git tag --sort=-v:refname | head -n 1)
printf 'Latest tag: %s\n' "$last_tag"
git log --oneline "$last_tag..HEAD"
git log --format='%h %s%n%b' "$last_tag..HEAD"
```

Edit [CHANGELOG.md](../../../CHANGELOG.md): move `[Unreleased]` into the owner-selected `## [X.Y.Z] - YYYY-MM-DD` section, using the actual release date. Write for users: open with one sentence naming the biggest changes; explain what users can now do or what problem stopped; keep breaking changes and the user's migration action explicit. Omit unreleased-only fixes, internal work, tests, commit subjects, and claims not verified against the current tree. Keep the section concise (about 650 words or less); use repo-relative doc links, which publication rewrites to links pinned to the immutable tag.

The extractor and the release command choose the version from the newest dated changelog section. Do not hardcode a version or pass one to the release command. From the repository root, validate the chosen section without contacting GitHub:

```bash
version=$(sh scripts/release-version.sh)
printf 'Candidate version: %s\n' "$version"
sh scripts/changelog-section.sh "$version" >/dev/null
```

Run relevant Markdown/frontmatter and changelog checks for what changed. A note-only edit does not justify a full Go or integration suite. Commit the notes/version preparation as one coherent commit, then hand its full SHA to the owner or authorized host operator. Do not push, tag, install, or publish from this skill.

## Request and follow the release

Only the owner or an explicitly authorized host operator should run the request, from a clean checkout of the exact release-prep commit that is contained in `origin/main`:

```bash
just release
```

This command takes **no version argument**. It confirms the newest dated section, clean working tree, exact `HEAD` ancestry on `origin/main`, tag state, and usable notes before dispatching [`release-request.yml`](../../../.github/workflows/release-request.yml) with that exact SHA. The release workflows treat the full commit SHA of this release-prep commit as the target; they do not substitute a newer `main` SHA. Workflow preflight checks version order and revalidates the main-push CI proof before tag/publication writes. The request workflow prepares target-source materials read-only, rechecks exact-SHA CI immediately before creating the immutable tag, then dispatches the trusted-main [Release workflow](../../../.github/workflows/release.yml) and waits for the exact original successful run before dispatching [Publish](../../../.github/workflows/publish.yml).

Monitor the original release-request run and its Release and Publish workflows through completion. Also check the Homebrew tap check if dispatched. A successful dispatch only means GitHub accepted a request; it does not prove release assets, PyPI, cache, or tap publication succeeded. Report the exact target SHA and each workflow's actual outcome, plus any skipped or unproven checks.

## Recover without automatically burning the version

A workflow failure does not by itself mean the version is consumed. Diagnose the failed stage and whether the immutable tag was created; preserve the same version whenever the supported route permits it. **Never bump the version automatically just because a workflow failed.** Get the owner's authorization before requesting or resuming a release.

- **Failure before the tag exists:** the version is unreserved. Read the original request's failed job. Fix the cause, then request the same version again if the notes remain valid, current release-version ordering permits it, and no tag has reserved it. A trusted workflow/orchestration fix on current `main` can leave the target source SHA unchanged. If the target source itself needs a fix, make a new source commit and use that full SHA; the same version is still available only while no tag has reserved it and the new SHA passes the normal exact-main-push-CI gate. The [preparation script](../../../tools/release-wiring/prepare.sh) and final pre-tag checks run before tag creation. Do not change the version merely to work around a fixable pre-tag failure.
- **Tag exists at the exact frozen SHA, with no later publication state:** the only automated continuation is the documented [tag-only resume](../../../docs/reference/release-process.md#resuming-a-tag-only-release). This can cover a request failure after tag creation (for example, a failed Release dispatch) or an unsuccessful Release run, but only when every proof below passes and no later state was written. The owner/authorized host operator runs `just release` with no arguments from a clean checkout at that exact tagged commit, whose newest dated changelog section still names the same version. This creates a fresh main-scoped request; it does not move or recreate the tag. [`request.sh`](../../../tools/release-wiring/request.sh) and `verify-resume` in the [release wiring](../../../tools/release-wiring/resume.go) must prove all of the following, or refuse: the tag is the request-created annotated tag with its expected message and exact target SHA; no draft or published GitHub Release exists; every prior normal Release run for that SHA is complete and unsuccessful (none succeeded or remains running), with no Homebrew-only or tag-push run; no Publish run exists; no other request for the version is running; PyPI has no equivalent version; the Homebrew formula does not carry the tag; and regular main-push CI succeeds for the exact SHA. Unreadable or incomplete proof blocks the resume. Fixing trusted release plumbing on `main` can accompany this route without changing the frozen target; a changed target source SHA cannot. If Release succeeded but its Publish dispatch failed, the prior successful Release means this is not tag-only and the automated resume refuses.
- **Tag points to a different SHA, a GitHub Release object exists, a normal Release run succeeded, or any Publish run exists (even if that run failed before an obvious registry write):** this is not the supported tag-only state. In particular, an existing draft/partial release or publication claim is not safe to overwrite, and Publish is not generically replayable. Do not move/delete the tag, retag different source, use `gh run rerun`, recreate releases, or clobber assets/claims. Preserve state and ask the owner to choose a disposition after read-only diagnosis. This blocks automatic continuation; it does **not** authorize an automatic version bump or prove the version must be abandoned. If a source fix changes the target SHA after tagging, the current resume proof rejects it; the workflows do not provide a same-tag source replacement.

Use the documented [partial-state inspection commands](../../../docs/reference/release-process.md#partial-state-and-validation-limits) to inspect the original runs and release without writing:

```console
git ls-remote origin refs/tags/vVERSION
gh run list --repo OWNER/REPO --workflow release-request.yml --limit 20
gh run list --repo OWNER/REPO --workflow release.yml --limit 20
gh run list --repo OWNER/REPO --workflow publish.yml --limit 20
gh run view RUN_ID --repo OWNER/REPO
gh release view vVERSION --repo OWNER/REPO
```

Do not dispatch Release or Publish directly: their provenance checks require the supported main-scoped request chain. A matching tag alone is not authorization or proof that a resume is safe.
