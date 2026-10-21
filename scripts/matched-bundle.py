#!/usr/bin/env python3
"""Freeze bounded source inputs and publish matched yolo launcher/helper bundles."""
from __future__ import annotations

import argparse
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import re
import shutil
import stat
import subprocess
import sys
import tempfile
import uuid

INPUT_FILES = {"go.mod", "go.sum", "flake.nix", "flake.lock", "Justfile",
               "scripts/build-go.sh", "scripts/stage-source-bundle.sh",
               "scripts/matched-bundle.py", "scripts/dev-wrapper.sh"}
INPUT_DIRS = ("vendor/", "cmd/", "internal/", "packs/")


def _inside(path: str) -> bool:
    return path in INPUT_FILES or any(path.startswith(root) for root in INPUT_DIRS)


def _binary_lists(script: Path) -> tuple[list[str], list[str]]:
    text = script.read_text()
    values = []
    for name in ("SHIPPED_BINARIES", "GUEST_BINARIES"):
        match = re.search(rf"^{name}=\(([^)]*)\)$", text, re.MULTILINE)
        if not match:
            raise ValueError(f"stage script has no {name} declaration: {script}")
        names = match.group(1).split()
        if not names or len(names) != len(set(names)):
            raise ValueError(f"stage script has an invalid {name} declaration")
        values.append(names)
    return values[0], values[1]


def _platform(root: Path) -> tuple[str, str]:
    goos = subprocess.check_output(["go", "env", "GOOS"], cwd=root, text=True).strip()
    goarch = subprocess.check_output(["go", "env", "GOARCH"], cwd=root, text=True).strip()
    if goos not in ("linux", "darwin"):
        raise RuntimeError(f"matched bundles are supported on Linux and macOS, not {goos}")
    return goos, goarch


def _validate_bundle_contents(bundle: Path, shipped: list[str], guest: list[str],
                              host_os: str, host_arch: str) -> None:
    if not stat.S_ISDIR(bundle.lstat().st_mode):
        raise RuntimeError("incomplete matched bundle: share/yolo-jail is not a directory")
    if {p.name for p in bundle.iterdir()} != {"flake.nix", "flake.lock", "yolo-build-stamp.json", "bin"}:
        raise RuntimeError("incomplete matched bundle: unexpected or missing share/yolo-jail entries")
    bins = bundle / "bin"
    if not stat.S_ISDIR(bins.lstat().st_mode):
        raise RuntimeError("incomplete matched bundle: bin is not a directory")
    expected = {f"linux-{host_arch}": shipped}
    if host_os == "darwin":
        expected[f"darwin-{host_arch}"] = guest
    if {p.name for p in bins.iterdir()} != set(expected):
        raise RuntimeError("incomplete matched bundle: target helper directories do not match this host")
    for dirname, names in expected.items():
        directory = bins / dirname
        if not stat.S_ISDIR(directory.lstat().st_mode):
            raise RuntimeError(f"incomplete matched bundle: {dirname} is not a directory")
        if {p.name for p in directory.iterdir()} != set(names):
            raise RuntimeError(f"incomplete matched bundle: {dirname} does not contain the declared ship set")
        for binary in directory.iterdir():
            mode = binary.lstat().st_mode
            if not stat.S_ISREG(mode) or not mode & 0o111:
                raise RuntimeError(f"incomplete matched bundle: {dirname}/{binary.name} is not an executable file")


def _git_env() -> dict[str, str]:
    # Explicit cwd is not enough for linked worktrees: inherited GIT_DIR,
    # GIT_INDEX_FILE and related state can redirect manifest reads elsewhere.
    return {key: value for key, value in os.environ.items() if not key.startswith("GIT_")}


def _git_paths(repo: Path) -> set[str]:
    git_env = _git_env()
    tracked = subprocess.check_output(["git", "ls-files", "-z", "--cached"], cwd=repo, env=git_env).decode().split("\0")
    untracked = subprocess.check_output(["git", "ls-files", "-z", "--others", "--exclude-standard"], cwd=repo, env=git_env).decode().split("\0")
    candidates = {p for p in tracked + untracked if p and _inside(p) and p != ".git" and not p.startswith(".git/")}
    if not candidates:
        return set()
    result = subprocess.run(["git", "check-ignore", "--no-index", "-z", "--stdin"],
                            cwd=repo, env=git_env, input="\0".join(sorted(candidates)).encode(),
                            stdout=subprocess.PIPE, check=False)
    ignored = set(result.stdout.decode().split("\0"))
    return candidates - ignored


