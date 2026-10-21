#!/bin/sh
# undeploy.sh — undo `just deploy` / `just install` on this machine, on any platform, so
# a packaged yolo (Homebrew, the release archive) is the one that runs. `just undeploy`
# runs it; `--dry-run` prints what it would do and changes nothing.
#
# WHAT A SOURCE INSTALL LEAVES, and what this does with each:
#   - $GOBIN/yolo, the `go install` binary: REMOVED, after the host-wide daemons it runs
#     are stopped (a launch ensures them again from whichever yolo it is).
#   - The stable flake-bundle link (~/.local/share/yolo-jail/flake-bundle): REMOVED. A
#     packaged yolo builds from the bundle beside its own binary, and with no link left
#     every staged generation is superseded.
#   - The staged generations (flake-bundles/<stamp>/): KEPT. A running jail executes out of
#     one, so deleting them here would brick it; a later launch's housekeeping reaps each
#     one once no jail mounts it (internal/flakebundle.Reap — by liveness, never by age).
#   - Everything else under ~/.local/share/yolo-jail and ~/.config/yolo-jail: KEPT. It is
#     shared with every other yolo — the Claude login, the broker's CA, each workspace's
#     state, your config — and deleting it would log every jail out.
#
# Host-only: inside a jail the baked /bin/yolo is the image's, and there is no install here
# to undo.

set -eu

dry_run=0
case "${1:-}" in
    "") ;;
    --dry-run|-n) dry_run=1 ;;
    -h|--help)
        echo "usage: undeploy.sh [--dry-run]"
        echo "Removes what 'just deploy' installed (the GOBIN yolo and the staged flake-bundle link),"
        echo "keeping your login, config and workspace state."
        exit 0 ;;
    *) echo "undeploy.sh: unknown argument '$1' (try --help)" >&2; exit 2 ;;
esac

if [ -n "${YOLO_VERSION:-}" ]; then
    echo "✗ 'just undeploy' is host-only — refusing inside a jail (YOLO_VERSION set)." >&2
    echo "  Run it on the host, in the checkout you deployed from." >&2
    exit 1
fi

run() {
    if [ "$dry_run" = 1 ]; then
        echo "  would run: $*"
    else
        "$@"
    fi
}

# The same GOBIN `just install` wrote to: GOBIN, else `go env`, else Go's own default.
gobin="${GOBIN:-}"
if [ -z "$gobin" ] && command -v go >/dev/null 2>&1; then
    gobin="$(go env GOBIN 2>/dev/null || true)"
    [ -n "$gobin" ] || gobin="$(go env GOPATH 2>/dev/null)/bin"
fi
[ -n "$gobin" ] || gobin="$HOME/go/bin"
bin="$gobin/yolo"

# Ask the binary for its bundle path while it still exists (it is the one authority:
# paths.FlakeBundleDir); fall back to that function's value when it is already gone.
bundle=""
if [ -x "$bin" ]; then
    bundle="$("$bin" internal bundle-dir 2>/dev/null | head -n 1 || true)"
fi
[ -n "$bundle" ] || bundle="$HOME/.local/share/yolo-jail/flake-bundle"

removed=0

if [ -x "$bin" ]; then
    echo "Stopping host-wide daemons run by $bin"
    names="$("$bin" host-daemon status --format json 2>/dev/null |
        grep -o '"loophole": *"[^"]*"' | sed 's/.*"\([^"]*\)"$/\1/' || true)"
    for name in $names; do
        # Stopping one that is not running is a no-op; a failure must not strand the
        # binary half-removed, so it is reported and the undeploy continues.
        run "$bin" host-daemon stop "$name" >/dev/null 2>&1 ||
            echo "  ⚠ could not stop $name; stop it with 'yolo host-daemon stop $name'" >&2
    done
    echo "Removing $bin"
    run rm -f "$bin"
    removed=1
else
    echo "No source-installed yolo at $bin"
fi

if [ -L "$bundle" ]; then
    echo "Removing flake-bundle link $bundle"
    run rm -f "$bundle"
    removed=1
elif [ -e "$bundle" ]; then
    # A pre-generations install staged a real directory here, and a running jail may be
    # executing out of it, so it is named rather than deleted.
    echo "⚠ $bundle is a directory from an older install, not a link; left in place." >&2
    echo "  Once no jail is running, remove it with: rm -rf '$bundle'" >&2
else
    echo "No flake-bundle link at $bundle"
fi

if [ "$removed" = 0 ]; then
    echo "Nothing to undeploy."
fi

if [ "$dry_run" = 1 ]; then
    echo "Dry run: nothing was changed."
    exit 0
fi

if [ -n "${YOLO_REPO_ROOT:-}" ]; then
    echo "⚠ YOLO_REPO_ROOT is obsolete and does not select runtime sources." >&2
    echo "  Unset it and remove the export from your shell rc; for development, build a matched bundle" >&2
    echo "  with 'just build-dev-bundle' and invoke its printed launcher path." >&2
fi

resolved="$(command -v yolo 2>/dev/null || true)"
if [ -n "$resolved" ] && [ "$resolved" != "$bin" ]; then
    echo "yolo now resolves to $resolved (run 'hash -r' in open shells)."
else
    echo "No yolo is left on PATH. Install one, e.g.: brew install mschulkind-oss/tap/yolo-jail"
fi
echo "Kept: your login, config and workspace state under ~/.local/share/yolo-jail and ~/.config/yolo-jail."
