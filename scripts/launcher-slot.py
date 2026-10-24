#!/usr/bin/env python3
"""Manage the launcher slot that `just deploy` and `just deploy-dev` install into.

THE SLOT is one path, `<GOBIN>/<name>`: the directory `just install` has always written to
(`go env GOBIN`, else `$(go env GOPATH)/bin`) and a launcher name, `yolo` unless the recipe was
given another. Whatever occupies it is classified before anything is written, and NOTHING is
executed until it has been identified as a yolo build:

  absent     nothing there.
  deployed   a yolo build: an ELF or Mach-O Go binary whose embedded build info names
             github.com/mschulkind-oss/yolo-jail/cmd/yolo as its main package.
  link       a symlink to a yolo build.
  dev        a symlink to THIS checkout's scripts/dev-wrapper.sh (`just deploy-dev`).
  other-dev  a symlink to ANOTHER checkout's scripts/dev-wrapper.sh. Refused.
  legacy     a pre-Go Python console script (the rule internal/hostmigrate applies).
  dangling   a symlink that resolves to nothing, including a dev link whose checkout moved.
  foreign    anything else. Refused, and left untouched.

Every kind but other-dev and foreign is replaced, and the recipe says what it replaced. `deploy`
builds and installs a matched launcher through matched-bundle.py; `deploy-dev` swaps in a symlink
to this checkout's dev wrapper, which builds a matched bundle from the checkout on every run.
"""
from __future__ import annotations

import argparse
import mmap
import os
from pathlib import Path
import shutil
import subprocess
import sys
import uuid

YOLO_MAIN = "github.com/mschulkind-oss/yolo-jail/cmd/yolo"
BUILDINFO_MAGIC = b"\xff Go buildinf:"
# ELF, then Mach-O 64/32 in both byte orders, then a universal (fat) Mach-O.
EXEC_MAGICS = (b"\x7fELF", b"\xcf\xfa\xed\xfe", b"\xfe\xed\xfa\xcf", b"\xce\xfa\xed\xfe",
               b"\xfe\xed\xfa\xce", b"\xca\xfe\xba\xbe")
DEV_WRAPPER = "dev-wrapper.sh"
REPLACEABLE = ("absent", "deployed", "dev", "link", "legacy", "dangling")


class SlotError(RuntimeError):
    """A refusal; its message names the next step."""


def _clean_env() -> dict[str, str]:
    # A probed yolo prints YOLO_VERSION in place of its own version; Git redirection and the
    # retired YOLO_REPO_ROOT must not reach a child either.
    return {k: v for k, v in os.environ.items()
            if not k.startswith("GIT_") and k not in ("YOLO_VERSION", "YOLO_REPO_ROOT")}


def validate_name(name: str) -> str:
    if (not name or name in (".", "..") or "/" in name or name.startswith("-")
            or any(ord(ch) < 0x20 or ord(ch) == 0x7f for ch in name)):
        raise SlotError(f"launcher name {name!r} is not a plain file name; pass one such as yolo-dev")
    return name


def name_arg(name: str) -> str:
    """The recipe argument that names this launcher: nothing for the default."""
    return "" if name == "yolo" else f" {name}"


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


def _uvarint(buf, pos: int) -> tuple[int, int]:
    value = shift = 0
    while True:
        byte = buf[pos]
        pos += 1
        value |= (byte & 0x7F) << shift
        if not byte & 0x80:
            return value, pos
        shift += 7
        if shift > 63:
            raise ValueError("varint overflow")


def go_main_package(path: Path) -> str | None:
    """The main package path in a Go binary's build info, read without running it.

    Only an ELF or Mach-O file is considered, and only the Go 1.18+ inline build-info layout
    (a 16-byte aligned "\\xff Go buildinf:" header whose flags carry bit 2, then the varint-length
    Go version and module info) counts. Anything else is not a Go binary here.
    """
    try:
        with path.open("rb") as handle:
            if handle.read(4) not in EXEC_MAGICS:
                return None
            if os.fstat(handle.fileno()).st_size < 64:
                return None
            with mmap.mmap(handle.fileno(), 0, access=mmap.ACCESS_READ) as buf:
                at = buf.find(BUILDINFO_MAGIC)
                while at != -1:
                    found = _parse_buildinfo(buf, at)
                    if found is not None:
                        return found
                    at = buf.find(BUILDINFO_MAGIC, at + 1)
    except (OSError, ValueError):
        return None
    return None


