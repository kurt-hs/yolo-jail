#!/usr/bin/env python3
"""Manage the launcher slot that `just deploy` and `just deploy-dev` install into.

THE SLOT is one path, `<GOBIN>/<name>`: the directory `just install` has always written to
(`go env GOBIN`, else `$(go env GOPATH)/bin`) and a launcher name, `yolo` unless the recipe was
given another. Whatever occupies it is classified before anything is written:

  absent    nothing there.
  deployed  a yolo binary (a `just deploy`, or any other build of this module).
  dev       a symlink to a checkout's scripts/dev-wrapper.sh (`just deploy-dev`), live or dangling.
  link      a symlink to a yolo binary.
  legacy    a pre-Go Python console script (the same rule internal/hostmigrate applies).
  dangling  a symlink that resolves to nothing.
  foreign   anything else. NEVER replaced: the recipe refuses and names the next step.

Every kind but foreign is replaced, and the recipe says what it replaced. `deploy` builds and
installs a matched launcher through matched-bundle.py; `deploy-dev` swaps in a symlink to this
checkout's dev wrapper, which builds a matched bundle from the checkout on every run.
"""
from __future__ import annotations

import argparse
import os
from pathlib import Path
import shutil
import subprocess
import sys
import uuid

MODULE_MARKER = b"github.com/mschulkind-oss/yolo-jail"
DEV_WRAPPER = "dev-wrapper.sh"
LEGACY_NAMES = ("yolo", "yolo-ps", "yolo-host-processes", "yolo-claude-oauth-broker-host")
REPLACEABLE = ("absent", "deployed", "dev", "link", "legacy", "dangling")


class SlotError(RuntimeError):
    """A refusal; its message names the next step."""


def _clean_env() -> dict[str, str]:
    # A probed yolo prints YOLO_VERSION in place of its own version; Git redirection and the
    # retired YOLO_REPO_ROOT must not reach a child either.
    return {k: v for k, v in os.environ.items()
            if not k.startswith("GIT_") and k not in ("YOLO_VERSION", "YOLO_REPO_ROOT")}


def validate_name(name: str) -> str:
    if not name or name in (".", "..") or "/" in name or name.startswith("-") or "\0" in name:
        raise SlotError(f"launcher name {name!r} is not a plain file name; pass one such as yolo-dev")
    return name


def resolve_gobin(explicit: Path | None = None) -> Path:
    """The directory `just install` writes to: GOBIN, else GOPATH/bin, as `go env` reports."""
    if explicit is not None:
        return explicit
    env = _clean_env()
    try:
        raw = subprocess.check_output(["go", "env", "GOBIN"], env=env, text=True).strip()
        if raw:
            return Path(raw)
        return Path(subprocess.check_output(["go", "env", "GOPATH"], env=env, text=True).strip()) / "bin"
    except (OSError, subprocess.CalledProcessError) as exc:
        raise SlotError(f"cannot ask Go for GOBIN ({exc}); install Go (`just setup`) or pass --gobin") from exc


def _is_yolo_binary(path: Path) -> bool:
    try:
        if not path.is_file() or not os.access(path, os.X_OK):
            return False
        tail = b""
        with path.open("rb") as handle:
            while chunk := handle.read(1 << 20):
                if MODULE_MARKER in tail + chunk:
                    return True
                tail = chunk[-len(MODULE_MARKER):]
    except OSError:
        return False
    return False


def version_fields(path: Path) -> dict[str, str]:
    """`<path> --version --full` as a field map; empty when the probe fails."""
    try:
        out = subprocess.run([str(path), "--version", "--full"], env=_clean_env(), text=True,
                             capture_output=True, timeout=30, check=True).stdout
    except (OSError, subprocess.SubprocessError):
        return {}
    fields = {}
    for line in out.splitlines():
        key, sep, value = line.partition(": ")
        if sep:
            fields[key.strip()] = value.strip()
    return fields