def _check_symlink(repo: Path, rel: str, selected: set[str]) -> str | None:
    source = repo / rel
    if not source.is_symlink():
        return None
    resolved = source.resolve(strict=True)
    try:
        target = resolved.relative_to(repo.resolve()).as_posix()
    except ValueError as exc:
        raise ValueError(f"required input symlink escapes snapshot: {rel} -> {resolved}") from exc
    if target not in selected:
        raise ValueError(f"required input symlink target is not in snapshot: {rel} -> {target}")
    return target


def _copy_repo_file(repo: Path, rel: str, destination: Path) -> int:
    """Copy one selected regular file through a no-follow walk under repo."""
    nofollow = getattr(os, "O_NOFOLLOW", 0)
    directory = getattr(os, "O_DIRECTORY", 0)
    if not nofollow or not directory:
        raise RuntimeError("safe snapshot copying requires O_NOFOLLOW and O_DIRECTORY")
    fd = os.open(repo, os.O_RDONLY | directory | nofollow)
    try:
        parts = PurePosixPath(rel).parts
        for part in parts[:-1]:
            next_fd = os.open(part, os.O_RDONLY | directory | nofollow, dir_fd=fd)
            os.close(fd)
            fd = next_fd
        source_fd = os.open(parts[-1], os.O_RDONLY | nofollow, dir_fd=fd)
        mode = os.fstat(source_fd).st_mode
        if not stat.S_ISREG(mode):
            os.close(source_fd)
            raise ValueError(f"snapshot input is no longer a regular file: {rel}")
        with os.fdopen(source_fd, "rb") as source, destination.open("wb") as target:
            shutil.copyfileobj(source, target)
        return stat.S_IMODE(mode)
    finally:
        os.close(fd)


def collect_inputs(repo: Path) -> list[str]:
    repo = repo.resolve()
    selected = _git_paths(repo)
    # Required top-level build inputs cannot silently disappear from an artifact.
    for rel in ("flake.nix", "flake.lock", "Justfile", "scripts/build-go.sh",
                "scripts/stage-source-bundle.sh", "scripts/matched-bundle.py", "scripts/dev-wrapper.sh", "go.mod"):
        if rel not in selected or not (repo / rel).exists():
            raise ValueError(f"required snapshot input missing: {rel}")
    for rel in sorted(selected):
        source = repo / rel
        if source.is_symlink():
            _check_symlink(repo, rel, selected)
        elif source.exists() and not source.is_file():
            raise ValueError(f"unsupported snapshot input type: {rel}")
    return sorted(rel for rel in selected if (repo / rel).exists())


def _entries(root: Path):
    for path in sorted(root.rglob("*"), key=lambda p: p.relative_to(root).as_posix()):
        rel = path.relative_to(root).as_posix()
        if path.is_symlink():
            yield rel, b"L", os.readlink(path).encode()
        elif path.is_file():
            executable = path.stat().st_mode & 0o111
            yield rel, b"X" if executable else b"F", path.read_bytes()


def snapshot_identity(root: Path) -> str:
    digest = hashlib.sha256()
    for rel, kind, content in _entries(root):
        path_bytes = rel.encode("utf-8")
        digest.update(len(path_bytes).to_bytes(8, "big"))
        digest.update(path_bytes)
        digest.update(kind)
        digest.update(len(content).to_bytes(8, "big"))
        digest.update(content)
    return digest.hexdigest()


def freeze_snapshot(repo: Path, destination: Path) -> str:
    repo = repo.resolve()
    inputs = collect_inputs(repo)
    destination.mkdir(parents=True, exist_ok=False)
    selected = set(inputs)
    for rel in inputs:
        target = destination / rel
        target.parent.mkdir(parents=True, exist_ok=True)
        resolved_target = _check_symlink(repo, rel, selected)
        copy_rel = resolved_target or rel
        mode = _copy_repo_file(repo, copy_rel, target)
        target.chmod(mode)
    return snapshot_identity(destination)


def _run(command, *, cwd: Path, env: dict[str, str], runner=subprocess.run):
    runner(command, cwd=cwd, env=env, check=True)


