#!/bin/sh
# Tests for undeploy.sh. Run by `just lint-ci`. Every case runs the script against a
# fake HOME and GOBIN holding a stub `yolo`, so nothing real is touched.

set -eu

here=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
script="$here/undeploy.sh"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

pass=0
fail=0

ok() { pass=$((pass + 1)); echo "ok   $1"; }
bad() { fail=$((fail + 1)); echo "FAIL $1" >&2; }

# check <label> <condition-exit-status>
check() {
    if [ "$2" = 0 ]; then ok "$1"; else bad "$1"; fi
}

# The stub yolo is a real Go build whose main package is yolo's (testdata/fake-yolo), since
# undeploy.sh runs only what launcher-slot.py identifies as a yolo build. It appends
# `stop <name>` for `host-daemon stop` to $HOME/../stop.log.
if ! command -v go >/dev/null 2>&1; then
    echo "FAIL test-undeploy.sh needs go to build its stub yolo; install it (just setup) and re-run" >&2
    exit 1
fi
mkdir -p "$tmp/fakemod/cmd/yolo"
printf 'module github.com/mschulkind-oss/yolo-jail\n\ngo 1.22\n' >"$tmp/fakemod/go.mod"
cp "$here/testdata/fake-yolo/main.go" "$tmp/fakemod/cmd/yolo/main.go"
(cd "$tmp/fakemod" && GOWORK=off GOTOOLCHAIN=local GOFLAGS=-mod=mod \
    go build -trimpath -o "$tmp/fake-yolo" ./cmd/yolo)

# fixture: builds $home, $gobin, $log with a stub yolo, a bundle symlink, a generation
# dir and a state file.
fixture() {
    home="$tmp/home"
    gobin="$tmp/gobin"
    log="$tmp/stop.log"
    rm -rf "$home" "$gobin" "$log"
    mkdir -p "$gobin" "$home/.local/share/yolo-jail/flake-bundles/gen1"
    : >"$log"
    echo state >"$home/.local/share/yolo-jail/state.json"
    ln -s flake-bundles/gen1 "$home/.local/share/yolo-jail/flake-bundle"
    cp "$tmp/fake-yolo" "$gobin/yolo"
}

# foreign <path>: an executable that is not a yolo build and records any run in $log.
foreign() {
    printf '#!/bin/sh\n# github.com/mschulkind-oss/yolo-jail/cmd/yolo\necho ran >>"%s"\n' "$log" >"$1"
    chmod +x "$1"
}

# run_script [args...] — YOLO_VERSION / YOLO_REPO_ROOT cleared, real yolo off PATH.
run_script() {
    env -u YOLO_VERSION -u YOLO_REPO_ROOT HOME="$home" GOBIN="$gobin" \
        PATH=/usr/bin:/bin sh "$script" "$@" >"$tmp/out" 2>"$tmp/err"
}

bundle="flake-bundle"

# (a) refuses inside a jail
fixture
rc=0
env YOLO_VERSION=1 HOME="$home" GOBIN="$gobin" PATH=/usr/bin:/bin sh "$script" \
    >"$tmp/out" 2>"$tmp/err" || rc=$?
check "a: refuses with YOLO_VERSION set (exit 1)" "$([ "$rc" = 1 ] && echo 0 || echo 1)"
check "a: nothing removed" "$([ -x "$gobin/yolo" ] && [ -L "$home/.local/share/yolo-jail/$bundle" ] && echo 0 || echo 1)"

# (b) --dry-run
fixture
rc=0
run_script --dry-run || rc=$?
check "b: dry-run exits 0" "$rc"
check "b: dry-run keeps binary and link" "$([ -x "$gobin/yolo" ] && [ -L "$home/.local/share/yolo-jail/$bundle" ] && echo 0 || echo 1)"
check "b: dry-run stops nothing" "$([ ! -s "$log" ] && echo 0 || echo 1)"

# (c) real run
fixture
rc=0
run_script || rc=$?
check "c: exits 0" "$rc"
check "c: stopped alpha" "$(grep -q '^stop alpha$' "$log" && echo 0 || echo 1)"
check "c: stopped beta" "$(grep -q '^stop beta$' "$log" && echo 0 || echo 1)"
check "c: binary removed" "$([ ! -e "$gobin/yolo" ] && echo 0 || echo 1)"
check "c: link removed" "$([ ! -L "$home/.local/share/yolo-jail/$bundle" ] && echo 0 || echo 1)"
check "c: generation kept" "$([ -d "$home/.local/share/yolo-jail/flake-bundles/gen1" ] && echo 0 || echo 1)"
check "c: state kept" "$([ -f "$home/.local/share/yolo-jail/state.json" ] && echo 0 || echo 1)"

# (d) a real directory at the bundle path is left alone
fixture
rm "$home/.local/share/yolo-jail/$bundle"
mkdir "$home/.local/share/yolo-jail/$bundle"
rc=0
run_script || rc=$?
check "d: exits 0" "$rc"
check "d: real directory kept" "$([ -d "$home/.local/share/yolo-jail/$bundle" ] && echo 0 || echo 1)"

# (e) nothing installed
fixture
rm -f "$gobin/yolo" "$home/.local/share/yolo-jail/$bundle"
rc=0
run_script || rc=$?
check "e: exits 0" "$rc"
check "e: says nothing to undeploy" "$(grep -q 'Nothing to undeploy' "$tmp/out" && echo 0 || echo 1)"

# (f) the obsolete selector is disclosed without its value
fixture
rc=0
env -u YOLO_VERSION YOLO_REPO_ROOT=/private/checkout HOME="$home" GOBIN="$gobin" PATH=/usr/bin:/bin sh "$script" \
    >"$tmp/out" 2>"$tmp/err" || rc=$?
