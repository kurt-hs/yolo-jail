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
    cat >"$gobin/yolo" <<EOS
#!/bin/sh
case "\$*" in
    "internal bundle-dir") echo "$home/.local/share/yolo-jail/flake-bundle" ;;
    "host-daemon status --format json")
        echo '[{"loophole": "alpha", "running": true}, {"loophole": "beta", "running": false}]' ;;
    "host-daemon stop "*) echo "stop \$3" >>"$log" ;;
esac
EOS
    chmod +x "$gobin/yolo"
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

echo "$pass passed, $fail failed"
[ "$fail" = 0 ]