def assemble(snapshot: Path, destination: Path, *, identity: str, build_kind: str,
             version: str, commit: str, source_dir: str = "", source_branch: str = "",
             runner=subprocess.run) -> Path:
    """Build from the frozen tree into an unpublished, private output directory."""
    snapshot = snapshot.resolve()
    destination.mkdir(parents=True, exist_ok=False)
    host_os, host_arch = _platform(snapshot)
    shipped, guest = _binary_lists(snapshot / "scripts/stage-source-bundle.sh")
    env = _git_env()
    env.pop("YOLO_REPO_ROOT", None)
    env.update({"VERSION": version, "COMMIT": commit, "SNAPSHOT_IDENTITY": identity,
                "BUILD_KIND": build_kind})
    if build_kind == "installed":
        env.update({"SOURCE_DIR": source_dir, "SOURCE_BRANCH": source_branch})
    env.update({"BUILD_GO_CMDS": "yolo", "GOOS": host_os, "GOARCH": host_arch})
    _run([str(snapshot / "scripts/build-go.sh")], cwd=snapshot, env=env, runner=runner)
    launcher = snapshot / "dist-go" / f"{host_os}-{host_arch}" / "yolo"
    if not launcher.is_file():
        raise RuntimeError(f"native launcher build missing: {launcher}")
    bin_dir = destination / "bin"
    bin_dir.mkdir()
    shutil.copy2(launcher, bin_dir / "yolo")
    share = destination / "share" / "yolo-jail"
    stage_env = env.copy()
    stage_env.pop("BUILD_GO_CMDS", None)
    stage_env.pop("SOURCE_DIR", None)
    stage_env.pop("SOURCE_BRANCH", None)
    stage_env["YOLO_BUNDLE_ARCHES"] = host_arch
    stage_env["YOLO_BUNDLE_GUEST_OSES"] = "darwin" if host_os == "darwin" else ""
    _run([str(snapshot / "scripts/stage-source-bundle.sh"), str(share)],
         cwd=snapshot, env=stage_env, runner=runner)
    validate_bundle(destination, identity, build_kind, shipped=shipped, guest=guest,
                    host_os=host_os, host_arch=host_arch)
    return destination


def validate_pair(launcher: Path, bundle: Path, identity: str, build_kind: str) -> None:
    for path in (launcher, bundle / "flake.nix", bundle / "flake.lock", bundle / "yolo-build-stamp.json"):
        try:
            mode = path.lstat().st_mode
        except OSError as exc:
            raise RuntimeError(f"incomplete matched bundle: missing {path}") from exc
        if not stat.S_ISREG(mode):
            raise RuntimeError(f"incomplete matched bundle: {path} is not a regular file")
    if not launcher.lstat().st_mode & 0o111:
        raise RuntimeError("incomplete matched bundle: native launcher is not executable")
    stamp = json.loads((bundle / "yolo-build-stamp.json").read_text())
    if stamp.get("snapshotIdentity") != identity or stamp.get("buildKind") != build_kind:
        raise RuntimeError("incomplete matched bundle: launcher and bundle identity metadata differ")
    env = _git_env()
    env.pop("YOLO_REPO_ROOT", None)
    result = subprocess.run([str(launcher), "--version", "--full"], check=True,
                            capture_output=True, text=True, env=env)
    fields = set(result.stdout.splitlines())
    if f"snapshot identity: {identity}" not in fields or f"build kind: {build_kind}" not in fields:
        raise RuntimeError("incomplete matched bundle: native launcher identity does not match bundle metadata")


def validate_bundle(root: Path, identity: str, build_kind: str, *,
                    shipped: list[str] | None = None, guest: list[str] | None = None,
                    host_os: str | None = None, host_arch: str | None = None) -> None:
    if not stat.S_ISDIR(root.lstat().st_mode) or {p.name for p in root.iterdir()} != {"bin", "share"}:
        raise RuntimeError("incomplete matched bundle: output root must contain only bin and share")
    launcher_dir = root / "bin"
    if not stat.S_ISDIR(launcher_dir.lstat().st_mode) or {p.name for p in launcher_dir.iterdir()} != {"yolo"}:
        raise RuntimeError("incomplete matched bundle: native bin must contain only yolo")
    bundle = root / "share/yolo-jail"
    validate_pair(root / "bin/yolo", bundle, identity, build_kind)
    if shipped is not None and guest is not None and host_os and host_arch:
        _validate_bundle_contents(bundle, shipped, guest, host_os, host_arch)


def _make_readonly(root: Path) -> None:
    for path in sorted(root.rglob("*"), reverse=True):
        if path.is_symlink():
            continue
        mode = path.stat().st_mode
        if path.is_dir():
            path.chmod(0o555)
        else:
            path.chmod((mode & 0o111) | 0o444)
    root.chmod(0o555)


def publish_dev_bundle(home: Path, payload: dict[str, tuple[bytes, int]], *, identity: str) -> Path:
    """Atomically publish a uniquely named home-local development generation."""
    root = home / ".local/share/yolo-jail/dev-bundles"
    root.mkdir(parents=True, exist_ok=True)
    staging = Path(tempfile.mkdtemp(prefix=".building-", dir=root))
    try:
        for rel, (content, mode) in payload.items():
            target = staging / PurePosixPath(rel)
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_bytes(content)
            target.chmod(mode)
        validate_bundle(staging, identity, "development")
        _make_readonly(staging)
        final = root / f"{identity[:16]}-{uuid.uuid4().hex}"
        os.rename(staging, final)
        return final
    except Exception:
        shutil.rmtree(staging, ignore_errors=True)
        raise


