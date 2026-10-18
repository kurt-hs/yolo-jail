#!/bin/sh
# Print the version `just release` releases: the one CHANGELOG.md's newest dated
# section names, and refuse when that section cannot be the release.
#
# The maintainer, 2026-10-18 (docs/reference/release-process.md PTG-D3): "why do I
# have to specify a version if we've already agreed and the gate will fail without
# it anyway from the changelog?" So `just release` takes no version. The release-prep
# commit renames `## [Unreleased]` to `## [X.Y.Z] - YYYY-MM-DD`, and that heading —
# the first dated `## [...] - YYYY-MM-DD` heading in the file, which sits above every
# older one — IS the version.
#
# Usage:
#   release-version.sh [changelog-file]
#
# The file defaults to CHANGELOG.md beside this script's repository root. Exit 0
# with the version alone on stdout; 1 with the reason and the next step on stderr;
# 2 on an unreadable file. RELEASE_VERSION_TODAY (YYYY-MM-DD) replaces today's UTC
# date, for the tests.
#
# What it refuses, each with the step that fixes it:
#   - no dated section at all (an [Unreleased] with content says to rename it);
#   - a version that is not X.Y.Z or X.Y.Z-pre;
#   - a date after today.
# Whether the version is already TAGGED is the recipe's question, not this
# script's: it needs origin. `--unreleased` prints `content` or `empty` for the
# [Unreleased] section instead, so the recipe's refusal can say which step comes
# next.
set -eu

mode=version
if [ "${1:-}" = "--unreleased" ]; then
    mode=unreleased
    shift
fi
root=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
file=${1:-$root/CHANGELOG.md}
if [ ! -r "$file" ]; then
    echo "✗ cannot read $file — run this from a yolo-jail checkout." >&2
    exit 2
fi

# One pass: the first dated heading (version, date), and whether [Unreleased]
# holds any line that is neither blank, an HTML comment nor a bare subheading
# (an empty `### Added` left for the next release is not notes). Fenced code is
# skipped, as changelog-section.sh skips it, so a `## ` line inside one is code.
parsed=$(awk '
    BEGIN { fence = 0; inunrel = 0; unrel = "empty"; v = ""; d = "" }
    /^[ \t]*(```|~~~)/ { fence = !fence; if (inunrel) unrel = "content"; next }
    fence { if (inunrel) unrel = "content"; next }
    /^##[ \t]/ {
        inunrel = 0
        h = $0
        sub(/^##[ \t]+/, "", h)
        if (h ~ /^\[[Uu]nreleased\]/) { inunrel = 1; next }
        if (v == "" && match(h, /^\[[^]]+\][ \t]+(-|—|–)[ \t]+[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]/)) {
            v = h; sub(/^\[/, "", v); sub(/\].*$/, "", v)
            d = h; sub(/^\[[^]]+\][ \t]+(-|—|–)[ \t]+/, "", d); d = substr(d, 1, 10)
        }
        next
    }
    /^#[ \t]/ { inunrel = 0; next }
    inunrel && $0 !~ /^[ \t]*$/ && $0 !~ /^[ \t]*<!--.*-->[ \t]*$/ && $0 !~ /^###/ { unrel = "content" }
    END { print unrel; print v; print d }
' "$file")
unreleased=$(printf '%s\n' "$parsed" | sed -n 1p)
version=$(printf '%s\n' "$parsed" | sed -n 2p)
date=$(printf '%s\n' "$parsed" | sed -n 3p)

if [ "$mode" = unreleased ]; then
    echo "$unreleased"
    exit 0
fi

if [ -z "$version" ]; then
    if [ "$unreleased" = content ]; then
        echo "✗ CHANGELOG.md has no dated release section — rename '## [Unreleased]' to '## [X.Y.Z] - $(date -u +%Y-%m-%d)' for the version you are releasing, commit, and run 'just release' again." >&2
    else
        echo "✗ CHANGELOG.md has no dated release section and nothing under [Unreleased] — write the release's notes under '## [X.Y.Z] - YYYY-MM-DD', commit, and run 'just release' again." >&2
    fi
    exit 1
fi
version=${version#v}
if ! printf '%s\n' "$version" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$'; then
    echo "✗ CHANGELOG.md's newest dated section is '[$version]', which is not a release version (want X.Y.Z or X.Y.Z-pre) — fix its heading, commit, and run 'just release' again." >&2
    exit 1
fi
today=${RELEASE_VERSION_TODAY:-$(date -u +%Y-%m-%d)}
# ISO dates order as strings.
if [ "$(printf '%s\n%s\n' "$date" "$today" | sort | tail -n 1)" != "$today" ]; then
    echo "✗ CHANGELOG.md dates $version $date, after today ($today, UTC) — set the heading's date to the day you release, commit, and run 'just release' again." >&2
    exit 1
fi
echo "$version"