def _parse_buildinfo(buf, at: int) -> str | None:
    if at % 16 or at + 32 > len(buf) or not buf[at + 15] & 0x2:
        return None
    try:
        size, pos = _uvarint(buf, at + 32)
        version = bytes(buf[pos:pos + size])
        size, pos = _uvarint(buf, pos + size)
        modinfo = bytes(buf[pos:pos + size])
    except (IndexError, ValueError):
        return None
    if not version.startswith(b"go") or len(modinfo) < 33:
        return None
    for line in modinfo[16:-16].decode("utf-8", "replace").splitlines():
        if line.startswith("path\t"):
            return line[len("path\t"):]
    return None


def is_yolo_build(path: Path) -> bool:
    try:
        if not path.is_file() or not os.access(path, os.X_OK):
            return False
    except OSError:
        return False
    return go_main_package(path) == YOLO_MAIN


def version_fields(path: Path) -> dict[str, str]:
    """`<path> --version --full` as a field map; empty when the probe fails.

    Run ONLY on a path is_yolo_build accepted.
    """
    if not is_yolo_build(path):
        return {}
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


def _is_dev_wrapper_target(target: str) -> bool:
    # The same test internal/hostmigrate's KindDevLauncher applies to the link's target.
    target_path = Path(target)
    return target_path.name == DEV_WRAPPER and target_path.parent.name == "scripts"


def classify(slot: Path, repo: Path | None = None) -> tuple[str, str]:
    """(kind, description) of whatever occupies slot, changing and running nothing but a
    yolo build's `--version --full`. With repo, a dev link to any other checkout is other-dev."""
    try:
        st = slot.lstat()
    except FileNotFoundError:
        return "absent", "nothing"
    if slot.is_symlink():
        target = os.readlink(slot)
        absolute = Path(target) if os.path.isabs(target) else slot.parent / target
        if not absolute.exists():
            what = "dev launcher link" if _is_dev_wrapper_target(target) else "symlink"
            return "dangling", f"a dangling {what} to {target}"
        if _is_dev_wrapper_target(target):
            checkout = Path(os.path.realpath(absolute)).parent.parent
            if repo is None or checkout == Path(os.path.realpath(repo)):
                return "dev", f"the dev launcher for the checkout at {checkout}"
            return "other-dev", f"the dev launcher for another checkout, at {checkout}"
        resolved = Path(os.path.realpath(absolute))
        if is_yolo_build(resolved):
            return "link", f"a symlink to {_describe_yolo(resolved)} at {resolved}"
        if _in_python_venv(absolute):
            return "legacy", f"a pre-Go Python yolo-jail console script linking into {absolute.parent.parent}"
        return "foreign", f"a symlink to {target}, which is not a yolo build"
    if slot.is_file():
        if is_yolo_build(slot):
            return "deployed", _describe_yolo(slot)
        if slot.name == "yolo" and _python_shebang(slot):
            return "legacy", "a pre-Go Python yolo script"
        return "foreign", "a file that is not a yolo build"
    kind = "directory" if slot.is_dir() else f"special file (mode {oct(st.st_mode)})"
    return "foreign", f"a {kind}"


def check_slot(slot: Path, mode: str, repo: Path) -> tuple[str, str]:
    kind, desc = classify(slot, repo)
    other = "deploy-dev yolo-dev" if mode == "dev" else "deploy yolo-stable"
    if kind == "other-dev":
        raise SlotError(
            f"{slot} is {desc}; it was left untouched.\n"
            f"  Run `just deploy-dev{name_arg(slot.name)}` in that checkout, or remove the link "
            f"(rm {slot}) and re-run here; or install under another name: just {other}")
    if kind not in REPLACEABLE:
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


def launchers(gobin: Path) -> list[tuple[Path, str, str, str]]:
    """Every yolo launcher in gobin as (path, kind, build kind, description): yolo builds, links
    to them, and dev links. Only identified yolo builds are run (for their build kind)."""
    found = []
    try:
        entries = sorted(p for p in gobin.iterdir() if not p.name.startswith("."))
    except OSError:
        return found
    for entry in entries:
        kind, desc = classify(entry)
        if kind in ("deployed", "link"):
            found.append((entry, kind, version_fields(Path(os.path.realpath(entry))).get("build kind", ""), desc))
        elif kind == "dev":
            found.append((entry, kind, "development", desc))
    return found