def _describe_yolo(path: Path) -> str:
    fields = version_fields(path)
    kind, stamp = fields.get("build kind", ""), fields.get("build stamp", "")
    if kind == "installed":
        return f"the deployed yolo launcher {stamp}".rstrip()
    if kind == "development":
        return f"a yolo development build {stamp}".rstrip()
    if stamp:
        return f"a yolo binary {stamp}"
    return "a yolo binary"


def _in_python_venv(target: Path) -> bool:
    return target.parent.name in ("bin", "Scripts") and (target.parent.parent / "pyvenv.cfg").exists()


def _python_shebang(path: Path) -> bool:
    try:
        with path.open("rb") as handle:
            line = handle.readline(256)
    except OSError:
        return False
    return line.startswith(b"#!") and b"python" in line


def classify(slot: Path) -> tuple[str, str]:
    """(kind, description) of whatever occupies slot, changing nothing."""
    try:
        st = slot.lstat()
    except FileNotFoundError:
        return "absent", "nothing"
    if slot.is_symlink():
        target = os.readlink(slot)
        absolute = Path(target) if os.path.isabs(target) else slot.parent / target
        if Path(target).name == DEV_WRAPPER:
            if absolute.exists():
                checkout = Path(os.path.realpath(absolute)).parent.parent
                return "dev", f"the dev launcher for the checkout at {checkout}"
            return "dev", f"a dangling dev launcher link to {target}"
        if not absolute.exists():
            return "dangling", f"a dangling symlink to {target}"
        resolved = Path(os.path.realpath(absolute))
        if _is_yolo_binary(resolved):
            return "link", f"a symlink to {_describe_yolo(resolved)} at {resolved}"
        if _in_python_venv(absolute):
            return "legacy", f"a pre-Go Python yolo-jail console script linking into {absolute.parent.parent}"
        return "foreign", f"a symlink to {target}, which is not yolo"
    if slot.is_file():
        if _is_yolo_binary(slot):
            return "deployed", _describe_yolo(slot)
        if slot.name == "yolo" and _python_shebang(slot):
            return "legacy", "a pre-Go Python yolo script"
        return "foreign", "a file that is not a yolo build"
    kind = "directory" if slot.is_dir() else f"special file (mode {oct(st.st_mode)})"
    return "foreign", f"a {kind}"


def check_slot(slot: Path, mode: str) -> tuple[str, str]:
    kind, desc = classify(slot)
    if kind not in REPLACEABLE:
        other = "deploy-dev yolo-dev" if mode == "dev" else "deploy yolo-stable"
        raise SlotError(
            f"{slot} holds {desc}; it is not something `just deploy` or `just deploy-dev` installed, "
            "so it was left untouched.\n"
            f"  Inspect it (ls -l {slot}), move it aside if it is yours to replace, and re-run; or keep it "
            f"and install under another name: just {other}")
    return kind, desc


def report_slot(slot: Path, kind: str, desc: str) -> None:
    if kind == "absent":
        print(f"Launcher slot {slot}: empty.")
    else:
        print(f"Launcher slot {slot}: replacing {desc}.")


def _path_warning(slot: Path) -> None:
    resolved = shutil.which(slot.name)
    if resolved is None:
        print(f"⚠ {slot.parent} is not on PATH, so `{slot.name}` does not run this launcher yet. "
              f"Add {slot.parent} to PATH, then run 'hash -r'.", file=sys.stderr)
    elif os.path.abspath(resolved) != os.path.abspath(slot):
        print(f"⚠ PATH resolves {slot.name} to {resolved}, not {slot}. "
              f"Remove that one or put {slot.parent} earlier in PATH, then run 'hash -r'.", file=sys.stderr)


def _sibling_deployed(gobin: Path, name: str, identity: str) -> list[Path]:
    """Other deployed launchers in gobin built from another snapshot."""
    found = []
    try:
        entries = sorted(gobin.iterdir())
    except OSError:
        return found
    for entry in entries:
        if entry.name == name or not entry.name.startswith("yolo") or entry.is_symlink():
            continue
        if not _is_yolo_binary(entry):
            continue
        fields = version_fields(entry)
        if fields.get("build kind") == "installed" and fields.get("snapshot identity") != identity:
            found.append(entry)
    return found


