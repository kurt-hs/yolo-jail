#!/usr/bin/env bash
# Compute the two macOS floor-profile store paths of the exact target commit,
# without building anything, for the darwin floor cache gate (darwin-floor-gate.sh).
# This evaluates target-controlled Nix, so it runs only in the read-only
# prepare-target job, after trusted-main SHA/CI eligibility, with no credential
# in its environment. The paths leave this job as inert data, which the write
# job validates before it asks the cache about them.
#
# The evaluation matches what a macos-user launch builds
# (darwinpkg.BuildFloorProfileArgv): the yoloNoncontainerProfile attribute,
# --impure, --accept-flake-config, and YOLO_EXTRA_PACKAGES unset, so the floor
# alone. Evaluating a darwin attribute on Linux only instantiates derivations,
# which is all an out path needs.
set -euo pipefail

: "${RELEASE_SHA:?RELEASE_SHA is required}"
: "${TARGET_ROOT:?TARGET_ROOT is required}"
target_head=$(git -C "$TARGET_ROOT" rev-parse HEAD) || {
  echo "✗ Could not resolve the isolated target source checkout; no tag was created." >&2
  exit 1
}
if [ "$target_head" != "$RELEASE_SHA" ]; then
  echo "✗ Target source checkout $target_head does not match frozen SHA $RELEASE_SHA; no tag was created." >&2
  exit 1
fi

paths=()
for system in aarch64-darwin x86_64-darwin; do
  if ! out=$(
    cd "$TARGET_ROOT" &&
      env -u GITHUB_TOKEN -u GH_TOKEN -u HOMEBREW_TAP_TOKEN -u CACHIX_AUTH_TOKEN -u YOLO_EXTRA_PACKAGES \
        nix --extra-experimental-features 'nix-command flakes' --accept-flake-config \
        eval --impure --raw ".#packages.${system}.yoloNoncontainerProfile.outPath"
  ); then
    echo "✗ Could not evaluate the ${system} floor profile of ${RELEASE_SHA}; fix the target flake and request this version again. No tag was created." >&2
    exit 1
  fi
  if [[ ! "$out" =~ ^/nix/store/[0-9a-df-np-sv-z]{32}-[A-Za-z0-9+._?=-]+$ ]]; then
    echo "✗ The ${system} floor profile evaluated to an invalid store path; inspect the target flake. No tag was created." >&2
    exit 1
  fi
  paths+=("$out")
  echo "${system} floor profile: ${out}"
done
if [ -n "${GITHUB_OUTPUT:-}" ]; then
  echo "paths=${paths[*]}" >> "$GITHUB_OUTPUT"
fi