def _publish_assembled_bundle(home: Path, assembled: Path, identity: str,
                              shipped: list[str], guest: list[str], host_os: str, host_arch: str) -> Path:
    root = home / ".local/share/yolo-jail/dev-bundles"
    root.mkdir(parents=True, exist_ok=True)
    staging = Path(tempfile.mkdtemp(prefix=".building-", dir=root))
    try:
        shutil.copytree(assembled, staging, dirs_exist_ok=True)
        validate_bundle(staging, identity, "development", shipped=shipped, guest=guest,
                        host_os=host_os, host_arch=host_arch)
        _make_readonly(staging)
        final = root / f"{identity[:16]}-{uuid.uuid4().hex}"
        os.rename(staging, final)
        return final
    except Exception:
        shutil.rmtree(staging, ignore_errors=True)
        raise


def build_dev_bundle(repo: Path, home: Path, *, builder=assemble, runner=subprocess.run) -> Path:
    """Reuse a complete current identity, otherwise publish a fresh immutable build."""
    repo = repo.resolve()
    home = home.resolve()
    version, commit, source_dir, source_branch = _source_stamps(repo)
    root = home / ".local/share/yolo-jail/dev-bundles"
    root.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix="yolo-matched-") as td:
        snapshot = Path(td) / "snapshot"
        identity = freeze_snapshot(repo, snapshot)
        host_os, host_arch = _platform(snapshot)
        shipped, guest = _binary_lists(snapshot / "scripts/stage-source-bundle.sh")
        for candidate in sorted(root.glob(f"{identity[:16]}-*")):
            try:
                validate_bundle(candidate, identity, "development", shipped=shipped, guest=guest,
                                host_os=host_os, host_arch=host_arch)
                return candidate / "bin/yolo"
            except (OSError, ValueError, RuntimeError, json.JSONDecodeError):
                continue
        output = Path(td) / "bundle"
        builder(snapshot, output, identity=identity, build_kind="development",
                version=version, commit=commit, runner=runner)
        published = _publish_assembled_bundle(home, output, identity, shipped, guest, host_os, host_arch)
        return published / "bin/yolo"


def validate_and_publish_dev(home: Path, payload: dict[str, tuple[bytes, int]], identity: str) -> Path:
    try:
        return publish_dev_bundle(home, payload, identity=identity)
    except RuntimeError as exc:
        raise RuntimeError(f"incomplete matched bundle: {exc}") from exc


def publish_install_generation(launcher: Path, bundle: Path, identity: str,
                               env: dict[str, str], runner=subprocess.run, *,
                               shipped: list[str] | None = None, guest: list[str] | None = None,
                               host_os: str | None = None, host_arch: str | None = None) -> str:
    staged = runner([str(launcher), "internal", "bundle-dir", "--stage"], env=env,
                    text=True, capture_output=True, check=True).stdout.strip()
    shutil.copytree(bundle, staged, dirs_exist_ok=True)
    validate_pair(launcher, Path(staged), identity, "installed")
    if shipped is not None and guest is not None and host_os and host_arch:
        _validate_bundle_contents(Path(staged), shipped, guest, host_os, host_arch)
    runner([str(launcher), "internal", "bundle-dir", "--activate", staged], env=env, check=True)
    return staged


def _source_stamps(repo: Path) -> tuple[str, str, str, str]:
    def git(*args):
        try:
            return subprocess.check_output(["git", *args], cwd=repo, env=_git_env(), text=True,
                                           stderr=subprocess.DEVNULL).strip()
        except subprocess.CalledProcessError:
            return ""
    return (git("describe", "--tags", "--dirty", "--always", "--long", "--abbrev=12"),
            git("rev-parse", "--short=12", "HEAD"), str(repo.resolve()),
            git("symbolic-ref", "--short", "-q", "HEAD"))


def _install_guard(env: dict[str, str]) -> None:
    if env.get("YOLO_VERSION"):
        raise RuntimeError("'just install' is host-only — refusing inside a jail (YOLO_VERSION set).")


def _atomic_install_launcher(source: Path, gobin: Path) -> Path:
    gobin.mkdir(parents=True, exist_ok=True)
    staged = gobin / f".yolo-install-{uuid.uuid4().hex}"
    shutil.copy2(source, staged)
    os.replace(staged, gobin / "yolo")
    return gobin / "yolo"


