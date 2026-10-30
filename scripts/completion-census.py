#!/usr/bin/env python3
"""Regenerate scripts/completion-readers.json: which Go tests read repository files outside
their own package directory.

`just done` (scripts/completion-check.py) selects Go tests from the packages whose own files
changed. A test that READS a file it does not import — a doc, a manifest, another package's
source — is invisible to that selection, so this census records those reads, per test, from Go's
own test log (`-test.testlogfile`, the record `go test` uses to invalidate its cache), and the
selector reruns a test whenever a change touches what it read.

Run it after adding or changing a test that reads outside its package directory:

    python3 scripts/completion-census.py

It runs every test of every package once, one process per test function, so it takes minutes,
not seconds. It writes only the readers file. Reads by a test's SUBPROCESSES (git, go build) are
not in the log; that is the same limit Go's own test cache has.

A `stat` of the repository root directory, by any spelling, records nothing; an `open` of it (a
listing) is still recorded as `.`. The output must not depend on where the checkout lives, and
production code stats the jail's own workspace path, the literal `/workspace`, which names the
repository root only in the checkout that IS `/workspace`. The other root stats in a test log
are incidental too: `os.StartProcess` stats a subprocess's working directory, and resolving a
path stats each of its ancestors. Neither reads the root's contents, and a test that does read
them opens the root or the paths under it, which are recorded.

A test that OPENS a path under the literal `/workspace` outside the checkout stops the census.
Production reads the jail's own workspace by that literal (the retire surgery's
`/workspace/mise.toml`, internal/entrypoint), so such a test reads whatever file the machine has
there: in this repository's jail, the live checkout's. The census cannot tell that read from a
real one when it runs from the checkout that IS `/workspace`, where it would record the file,
while every other checkout drops it, so the two outputs differed until each package pointed the
seam at a fixture (`entrypoint.OverrideWorkspaceMisePath` in its TestMain). So the census
refuses, naming every such test, wherever it can see the leak: from any other checkout.
"""
import concurrent.futures as cf
import json
import os
import subprocess
import sys
import tempfile
from pathlib import Path

from go_packages import package_patterns

class CensusError(RuntimeError):
    """A collector failure that must stop regeneration without writing partial output."""


ROOT = Path(__file__).resolve().parent.parent
OUT = ROOT / "scripts" / "completion-readers.json"


def go_packages():
    """(packages with tests, every active Git-visible package directory)."""
    try:
        patterns = package_patterns(ROOT)
    except (OSError, RuntimeError, subprocess.CalledProcessError) as exc:
        raise CensusError("Go package selection failed; repair the Go package graph and rerun "
                          f"scripts/completion-census.py: {exc}") from exc
    if not patterns:
        return [], set()
    try:
        out = subprocess.check_output(
            ["go", "list", "-f", "{{.ImportPath}}\t{{.Dir}}\t{{len .TestGoFiles}}\t{{len .XTestGoFiles}}",
             *patterns], cwd=ROOT, text=True, stderr=subprocess.PIPE)
    except (OSError, subprocess.CalledProcessError, subprocess.TimeoutExpired) as exc:
        detail = getattr(exc, "stderr", b"")
        if isinstance(detail, bytes):
            detail = detail.decode(errors="replace")
        raise CensusError(f"go list failed; fix package loading and rerun scripts/completion-census.py: {detail or exc}") from exc
    pkgs, dirs = [], set()
    try:
        for line in out.splitlines():
            ip, d, t, x = line.split("\t")
            dirs.add(os.path.relpath(d, ROOT))
            if int(t) or int(x):
                pkgs.append((ip, Path(d)))
    except ValueError as exc:
        raise CensusError(f"go list returned an unreadable package record {line!r}; rerun scripts/completion-census.py") from exc
    return pkgs, dirs


def _git_metadata(root):
    for directory in (root, *root.parents):
        if os.path.lexists(directory / ".git"):
            return True
    return bool(os.environ.get("GIT_DIR") or os.environ.get("GIT_WORK_TREE")
                or os.environ.get("GIT_COMMON_DIR"))