def deploy(repo: Path, home: Path, gobin: Path, name: str, *, installer=None) -> Path:
    """Install a matched deployed launcher at gobin/name, replacing what deploy-dev put there."""
    slot = gobin / validate_name(name)
    kind, desc = check_slot(slot, "deploy")
    report_slot(slot, kind, desc)
    if installer is None:
        def installer():
            # python3 from PATH, as the Justfile runs it, not sys.executable.
            subprocess.run(["python3", str(repo / "scripts/matched-bundle.py"), "install",
                            "--repo", str(repo), "--home", str(home), "--gobin", str(gobin),
                            "--name", name], check=True)
    installer()
    if kind == "dev":
        print(f"Switched {slot.name} from the dev launcher to a deployed one.")
    elif kind != "absent":
        print(f"Replaced {desc}.")
    identity = version_fields(slot).get("snapshot identity", "")
    for other in _sibling_deployed(gobin, name, identity):
        print(f"⚠ {other} is another deployed launcher. Deployed launchers share ONE installed flake "
              f"bundle, so it now refuses to launch. Make it the dev launcher (just deploy-dev {other.name}) "
              f"or remove it (rm {other}).", file=sys.stderr)
    return slot


def deploy_dev(repo: Path, gobin: Path, name: str) -> Path:
    """Point gobin/name at this checkout's dev wrapper, replacing what deploy put there."""
    slot = gobin / validate_name(name)
    wrapper = repo.resolve() / "scripts" / DEV_WRAPPER
    if not wrapper.is_file() or not os.access(wrapper, os.X_OK):
        raise SlotError(f"{wrapper} is missing or not executable; run deploy-dev from a yolo-jail checkout")
    kind, desc = check_slot(slot, "dev")
    report_slot(slot, kind, desc)
    gobin.mkdir(parents=True, exist_ok=True)
    staged = gobin / f".{name}-dev-{uuid.uuid4().hex}"
    os.symlink(wrapper, staged)
    try:
        os.replace(staged, slot)
    except OSError:
        staged.unlink(missing_ok=True)
        raise
    print(f"Installed {slot} -> {wrapper}: every run builds (or reuses) a bundle matched to this checkout.")
    if kind == "deployed":
        print("Replaced the deployed launcher. Its installed flake bundle is kept for any other deployed "
              "launcher; `just deploy` switches back, and `just undeploy` removes both.")
    elif kind not in ("absent", "dev"):
        print(f"Replaced {desc}.")
    _path_warning(slot)
    return slot


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    sub = parser.add_subparsers(dest="command", required=True)
    for cmd in ("inspect", "path", "deploy", "deploy-dev"):
        p = sub.add_parser(cmd)
        p.add_argument("--name", default="yolo")
        p.add_argument("--gobin", type=Path)
        if cmd in ("deploy", "deploy-dev"):
            p.add_argument("--repo", type=Path, default=Path.cwd())
        if cmd == "deploy":
            p.add_argument("--home", type=Path, default=Path.home())
    args = parser.parse_args(argv)
    try:
        if args.command in ("deploy", "deploy-dev") and os.environ.get("YOLO_VERSION"):
            raise SlotError(f"'just {args.command}' is host-only — refusing inside a jail (YOLO_VERSION set). "
                            "Run it on the host; in a jail, test Go changes with: just dev -- <args>")
        gobin = resolve_gobin(args.gobin)
        name = validate_name(args.name)
        if args.command == "path":
            print(gobin / name)
        elif args.command == "inspect":
            kind, desc = classify(gobin / name)
            print(f"{kind}\t{desc}")
        elif args.command == "deploy":
            deploy(args.repo.resolve(), args.home, gobin, name)
        else:
            deploy_dev(args.repo, gobin, name)
        return 0
    except SlotError as exc:
        print(f"✗ {exc}", file=sys.stderr)
        return 1
    except subprocess.CalledProcessError as exc:
        print(f"✗ the install step failed (exit {exc.returncode}), so the launcher in the slot was not replaced; "
              "fix the error above and re-run.", file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