def _resolved_path(path: str | Path) -> str:
    try:
        return subprocess.check_output(["readlink", "-f", str(path)], text=True,
                                       stderr=subprocess.DEVNULL).strip()
    except (OSError, subprocess.CalledProcessError):
        return str(path)


def command(repo: Path, mode: str, *, home: Path, gobin: Path | None = None,
            args: list[str] | None = None, builder=assemble, runner=subprocess.run,
            execer=os.execvpe):
    if mode in ("build-dev", "dev"):
        launcher = build_dev_bundle(repo, home, builder=builder, runner=runner)
        if mode == "build-dev":
            return launcher
        if "YOLO_REPO_ROOT" in os.environ:
            print("Ignoring obsolete YOLO_REPO_ROOT; remove that export and invoke the matched bundle by path.", file=sys.stderr)
        child_env = os.environ.copy()
        child_env.pop("YOLO_REPO_ROOT", None)
        print(f"Matched development bundle: {launcher}")
        child_argv = [str(launcher), *(args or [])]
        execer(child_argv[0], child_argv, child_env)
        return 0
    if mode != "install":
        raise ValueError(f"unknown matched bundle mode: {mode}")
    _install_guard(os.environ)
    version, commit, source_dir, source_branch = _source_stamps(repo)
    if any(ch.isspace() for ch in source_dir):
        print("⚠ checkout path has whitespace — 'yolo update' will not be able to find it", file=sys.stderr)
        source_dir = ""
    state = home / ".local/share/yolo-jail"
    state.mkdir(parents=True, exist_ok=True)
    env = _git_env()
    env.pop("YOLO_REPO_ROOT", None)
    env["HOME"] = str(home)
    with tempfile.TemporaryDirectory(prefix="yolo-matched-") as td:
        work = Path(td)
        snapshot = work / "snapshot"
        identity = freeze_snapshot(repo, snapshot)
        shipped, guest = _binary_lists(snapshot / "scripts/stage-source-bundle.sh")
        host_os, host_arch = _platform(snapshot)
        output = work / "bundle"
        assemble(snapshot, output, identity=identity, build_kind="installed",
                 version=version, commit=commit, source_dir=source_dir,
                 source_branch=source_branch, runner=runner)
        launcher = output / "bin/yolo"
        subprocess.run([str(launcher), "internal", "migrate-host"], env=env, check=True)
        publish_install_generation(launcher, output / "share/yolo-jail", identity, env,
                                   shipped=shipped, guest=guest, host_os=host_os, host_arch=host_arch)
        if gobin is None:
            raw = subprocess.check_output(["go", "env", "GOBIN"], cwd=snapshot, text=True).strip()
            gobin = Path(raw) if raw else Path(subprocess.check_output(["go", "env", "GOPATH"], cwd=snapshot, text=True).strip()) / "bin"
        installed = _atomic_install_launcher(launcher, gobin)
        print(f"Installed to {gobin}")
        resolved = shutil.which("yolo")
        if resolved and _resolved_path(resolved) != _resolved_path(installed):
            print(file=sys.stderr)
            print("⚠ WARNING: this install is NOT the yolo your shell runs.", file=sys.stderr)
            print(f"    installed:  {installed}\n    resolves:   {resolved}", file=sys.stderr)
            print(f"  Every launch will keep using the older one. Put {gobin} ahead of it on PATH (or remove {resolved}), then run 'hash -r'.", file=sys.stderr)
            print(file=sys.stderr)
        if resolved and resolved != str(installed):
            print(f"⚠ PATH resolves yolo to {resolved}, not the copy just installed at {installed}.", file=sys.stderr)
            print(f"  Remove the other install, or put {gobin} earlier in PATH.", file=sys.stderr)
        return installed


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("mode", choices=("build-dev", "dev", "install"))
    parser.add_argument("--repo", type=Path, default=Path.cwd())
    parser.add_argument("--home", type=Path, default=Path.home())
    parser.add_argument("--gobin", type=Path)
    raw = sys.argv[1:]
    if "--" in raw:
        split = raw.index("--")
        args = parser.parse_args(raw[:split])
        forwarded = raw[split + 1:]
    else:
        args = parser.parse_args(raw)
        forwarded = []
    try:
        result = command(args.repo, args.mode, home=args.home, gobin=args.gobin,
                         args=forwarded, execer=os.execvpe)
        if args.mode == "build-dev":
            print(result)
        return result if isinstance(result, int) else 0
    except (OSError, subprocess.CalledProcessError, RuntimeError, ValueError) as exc:
        print(f"matched-bundle: {exc}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