def _git_files(root, *selection):
    try:
        result = subprocess.run(["git", "-C", str(root), "ls-files", *selection, "-z"],
                                stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    except OSError as exc:
        raise CensusError(f"could not run Git source inventory; install/repair Git and rerun "
                          f"scripts/completion-census.py: {exc}") from exc
    if result.returncode:
        raise CensusError("Git source inventory failed; repair the checkout and rerun "
                          f"scripts/completion-census.py: {result.stderr.decode(errors='replace').strip()}")
    return {os.fsdecode(path) for path in result.stdout.split(b"\0") if path}


class GitVisibility:
    """Repository paths that are tracked or not ignored, queried live for newly-created paths."""

    def __init__(self, root):
        self.root = Path(root).resolve()
        self.files = None if not _git_metadata(self.root) else _git_files(
            self.root, "--cached", "--others", "--exclude-standard")
        self.dirs = None
        if self.files is not None:
            self.dirs = {"."}
            for path in self.files:
                parent = os.path.dirname(path)
                while parent and parent != ".":
                    self.dirs.add(parent)
                    parent = os.path.dirname(parent)

    def contains(self, path, directory=False):
        if self.files is None:  # An archive has no ignore/tracking authority; keep normal coverage.
            return True
        known = self.dirs if directory else self.files
        if path in known:
            return True
        # Tests may create a real, nonignored source dependency after the initial inventory.
        # Query just that path rather than freezing out newly visible dependencies.
        entries = _git_files(self.root, "--cached", "--others", "--exclude-standard", "--",
                             ":(literal)" + path)
        if entries:
            return True
        if directory:
            # Empty or ignored-only directories are not repository source dependencies. Their
            # unignored parent may still be retained through tracked/nonignored descendants.
            return False
        # Missing nonignored files can be real optional-source dependencies (e.g. os.Stat).
        # Retain them unless Git says ignore rules exclude the path. --no-index still sees
        # ignore rules for force-tracked paths, which were accepted from the cached inventory.
        try:
            result = subprocess.run(["git", "-C", str(self.root), "check-ignore", "--quiet",
                                     "--no-index", "--", path],
                                    stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        except OSError as exc:
            raise CensusError(f"could not run Git ignore check; install/repair Git and rerun "
                              f"scripts/completion-census.py: {exc}") from exc
        if result.returncode == 0:
            return False
        if result.returncode == 1:
            return True
        raise CensusError("Git ignore check failed; repair the checkout and rerun "
                          f"scripts/completion-census.py: {result.stderr.decode(errors='replace').strip()}")


def parse_log(text, pkgdir, pkg_dirs, visibility=None):
    """The Git-visible testlog open/stat entries outside this test's own Go package."""
    opened, listed, stats = set(), set(), set()
    own = os.path.relpath(pkgdir, ROOT)
    visibility = visibility or GitVisibility(ROOT)
    for line in text.splitlines():
        op, _, path = line.partition(" ")
        if op not in ("open", "stat"):
            continue
        r = rel(path, pkgdir)
        if r is None or r == ".git" or r.startswith(".git" + os.sep):
            continue
        if r == "." and op == "stat":
            # A stat of the root itself is not a read of the repository (module docstring).
            continue
        if owner(r, pkg_dirs) == own:
            continue
        is_dir = (ROOT / r).is_dir()
        if not visibility.contains(r, directory=is_dir):
            continue
        if is_dir:
            # A listed or stat'ed directory changes only when a direct child appears or goes.
            listed.add(r)
        elif op == "stat":
            stats.add(r)
        else:
            opened.add(r)
    stats -= opened | listed
    return opened, listed, stats


JAIL_WORKSPACE = "/workspace"


def jail_workspace_opens(text, pkgdir):
    """The paths a test log opens under the literal jail workspace that are not in this checkout
    (module docstring). Stats are not counted: production stats the workspace root, and
    resolving the checkout's own path stats its ancestors."""
    leaks = set()
    for line in text.splitlines():
        op, _, path = line.partition(" ")
        if op != "open":
            continue
        p = os.path.normpath(path if os.path.isabs(path) else os.path.join(pkgdir, path))
        if p != JAIL_WORKSPACE and not p.startswith(JAIL_WORKSPACE + os.sep):
            continue
        if rel(p, pkgdir) is not None:
            continue
        if str(ROOT).startswith(p + os.sep):  # an ancestor of this checkout
            continue
        leaks.add(p)
    return leaks


def refuse_jail_workspace_opens(leaks):
    """Stop the census when any test opened the jail workspace from outside it (module
    docstring); leaks maps (package directory, test) to the paths it opened."""
    if not leaks:
        return
    lines = [f"  {pkg} {name}: {', '.join(sorted(paths))}" for (pkg, name), paths in sorted(leaks.items())]
    raise CensusError(
        "these tests open the machine's own /workspace, not this checkout or a fixture, which "
        "the census cannot tell from a repository read when it runs from the checkout that IS "
        "/workspace:\n" + "\n".join(lines) +
        "\nPoint the production seam at a fixture in the package's TestMain (for the workspace "
        "mise.toml, entrypoint.OverrideWorkspaceMisePath; internal/entrypoint/workspacemise_test.go "
        "shows the shape), then rerun scripts/completion-census.py")


def tracked_dirs(root=ROOT):
    """Tracked files by directory, for compressing stable directory coverage patterns."""
    root = Path(root).resolve()
    if not _git_metadata(root):
        return {}
    files = _git_files(root, "--cached")
    by_dir = {}
    for f in files:
        by_dir.setdefault(os.path.dirname(f) or ".", set()).add(f)
    return by_dir


def rel(p, base):
    """Repository-relative path for a test-log entry, or None when it is outside the tree."""
    if not os.path.isabs(p):
        p = os.path.join(base, p)
    p = os.path.normpath(p)
    root = str(ROOT)
    if p == root:
        return "."
    if not p.startswith(root + os.sep):
        return None
    return os.path.relpath(p, root)


def owner(rel_path, pkg_dirs):
    """The package directory a repository-relative path belongs to: itself or its nearest
    ancestor that is a package directory, or None."""
    d = rel_path
    while d and d != ".":
        if d in pkg_dirs:
            return d
        d = os.path.dirname(d)
    return None


def compress(paths, by_dir):
    """Replace "every tracked file directly in D" with `D/*`, and "every tracked file with
    extension E directly in D" (for .go, every non-test one) with `D/*E`. The selector matches
    `D/*E` against any path in D with that extension, so a `_test.go` change can select a reader
    of non-test sources: more than needed, never less."""
    out = set(paths)
    for d, members in by_dir.items():
        prefix = "" if d == "." else d + "/"
        if len(members) > 1 and members <= out:
            out -= members
            out.add(prefix + "*")
            continue
        by_ext = {}
        for f in members:
            ext = os.path.splitext(f)[1]
            if ext and not f.endswith("_test.go"):
                by_ext.setdefault(ext, set()).add(f)
        for ext, group in by_ext.items():
            if len(group) > 1 and group <= out:
                out -= {f for f in out if os.path.dirname(f) == d and f.endswith(ext)}
                out.add(prefix + "*" + ext)
    return sorted(out)


def compress_listed(listed, by_dir):
    """Replace "every tracked directory at or under D" with `D/**`, outermost first."""
    dirs = set()
    for d in by_dir:
        while d and d != ".":
            dirs.add(d)
            d = os.path.dirname(d)
    out = set(listed)
    for d in sorted(dirs, key=lambda x: x.count("/")):
        tree = {x for x in dirs if x == d or x.startswith(d + "/")}
        if len(tree) > 1 and tree <= out:
            out -= tree
            out.add(d + "/**")
    return sorted(out)


def checked_run(command, cwd, timeout, description):
    try:
        result = subprocess.run(command, cwd=cwd, capture_output=True, text=True, timeout=timeout)
    except subprocess.TimeoutExpired as exc:
        raise CensusError(f"{description} timed out after {timeout}s; resolve the slow/hung collector test "
                          "and rerun scripts/completion-census.py") from exc
    except OSError as exc:
        raise CensusError(f"could not run {description}: {exc}; repair the toolchain and rerun "
                          "scripts/completion-census.py") from exc
    if result.returncode:
        stdout = result.stdout if result.stdout else "<empty>"
        stderr = result.stderr if result.stderr else "<empty>"
        detail = (f"command argv: {result.args!r}\n"
                  f"direct return code: {result.returncode}\n"
                  f"stdout (verbatim):\n{stdout}\n"
                  f"stderr (verbatim):\n{stderr}")
        raise CensusError(f"{description} failed:\n{detail}; fix the failing test/collector and rerun "
                          "scripts/completion-census.py")
    return result


def list_tests(binary, cwd):
    result = checked_run([str(binary), "-test.list", ".*"], cwd, 700, "Go test listing")
    return [name for name in result.stdout.split() if name[:1].isupper()]


def run_test(binary, cwd, name, log):
    checked_run([str(binary), "-test.short", "-test.run", f"^{name}$",
                 f"-test.testlogfile={log}", "-test.timeout=600s"],
                cwd, 700, f"Go test {cwd} {name}")
    try:
        return log.read_text(errors="replace")
    except OSError as exc:
        raise CensusError(f"Go test {cwd} {name} did not leave its test log: {exc}; "
                          "rerun scripts/completion-census.py") from exc


def main():
    jobs_n = int(os.environ.get("CENSUS_JOBS", str(max(2, (os.cpu_count() or 4) // 2))))
    pkgs, pkg_dirs = go_packages()
    by_dir = tracked_dirs()
    visibility = GitVisibility(ROOT)
    with tempfile.TemporaryDirectory(prefix="completion-census-") as tmp:
        tmp = Path(tmp)

        def build(i):
            b = tmp / f"{i}.test"
            checked_run(["go", "test", "-c", "-o", str(b), pkgs[i][0]], ROOT, 700,
                        f"Go test compile {pkgs[i][0]}")
            return b

        with cf.ThreadPoolExecutor(jobs_n) as ex:
            bins = list(ex.map(build, range(len(pkgs))))
        jobs = []
        for i, (ip, d) in enumerate(pkgs):
            jobs += [(i, n) for n in list_tests(bins[i], d)]
        print(f"census: {len(pkgs)} packages, {len(jobs)} tests, {jobs_n} at a time", flush=True)

        def run(job):
            i, name = job
            ip, d = pkgs[i]
            log = tmp / f"{i}-{name}.log"
            text = run_test(bins[i], d, name, log)
            return i, name, parse_log(text, d, pkg_dirs, visibility), jail_workspace_opens(text, d)

        readers, sets, index, leaks = {}, [], {}, {}
        with cf.ThreadPoolExecutor(jobs_n) as ex:
            for k, (i, name, (opened, listed, stats), leaked) in enumerate(ex.map(run, jobs)):
                if leaked:
                    leaks[(os.path.relpath(pkgs[i][1], ROOT), name)] = leaked
                if opened or listed or stats:
                    pkg = os.path.relpath(pkgs[i][1], ROOT)
                    entry = {"read": compress(opened | stats, by_dir), "listed": compress_listed(listed, by_dir)}
                    key = json.dumps(entry, sort_keys=True)
                    if key not in index:
                        index[key] = len(sets)
                        sets.append(entry)
                    readers.setdefault(pkg, {})[name] = index[key]
                if k and k % 1000 == 0:
                    print(f"census: {k}/{len(jobs)}", flush=True)
    refuse_jail_workspace_opens(leaks)  # before any output is written
    doc = {
        "about": "Generated by scripts/completion-census.py; read by scripts/completion-check.py. "
                 "readers: package directory -> test -> index into sets. A set names the repository "
                 "paths outside the package directory that are tracked or nonignored at collection time. "
                 "Ignored-only generated paths are excluded; `D/*` is every tracked file directly in D, "
                 "`D/*.E` every one with that extension; `D/**` is D and every tracked directory under it. "
                 "Regenerate, do not edit.",
        "sets": sets,
        "readers": {p: dict(sorted(t.items())) for p, t in sorted(readers.items())},
    }
    OUT.write_text(json.dumps(doc, indent=0, sort_keys=False) + "\n")
    print(f"census: wrote {OUT.relative_to(ROOT)} ({sum(len(t) for t in readers.values())} reading tests "
          f"in {len(readers)} packages)")


if __name__ == "__main__":
    try:
        sys.exit(main())
    except CensusError as exc:
        print(f"completion census: {exc}", file=sys.stderr)
        sys.exit(1)
