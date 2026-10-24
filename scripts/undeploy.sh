#!/bin/sh
# undeploy.sh — undo `just deploy` / `just install` on this machine, on any platform, so
# a packaged yolo (Homebrew, the release archive) is the one that runs. `just undeploy`
# runs it; `--dry-run` prints what it would do and changes nothing. A launcher name
# (`just undeploy yolo-dev`) removes $GOBIN/<name> instead of $GOBIN/yolo, and only when
# scripts/launcher-slot.py identifies it as a yolo build, a link to one, or a dev link.
#
# WHAT A SOURCE INSTALL LEAVES, and what this does with each:
#   - $GOBIN/yolo, the installed binary: REMOVED, after the host-wide daemons it runs are
#     stopped (a launch ensures them again from whichever yolo it is). It is run only once
#     launcher-slot.py has identified it as a yolo build. A `just deploy-dev` link to a
#     checkout's scripts/dev-wrapper.sh, or a pre-Go Python script, is REMOVED without being
#     run; anything else is refused with nothing changed.
#   - The stable flake-bundle link (~/.local/share/yolo-jail/flake-bundle): REMOVED, unless
#     another deployed launcher in $GOBIN still uses it. A packaged yolo builds from the
#     bundle beside its own binary, and with no link left every staged generation is
#     superseded.
#   - Other yolo launchers in $GOBIN (`just deploy <name>`, `just deploy-dev <name>`): KEPT
#     and listed, each with the command that removes it.
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
name=""
for arg in "$@"; do
    case "$arg" in
        --dry-run|-n) dry_run=1 ;;
        -h|--help)
            echo "usage: undeploy.sh [--dry-run] [name]"
            echo "Removes what 'just deploy' or 'just deploy-dev' installed (the GOBIN launcher, yolo unless"
            echo "named, and the staged flake-bundle link), keeping your login, config and workspace state."
            exit 0 ;;
        -*) echo "undeploy.sh: unknown argument '$arg' (try --help)" >&2; exit 2 ;;
        *)
            if [ -n "$name" ]; then
                echo "undeploy.sh: one launcher name at a time, not '$name' and '$arg'" >&2; exit 2
            fi
            case "$arg" in
                */*|.|..) echo "undeploy.sh: '$arg' is not a plain launcher name, such as yolo-dev" >&2; exit 2 ;;
            esac
            name="$arg" ;;
    esac
done

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
bin="$gobin/${name:-yolo}"

# The slot helper identifies yolo's launchers without running anything that is not one.
helper="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)/launcher-slot.py"
have_helper=0
if command -v python3 >/dev/null 2>&1 && [ -f "$helper" ]; then
    have_helper=1
fi
slot() { python3 "$helper" "$@" --gobin "$gobin"; }

# Identify what holds the slot before anything runs: only an identified yolo build is ever
# executed (for its bundle path and to stop its host daemons). A dev link, a legacy Python
# script or a dangling link is removed or noted without being run; anything else is refused.
kind=""
if [ "$have_helper" = 1 ]; then
    kind="$(slot inspect --name "${name:-yolo}" | cut -f1)"
    if [ "$kind" = dangling ]; then
        case "$(readlink "$bin")" in */scripts/dev-wrapper.sh) kind=dev ;; esac
    fi
    case "$kind" in
        absent|deployed|link|dev|legacy|dangling) ;;
        *)
            echo "✗ $bin is not a yolo launcher that 'just deploy' or 'just deploy-dev' installed; nothing was changed." >&2
            echo "  Inspect it (ls -l '$bin'), remove it by hand if it is yours to remove, and re-run 'just undeploy${name:+ $name}'." >&2
            exit 1 ;;
    esac
elif [ -n "$name" ] && { [ -e "$bin" ] || [ -L "$bin" ]; }; then
    echo "✗ undeploying a named launcher needs python3 to identify $bin first; nothing was changed." >&2
    echo "  Install python3 and re-run, or remove it by hand once you have checked it: ls -l '$bin'" >&2
    exit 1