check "f: exits 0" "$rc"
check "f: names obsolete selector" "$(grep -q 'YOLO_REPO_ROOT is obsolete' "$tmp/err" && echo 0 || echo 1)"
check "f: does not disclose selector value" "$(! grep -q '/private/checkout' "$tmp/err" && echo 0 || echo 1)"
check "f: offers matched build by path" "$(grep -q 'just build-dev-bundle' "$tmp/err" && grep -q 'printed launcher path' "$tmp/err" && echo 0 || echo 1)"

# (g) a `just deploy-dev` link is removed without being run (running it would build a bundle)
fixture
mkdir -p "$tmp/checkout/scripts"
printf '#!/bin/sh\necho ran >>"%s"\n' "$log" >"$tmp/checkout/scripts/dev-wrapper.sh"
chmod +x "$tmp/checkout/scripts/dev-wrapper.sh"
rm -f "$gobin/yolo"
ln -s "$tmp/checkout/scripts/dev-wrapper.sh" "$gobin/yolo"
rc=0
run_script || rc=$?
check "g: exits 0" "$rc"
check "g: dev link removed" "$([ ! -e "$gobin/yolo" ] && [ ! -L "$gobin/yolo" ] && echo 0 || echo 1)"
check "g: wrapper never ran" "$([ ! -s "$log" ] && echo 0 || echo 1)"
check "g: checkout kept" "$([ -f "$tmp/checkout/scripts/dev-wrapper.sh" ] && echo 0 || echo 1)"
check "g: says it was the dev launcher" "$(grep -q 'dev launcher link' "$tmp/out" && echo 0 || echo 1)"
check "g: stable link still removed" "$([ ! -L "$home/.local/share/yolo-jail/$bundle" ] && echo 0 || echo 1)"

# (h) a GOBIN/yolo that is not a yolo build is refused, never run, and nothing changes
fixture
foreign "$gobin/yolo"
cp "$gobin/yolo" "$tmp/foreign.orig"
rc=0
run_script || rc=$?
check "h: refuses (exit 1)" "$([ "$rc" = 1 ] && echo 0 || echo 1)"
check "h: never ran it" "$([ ! -s "$log" ] && echo 0 || echo 1)"
check "h: left untouched" "$(cmp -s "$gobin/yolo" "$tmp/foreign.orig" && echo 0 || echo 1)"
check "h: stable link kept" "$([ -L "$home/.local/share/yolo-jail/$bundle" ] && echo 0 || echo 1)"
check "h: names the next step" "$(grep -q "remove it by hand" "$tmp/err" && echo 0 || echo 1)"

# (i) a named undeploy removes only that launcher, and keeps the bundle the deployed yolo uses
fixture
ln -s "$tmp/checkout/scripts/dev-wrapper.sh" "$gobin/yolo-dev"
rc=0
run_script yolo-dev || rc=$?
check "i: exits 0" "$rc"
check "i: yolo-dev removed" "$([ ! -L "$gobin/yolo-dev" ] && echo 0 || echo 1)"
check "i: yolo kept" "$(cmp -s "$gobin/yolo" "$tmp/fake-yolo" && echo 0 || echo 1)"
check "i: no daemon stopped" "$([ ! -s "$log" ] && echo 0 || echo 1)"
check "i: stable link kept for yolo" "$([ -L "$home/.local/share/yolo-jail/$bundle" ] && echo 0 || echo 1)"
check "i: says why it kept the link" "$(grep -q "still used by" "$tmp/out" && echo 0 || echo 1)"

# (j) an unnamed undeploy lists the launchers it left behind, with the command for each
fixture
ln -s "$tmp/checkout/scripts/dev-wrapper.sh" "$gobin/yolo-dev"
rc=0
run_script || rc=$?
check "j: exits 0" "$rc"
check "j: yolo removed" "$([ ! -e "$gobin/yolo" ] && echo 0 || echo 1)"
check "j: yolo-dev kept" "$([ -L "$gobin/yolo-dev" ] && echo 0 || echo 1)"
check "j: lists yolo-dev with its command" "$(grep -q "$gobin/yolo-dev: .*Remove it with: just undeploy yolo-dev" "$tmp/out" && echo 0 || echo 1)"
check "j: a dev launcher does not keep the stable link" "$([ ! -L "$home/.local/share/yolo-jail/$bundle" ] && echo 0 || echo 1)"

# (k) a named launcher that is not yolo's is refused untouched
fixture
foreign "$gobin/yolo-x"
rc=0
run_script yolo-x || rc=$?
check "k: refuses (exit 1)" "$([ "$rc" = 1 ] && echo 0 || echo 1)"
check "k: never ran it" "$([ ! -s "$log" ] && echo 0 || echo 1)"
check "k: kept it and yolo" "$([ -x "$gobin/yolo-x" ] && [ -x "$gobin/yolo" ] && echo 0 || echo 1)"

# (l) one name at a time, and a plain name only
fixture
rc=0
run_script yolo-a yolo-b || rc=$?
check "l: two names refused (exit 2)" "$([ "$rc" = 2 ] && echo 0 || echo 1)"
rc=0
run_script ../yolo || rc=$?
check "l: a path refused (exit 2)" "$([ "$rc" = 2 ] && echo 0 || echo 1)"
check "l: nothing removed" "$([ -x "$gobin/yolo" ] && echo 0 || echo 1)"

echo "$pass passed, $fail failed"
[ "$fail" = 0 ]