def check_no_other_deployed(slot: Path) -> None:
    """Refuse BEFORE the installed bundle moves if another deployed launcher shares it."""
    mine = os.path.realpath(slot)
    for path, _, build_kind, desc in launchers(slot.parent):
        if build_kind != "installed" or os.path.realpath(path) == mine:
            continue
        other = path.name
        raise SlotError(
            f"{path} is another deployed launcher ({desc}). Deployed launchers share ONE installed flake "
            f"bundle, so deploying {slot.name} would make {other} refuse to launch. Nothing was changed. "
            "Choose one:\n"
            f"  just deploy-dev{name_arg(other)}   # make {other} the dev launcher, then re-run "
            f"just deploy{name_arg(slot.name)}\n"
            f"  just deploy{name_arg(other)}       # redeploy {other} instead; {slot.name} then refuses "
            "until it is redeployed or made the dev launcher")


def deploy(repo: Path, home: Path, gobin: Path, name: str, *, installer=None) -> Path:
    """Install a matched deployed launcher at gobin/name, replacing what deploy-dev put there."""
    slot = gobin / validate_name(name)
    kind, desc = check_slot(slot, "deploy", repo)
    check_no_other_deployed(slot)
    report_slot(slot, kind, desc)
    if installer is None:
        def installer():
            # python3 from PATH, as the Justfile runs it, not sys.executable.
            subprocess.run(["python3", str(repo / "scripts/matched-bundle.py"), "install",
                            "--repo", str(repo), "--home", str(home), "--gobin", str(gobin),
                            "--name", name], check=True)
    installer()
    if kind == "dev":
        print(f"Switched {name} from the dev launcher to a deployed one; "
              f"`just deploy-dev{name_arg(name)}` switches it back.")
    elif kind != "absent":
        print(f"Replaced {desc}.")
    return slot


def deploy_dev(repo: Path, gobin: Path, name: str) -> Path:
    """Point gobin/name at this checkout's dev wrapper, replacing what deploy put there."""
    slot = gobin / validate_name(name)
    wrapper = repo.resolve() / "scripts" / DEV_WRAPPER
    if not wrapper.is_file() or not os.access(wrapper, os.X_OK):
        raise SlotError(f"{wrapper} is missing or not executable; run deploy-dev from a yolo-jail checkout")
    kind, desc = check_slot(slot, "dev", repo)
    report_slot(slot, kind, desc)
    gobin.mkdir(parents=True, exist_ok=True)
    staged = gobin / f".{name}-dev-{uuid.uuid4().hex}"
    os.symlink(wrapper, staged)
    try:
        os.replace(staged, slot)
    except OSError:
        staged.unlink(missing_ok=True)
        raise
    print(f"Installed {slot} -> {wrapper}: every run builds (or reuses) a bundle matched to this checkout. "
          f"`just deploy{name_arg(name)}` switches {name} back to a deployed launcher.")
    if kind == "deployed":
        print(f"Replaced the deployed launcher. Its installed flake bundle is kept for any other deployed "
              f"launcher; `just undeploy{name_arg(name)}` removes {name}.")
    elif kind not in ("absent", "dev"):
        print(f"Replaced {desc}.")
    _path_warning(slot)
    return slot


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    sub = parser.add_subparsers(dest="command", required=True)
    for cmd in ("inspect", "path", "list", "deploy", "deploy-dev"):
        p = sub.add_parser(cmd)
        p.add_argument("--name", default="yolo")
        p.add_argument("--gobin", type=Path)
        if cmd in ("inspect", "deploy", "deploy-dev"):
            p.add_argument("--repo", type=Path)
        if cmd == "deploy":
            p.add_argument("--home", type=Path, default=Path.home())
    args = parser.parse_args(argv)
    try:
        if args.command in ("deploy", "deploy-dev") and os.environ.get("YOLO_VERSION"):
            raise SlotError(f"'just {args.command}' is host-only — refusing inside a jail (YOLO_VERSION set). "
                            "Run it on the host; in a jail, test Go changes with: just dev -- <args>")
        gobin = resolve_gobin(args.gobin)
        name = validate_name(args.name)
        repo = getattr(args, "repo", None)
        if args.command in ("deploy", "deploy-dev"):
            repo = (repo or Path.cwd()).resolve()
        if args.command == "path":
            print(gobin / name)
        elif args.command == "inspect":
            kind, desc = classify(gobin / name, repo)
            print(f"{kind}\t{desc}")
        elif args.command == "list":
            for path, kind, build_kind, desc in launchers(gobin):
                print(f"{path}\t{kind}\t{build_kind or '-'}\t{desc}")
        elif args.command == "deploy":
            deploy(repo, args.home, gobin, name)
        else:
            deploy_dev(repo, gobin, name)
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