elif [ -L "$bin" ]; then
    # No python3: the one thing recognizable from the shell is a dev link.
    case "$(readlink "$bin")" in */scripts/dev-wrapper.sh) kind=dev ;; esac
fi
runnable=0
case "$kind" in
    deployed|link) runnable=1 ;;
    "") [ -x "$bin" ] && runnable=1 ;;
esac

# Ask the binary for its bundle path while it still exists (it is the one authority:
# paths.FlakeBundleDir); fall back to that function's value when it is already gone.
# A `just deploy-dev` link is never run: the dev wrapper would build a whole bundle just to
# answer, and the host daemons a jail started end with that jail.
bundle=""
if [ "$runnable" = 1 ]; then
    bundle="$("$bin" internal bundle-dir 2>/dev/null | head -n 1 || true)"
fi
[ -n "$bundle" ] || bundle="$HOME/.local/share/yolo-jail/flake-bundle"

removed=0

if [ "$kind" = dev ]; then
    echo "Removing the dev launcher link $bin (-> $(readlink "$bin"))"
    run rm -f "$bin"
    removed=1
elif [ "$kind" = legacy ]; then
    echo "Removing the pre-Go Python launcher $bin"
    run rm -f "$bin"
    removed=1
elif [ "$kind" = dangling ]; then
    echo "⚠ $bin is a dangling symlink to $(readlink "$bin"); left in place." >&2
    echo "  Remove it with: rm '$bin'" >&2
elif [ "$runnable" = 1 ]; then
    echo "Stopping host-wide daemons run by $bin"
    names="$("$bin" host-daemon status --format json 2>/dev/null |
        grep -o '"loophole": *"[^"]*"' | sed 's/.*"\([^"]*\)"$/\1/' || true)"
    for daemon in $names; do
        # Stopping one that is not running is a no-op; a failure must not strand the
        # binary half-removed, so it is reported and the undeploy continues.
        run "$bin" host-daemon stop "$daemon" >/dev/null 2>&1 ||
            echo "  ⚠ could not stop $daemon; stop it with 'yolo host-daemon stop $daemon'" >&2
    done
    echo "Removing $bin"
    run rm -f "$bin"
    removed=1
else
    echo "No source-installed launcher at $bin"
fi

# Every other yolo launcher in GOBIN, as "path<TAB>kind<TAB>build kind<TAB>description".
tab="$(printf '\t')"
others=""
if [ "$have_helper" = 1 ]; then
    others="$(slot list | awk -F "$tab" -v bin="$bin" '$1 != bin' || true)"
fi
users="$(printf '%s\n' "$others" | awk -F "$tab" '$3 == "installed" { print $1 }')"

if [ -n "$users" ] && [ -L "$bundle" ]; then
    echo "Kept flake-bundle link $bundle: still used by the deployed launcher(s):"
    printf '%s\n' "$users" | sed 's/^/  /'
elif [ -L "$bundle" ]; then
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

if [ -z "$name" ] && [ -n "$others" ]; then
    echo "Other yolo launchers are still in $gobin:"
    printf '%s\n' "$others" | while IFS="$tab" read -r path _ _ desc; do
        echo "  $path: $desc. Remove it with: just undeploy $(basename "$path")"
    done
fi

launcher="${name:-yolo}"
resolved="$(command -v "$launcher" 2>/dev/null || true)"
if [ -n "$resolved" ] && [ "$resolved" != "$bin" ]; then
    echo "$launcher now resolves to $resolved (run 'hash -r' in open shells)."
elif [ -z "$name" ]; then
    echo "No yolo is left on PATH. Install one, e.g.: brew install mschulkind-oss/tap/yolo-jail"
fi
echo "Kept: your login, config and workspace state under ~/.local/share/yolo-jail and ~/.config/yolo-jail."
