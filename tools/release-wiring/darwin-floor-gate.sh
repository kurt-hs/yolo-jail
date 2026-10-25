#!/usr/bin/env bash
# The darwin floor cache gate: before the tag is written, both macOS floor
# profiles of the exact target commit must be in the project's Cachix cache, or
# every macos-user user of the release compiles the floor (a ~27-minute mise
# source build) themselves. darwin-floor-cache.yml pushes them on main pushes
# that change flake.nix or flake.lock; this waits while such a run for the
# target's flake inputs is queued or running. On a miss with none pending it
# dispatches main's darwin-floor-cache.yml once to build the exact target
# (`-f sha=RELEASE_SHA`), waits for that run, and passes only when it succeeded
# and the cache then serves every path; anything else fails the request with
# no tag (docs/reference/release-process.md#PTG-D5).
#
# Inputs: DARWIN_FLOOR_PATHS (space-separated store paths from the read-only
# prepare-target job's darwin-floor-paths.sh), CACHIX_CONFIGURED (true|false,
# whether the repository has a CACHIX_AUTH_TOKEN secret), CACHIX_CACHE (the
# cache name), GITHUB_REPOSITORY, RELEASE_SHA and a GH_TOKEN with actions: write
# (the dispatch) and actions: read (watching runs).
set -euo pipefail

fail() {
  echo "✗ $* No tag was created." >&2
  exit 1
}

: "${GITHUB_REPOSITORY:?GITHUB_REPOSITORY is required (OWNER/REPO)}"
: "${RELEASE_SHA:?RELEASE_SHA is required (full lowercase commit SHA)}"
case "${CACHIX_CONFIGURED:-}" in
  false)
    # Only a fork may skip: the project's own repository always pushes its floor
    # to Cachix, so a missing secret there is a broken release setup, not a fork
    # (PTG-D5: no release without the floor).
    if [ "$GITHUB_REPOSITORY" = "mschulkind-oss/yolo-jail" ]; then
      fail "${GITHUB_REPOSITORY} has no CACHIX_AUTH_TOKEN secret, so the darwin floor cannot be built into Cachix. Restore the secret (repository Settings > Secrets and variables > Actions), then request this release again; the version is still unreserved."
    fi
    echo "Cachix is not configured for ${GITHUB_REPOSITORY} (no CACHIX_AUTH_TOKEN secret), so darwin-floor-cache.yml pushes nothing; skipping the darwin floor cache gate. macos-user users of this release build the floor themselves."
    exit 0
    ;;
  true) ;;
  *) fail "CACHIX_CONFIGURED must be true or false (got '${CACHIX_CONFIGURED:-}'); the release-request workflow sets it from the CACHIX_AUTH_TOKEN secret." ;;
esac
cache=${CACHIX_CACHE:-yolo-jail}
if ! printf '%s\n' "$cache" | grep -Eq '^[a-z0-9][a-z0-9-]{0,62}$'; then
  fail "Invalid Cachix cache name '${cache}'; fix the CACHIX_CACHE repository variable."
fi
read -r -a paths <<< "${DARWIN_FLOOR_PATHS:-}"
if [ "${#paths[@]}" -ne 2 ]; then
  fail "Expected the two darwin floor profile paths from the prepare-target job, got ${#paths[@]}; inspect that job's 'Compute the darwin floor profile paths' step."
fi
path_args=()
for path in "${paths[@]}"; do
  if [[ ! "$path" =~ ^/nix/store/[0-9a-df-np-sv-z]{32}-[A-Za-z0-9+._?=-]+$ ]]; then
    fail "The prepare-target job handed over an invalid store path; inspect its 'Compute the darwin floor profile paths' step."
  fi
  path_args+=(--path "$path")
done

# The commits whose darwin-floor-cache runs build these exact paths: main's
# commits from the last one at or before the target that changed the flake
# inputs, up to the next one after it that changes them again. Every run's
# title names the commit it builds: a push run's is the tip of its push, a
# dispatch without a sha input builds main's tip at dispatch, and one with a
# sha input builds that commit, so a run building these paths can name any
# commit in that span.
git fetch --quiet --no-tags origin main || fail "Could not refresh origin/main; retry after connectivity is restored."
wait_file=$(mktemp "${RUNNER_TEMP:-${TMPDIR:-/tmp}}/darwin-floor-wait.XXXXXX") || fail "Could not allocate runner temp space."
trap 'rm -f "$wait_file"' EXIT
echo "$RELEASE_SHA" > "$wait_file"
flake_commit=$(git log -1 --format=%H "$RELEASE_SHA" -- flake.nix flake.lock) || fail "Could not read the flake history of ${RELEASE_SHA}."
if [ -n "$flake_commit" ]; then
  # rev-list lists newest first, so the last line is the oldest later change.
  next_change=$(git rev-list --ancestry-path "${RELEASE_SHA}..FETCH_HEAD" -- flake.nix flake.lock | tail -n 1) ||
    fail "Could not read main's flake history after ${RELEASE_SHA}."
  end=FETCH_HEAD
  if [ -n "$next_change" ]; then
    end="${next_change}^"
  fi
  {
    echo "$flake_commit"
    git rev-list --max-count=4000 --ancestry-path "${flake_commit}..${end}"
  } >> "$wait_file" || fail "Could not list main's commits since ${flake_commit}."
fi

# 180 minutes: a pending run waited out at darwin-floor-cache.yml's 90-minute
# job ceiling, then the build this gate dispatches at the same ceiling.
if ! go run ./tools/release-gate darwin-floor --repo "$GITHUB_REPOSITORY" --cache "$cache" --sha "$RELEASE_SHA" \
  "${path_args[@]}" --wait-shas "$wait_file" --timeout 180m --poll-interval 30s; then
  fail "The darwin floor profiles of ${RELEASE_SHA} are not in https://${cache}.cachix.org, and the build this request dispatched or waited for did not put them there (what to check is above). Fix that, then request this version again; it is still unreserved, and the request builds the floor again."
fi
