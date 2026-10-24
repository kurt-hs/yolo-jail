#!/usr/bin/env python3
"""Regressions for launcher-slot.py: classifying, replacing and refusing what holds the slot.

A yolo build is a real Go binary here: testdata/fake-yolo built as the main package
github.com/mschulkind-oss/yolo-jail/cmd/yolo of a throwaway module, so it carries the same build
info a real yolo build does. Nothing else may be classified as yolo's, and nothing is run before
it is.
"""
import atexit
import contextlib
import importlib.util
import io
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

HERE = Path(__file__).resolve().parent
SPEC = importlib.util.spec_from_file_location("launcher_slot", HERE / "launcher-slot.py")
ls = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(ls)

_BUILDS = tempfile.TemporaryDirectory()
atexit.register(_BUILDS.cleanup)
_BUILT: dict[tuple, Path] = {}


def _build(main: str, kind: str, stamp: str, identity: str) -> Path:
    """Build testdata/fake-yolo as module path <main>, once per variant (default GOCACHE)."""
    key = (main, kind, stamp, identity)
    if key not in _BUILT:
        mod = Path(_BUILDS.name) / "mod"
        pkg = mod / main
        pkg.mkdir(parents=True, exist_ok=True)
        (mod / "go.mod").write_text("module github.com/mschulkind-oss/yolo-jail\n\ngo 1.22\n")
        shutil.copy2(HERE / "testdata/fake-yolo/main.go", pkg / "main.go")
        out = Path(_BUILDS.name) / f"bin-{len(_BUILT)}"
        env = {k: v for k, v in os.environ.items() if not k.startswith("GIT_")}
        env.update(GOWORK="off", GOTOOLCHAIN="local", GOFLAGS="-mod=mod", CGO_ENABLED="0")
        ldflags = f"-X main.kind={kind} -X main.stamp={stamp} -X main.identity={identity}"
        subprocess.run(["go", "build", "-trimpath", "-ldflags", ldflags, "-o", str(out), f"./{main}"],
                       cwd=mod, env=env, check=True)
        _BUILT[key] = out
    return _BUILT[key]


def fake_yolo(path: Path, *, kind="installed", identity="a" * 64, stamp="v1-0-gabc", main="cmd/yolo"):
    """A yolo build at path that answers `--version --full` with these fields."""
    path.parent.mkdir(parents=True, exist_ok=True)
    shutil.copy2(_build(main, kind, stamp, identity), path)
    return path


def wrapper_script(path: Path, sentinel: Path) -> Path:
    """The case the old marker test accepted: a script that names yolo's module and execs a
    dev wrapper. It is not a yolo build, and it records any run in sentinel."""
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text("#!/bin/sh\n# github.com/mschulkind-oss/yolo-jail/cmd/yolo\n"
                    f"echo ran >>'{sentinel}'\n"
                    'exec ~/ghq/github.com/mschulkind-oss/yolo-jail/scripts/dev-wrapper.sh "$@"\n')
    path.chmod(0o755)
    return path


def checkout(root: Path, name="checkout with spaces") -> Path:
    repo = root / name
    (repo / "scripts").mkdir(parents=True)
    wrapper = repo / "scripts/dev-wrapper.sh"
    wrapper.write_text("#!/usr/bin/env python3\n")
    wrapper.chmod(0o755)
    for script in ("launcher-slot.py", "undeploy.sh"):
        shutil.copy2(HERE / script, repo / "scripts" / script)
    return repo


def quiet(fn, *args, **kwargs):
    out, err = io.StringIO(), io.StringIO()
    with contextlib.redirect_stdout(out), contextlib.redirect_stderr(err):
        result = fn(*args, **kwargs)
    return result, out.getvalue(), err.getvalue()


class Fixture(unittest.TestCase):
    def setUp(self):
        self.td = tempfile.TemporaryDirectory()
        self.root = Path(self.td.name)
        self.gobin = self.root / "gobin"
        self.gobin.mkdir()
        self.repo = checkout(self.root)
        self.wrapper = self.repo.resolve() / "scripts/dev-wrapper.sh"
        self.sentinel = self.root / "ran.log"

    def tearDown(self):
        self.td.cleanup()

    def assert_never_ran(self):
        self.assertFalse(self.sentinel.exists(), "a file that is not a yolo build was executed")


class ClassifyTests(Fixture):
    def kind(self, name):
        return ls.classify(self.gobin / name, self.repo)[0]

    def test_every_kind(self):
        fake_yolo(self.gobin / "deployed")
        os.symlink(self.repo / "scripts/dev-wrapper.sh", self.gobin / "dev")
        other = checkout(self.root, "another checkout")
        os.symlink(other / "scripts/dev-wrapper.sh", self.gobin / "otherdev")
        os.symlink(self.root / "gone/scripts/dev-wrapper.sh", self.gobin / "devgone")
        os.symlink(fake_yolo(self.root / "elsewhere/yolo"), self.gobin / "link")
        os.symlink(self.root / "nothing", self.gobin / "dangling")
        venv = self.root / "venv"
        (venv / "bin").mkdir(parents=True)
        (venv / "pyvenv.cfg").write_text("")
        (venv / "bin/yolo").write_text("#!/usr/bin/env python3\n")
        os.symlink(venv / "bin/yolo", self.gobin / "yolo-ps")
        os.symlink(venv / "bin/yolo", self.gobin / "venvlink")
        (self.gobin / "yolo-dev").write_text("#!/usr/bin/env python3\nimport yolo_jail\n")
        (self.gobin / "yolo").write_text("#!/usr/bin/env python3\nimport yolo_jail\n")
        (self.gobin / "pyother").write_text("#!/usr/bin/env python3\n")
        (self.gobin / "shell").write_text("#!/bin/sh\necho hi\n")
        (self.gobin / "shell").chmod(0o755)
        wrapper_script(self.gobin / "wrapper", self.sentinel)
        fake_yolo(self.gobin / "othergo", main="cmd/other")
        marker = self.gobin / "elfmarker"
        marker.write_bytes(b"\x7fELF" + b"\0" * 60 + ls.YOLO_MAIN.encode() + b"\0" * 4096)
        marker.chmod(0o755)
        (self.gobin / "dir").mkdir()
        (self.root / "plain").write_text("data")
        os.symlink(self.root / "plain", self.gobin / "otherlink")
        # A dev-wrapper.sh that is not in a scripts/ directory is not a dev link.
        (self.root / "bin").mkdir()
        (self.root / "bin/dev-wrapper.sh").write_text("#!/bin/sh\n")
        os.symlink(self.root / "bin/dev-wrapper.sh", self.gobin / "notscripts")
        want = {"absent": "absent", "deployed": "deployed", "dev": "dev", "otherdev": "other-dev",
                "devgone": "dangling", "link": "link", "dangling": "dangling", "yolo-ps": "legacy",
                "venvlink": "foreign", "yolo": "legacy", "yolo-dev": "foreign", "pyother": "foreign", "shell": "foreign", "wrapper": "foreign",
                "othergo": "foreign", "elfmarker": "foreign", "dir": "foreign", "otherlink": "foreign",
                "notscripts": "foreign"}
        self.assertEqual({name: self.kind(name) for name in want}, want)
        self.assert_never_ran()

    def test_the_build_info_names_the_main_package(self):
        self.assertEqual(ls.go_main_package(fake_yolo(self.root / "y")), ls.YOLO_MAIN)
        self.assertEqual(ls.go_main_package(fake_yolo(self.root / "o", main="cmd/other")),
                         "github.com/mschulkind-oss/yolo-jail/cmd/other")
        self.assertIsNone(ls.go_main_package(wrapper_script(self.root / "w", self.sentinel)))

    def test_descriptions_name_what_is_there(self):
        fake_yolo(self.gobin / "yolo", stamp="v0.13.0-5-gdeadbeef")
        self.assertIn("deployed yolo launcher v0.13.0-5-gdeadbeef", ls.classify(self.gobin / "yolo")[1])
        fake_yolo(self.gobin / "yolo-b", kind="development", stamp="v0.13.0-dirty")
        self.assertIn("yolo development build v0.13.0-dirty", ls.classify(self.gobin / "yolo-b")[1])
        os.symlink(self.repo / "scripts/dev-wrapper.sh", self.gobin / "yolo-dev")
        self.assertIn(str(self.repo.resolve()), ls.classify(self.gobin / "yolo-dev")[1])

    def test_version_is_never_asked_of_something_else(self):
        self.assertEqual(ls.version_fields(wrapper_script(self.root / "w", self.sentinel)), {})
        self.assert_never_ran()
        self.assertEqual(ls.version_fields(fake_yolo(self.root / "y", kind="installed"))["build kind"],
                         "installed")

    def test_legacy_names_are_hostmigrates(self):
        go = (HERE.parent / "internal/hostmigrate/hostmigrate.go").read_text()
        block = go.split("var LegacyNames = []string{", 1)[1].split("}", 1)[0]
        self.assertEqual(tuple(n.strip().strip('",') for n in block.split() if n.strip()), ls.LEGACY_NAMES)

    def test_a_name_ending_in_test_is_refused_with_the_reason(self):
        with self.assertRaisesRegex(ls.SlotError, "Go test binary"):
            ls.validate_name("yolo.test")

    def test_names_must_be_plain(self):
        for bad in ("", ".", "..", "a/b", "-x", "yolo\n", "yo\x1blo", "yolo\x7f", "a\tb"):
            with self.assertRaises(ls.SlotError, msg=repr(bad)):
                ls.validate_name(bad)
        self.assertEqual(ls.validate_name("yolo-dev"), "yolo-dev")


class ReplaceTests(Fixture):
    def setUp(self):
        super().setUp()
        self.saved_path = os.environ.get("PATH", "")
        os.environ["PATH"] = str(self.gobin) + os.pathsep + self.saved_path

    def tearDown(self):
        os.environ["PATH"] = self.saved_path
        super().tearDown()

    def installer(self, name="yolo", identity="b" * 64, calls=None):
        def run():
            if calls is not None:
                calls.append(name)
            staged = fake_yolo(self.root / "staged" / name, identity=identity)
            os.replace(staged, self.gobin / name)
        return run

    def deploy(self, name="yolo", calls=None):
        return quiet(ls.deploy, self.repo, self.root / "home", self.gobin, name,
                     installer=self.installer(name, calls=calls))

    def test_deploy_dev_into_an_empty_slot(self):
        slot, out, _ = quiet(ls.deploy_dev, self.repo, self.gobin, "yolo")
        self.assertEqual(Path(os.readlink(slot)), self.wrapper)
        self.assertIn("empty", out)
        self.assertEqual([p.name for p in self.gobin.iterdir()], ["yolo"], "a staged link was left behind")

    def test_deploy_dev_replaces_a_deployed_launcher_and_names_the_way_back(self):
        fake_yolo(self.gobin / "yolo")
        slot, out, _ = quiet(ls.deploy_dev, self.repo, self.gobin, "yolo")
        self.assertTrue(slot.is_symlink())
        self.assertIn("replacing the deployed yolo launcher", out)
        self.assertIn("`just deploy` switches yolo back", out)
        _, out, _ = quiet(ls.deploy_dev, self.repo, self.gobin, "yolo-dev")
        self.assertIn("`just deploy yolo-dev` switches yolo-dev back", out)

    def test_deploy_replaces_the_dev_link_and_names_the_way_back(self):
        os.symlink(self.wrapper, self.gobin / "yolo-x")
        calls = []
        slot, out, _ = self.deploy("yolo-x", calls)
        self.assertEqual(calls, ["yolo-x"])
        self.assertFalse(slot.is_symlink())
        self.assertIn("replacing the dev launcher for the checkout at", out)
        self.assertIn("Switched yolo-x from the dev launcher to a deployed one; "
                      "`just deploy-dev yolo-x` switches it back.", out)

    def test_replaces_legacy_dangling_and_links_and_says_what(self):
        cases = {
            "legacy": lambda p: p.write_text("#!/usr/bin/env python3\nimport yolo_jail\n"),
            "dangling": lambda p: os.symlink(self.root / "nowhere", p),
            "link": lambda p: os.symlink(fake_yolo(self.root / "other/yolo", kind="development"), p),
        }
        for label, make in cases.items():
            with self.subTest(label):
                slot = self.gobin / "yolo"
                if slot.is_symlink() or slot.exists():
                    slot.unlink()
                make(slot)
                _, out, _ = self.deploy()
                self.assertIn("Replaced", out)
                self.assertEqual(ls.classify(slot)[0], "deployed")

    def test_foreign_and_the_wrapper_script_are_refused_untouched_and_never_run(self):
        for label, make in {
            "shell": lambda p: (p.write_text("#!/bin/sh\necho mine\n"), p.chmod(0o755)),
            "wrapper script": lambda p: wrapper_script(p, self.sentinel),
            "other Go binary": lambda p: fake_yolo(p, main="cmd/other"),
        }.items():
            with self.subTest(label):
                slot = self.gobin / "yolo"
                slot.unlink(missing_ok=True)
                make(slot)
                before = slot.read_bytes()
                calls = []
                with self.assertRaisesRegex(ls.SlotError, "(?s)left untouched.*just deploy yolo-stable"):
                    self.deploy(calls=calls)
                with self.assertRaisesRegex(ls.SlotError, "just deploy-dev yolo-dev"):
                    quiet(ls.deploy_dev, self.repo, self.gobin, "yolo")
                self.assertEqual(calls, [], "the installer ran over a foreign file")
                self.assertEqual(slot.read_bytes(), before)
                self.assertFalse(slot.is_symlink())
                self.assert_never_ran()

    def test_another_checkouts_dev_link_is_refused_untouched(self):
        other = checkout(self.root, "another checkout")
        os.symlink(other / "scripts/dev-wrapper.sh", self.gobin / "yolo-dev")
        with self.assertRaisesRegex(ls.SlotError, "(?s)another checkout.*`just deploy-dev yolo-dev` in that checkout"):
            quiet(ls.deploy_dev, self.repo, self.gobin, "yolo-dev")
        with self.assertRaisesRegex(ls.SlotError, "another checkout"):
            self.deploy("yolo-dev")
        self.assertEqual(Path(os.readlink(self.gobin / "yolo-dev")), other / "scripts/dev-wrapper.sh")

    def test_side_by_side_names_leave_each_other_alone(self):
        fake_yolo(self.gobin / "yolo")
        before = (self.gobin / "yolo").read_bytes()
        quiet(ls.deploy_dev, self.repo, self.gobin, "yolo-dev")
        self.assertEqual((self.gobin / "yolo").read_bytes(), before)
        self.assertEqual(Path(os.readlink(self.gobin / "yolo-dev")), self.wrapper)
        # And back the other way: redeploying yolo keeps yolo-dev.
        self.deploy()
        self.assertTrue((self.gobin / "yolo-dev").is_symlink())

    def test_a_second_deployed_launcher_is_refused_before_anything_moves(self):
        for label, make in {
            "regular": lambda p: fake_yolo(p, identity="c" * 64),
            "symlinked": lambda p: os.symlink(fake_yolo(self.root / "kept/yolo", identity="c" * 64), p),
        }.items():
            with self.subTest(label):
                other = self.gobin / "yolo-stable"
                other.unlink(missing_ok=True)
                make(other)
                calls = []
                with self.assertRaises(ls.SlotError) as caught:
                    self.deploy(calls=calls)
                message = str(caught.exception)
                self.assertEqual(calls, [], "the installer ran before the coexist check")
                self.assertFalse((self.gobin / "yolo").exists())
                self.assertIn(str(other), message)
                self.assertIn("Nothing was changed", message)
                self.assertIn("just deploy-dev yolo-stable", message)
                self.assertIn("just deploy yolo-stable", message)
                self.assertIn("just undeploy yolo-stable", message)

    def test_dev_builds_dev_links_and_aliases_of_the_slot_may_coexist(self):
        fake_yolo(self.gobin / "yolo-b", kind="development")
        os.symlink(self.wrapper, self.gobin / "yolo-dev")
        fake_yolo(self.gobin / "yolo")
        os.symlink(self.gobin / "yolo", self.gobin / "y")
        calls = []
        self.deploy(calls=calls)
        self.assertEqual(calls, ["yolo"])

    def test_path_warning_when_the_slot_is_shadowed(self):
        shadow = self.root / "shadow"
        fake_yolo(shadow / "yolo")
        os.environ["PATH"] = str(shadow) + os.pathsep + os.environ["PATH"]
        _, _, err = quiet(ls.deploy_dev, self.repo, self.gobin, "yolo")
        self.assertIn(f"PATH resolves yolo to {shadow / 'yolo'}", err)


class CommandLineTests(Fixture):
    def env(self, **extra):
        env = {k: v for k, v in os.environ.items() if k not in ("YOLO_VERSION", "YOLO_REPO_ROOT")}
        env.update({"GOBIN": str(self.gobin), "HOME": str(self.root / "home")}, **extra)
        (self.root / "home").mkdir(exist_ok=True)
        return env

    def slot(self, *args, **extra):
        return subprocess.run(["python3", str(self.repo / "scripts/launcher-slot.py"), *args,
                               "--gobin", str(self.gobin)], env=self.env(**extra), text=True,
                              capture_output=True)

    def test_refuses_inside_a_jail(self):
        gobin = self.root / "fresh"
        result = subprocess.run(["python3", str(self.repo / "scripts/launcher-slot.py"), "deploy-dev",
                                 "--repo", str(self.repo), "--gobin", str(gobin)],
                                env=self.env(YOLO_VERSION="1"), text=True, capture_output=True)
        self.assertEqual(result.returncode, 1)
        self.assertIn("host-only", result.stderr)
        self.assertFalse(gobin.exists())

    def test_inspect_and_list_never_run_what_they_cannot_identify(self):
        wrapper_script(self.gobin / "yolo", self.sentinel)
        fake_yolo(self.gobin / "yolo-stable")
        os.symlink(self.wrapper, self.gobin / "yolo-dev")
        inspected = self.slot("inspect", "--name", "yolo")
        self.assertEqual(inspected.stdout.split("\t")[0], "foreign")
        listed = self.slot("list").stdout.splitlines()
        self.assertEqual([line.split("\t")[:3] for line in listed],
                         [[str(self.gobin / "yolo-dev"), "dev", "development"],
                          [str(self.gobin / "yolo-stable"), "deployed", "installed"]])
        self.assert_never_ran()

    def test_just_install_dev_passes_the_launcher_name(self):
        """The production Justfile's dev install, under a name, through `just`."""
        if shutil.which("just") is None:
            self.skipTest("just is not installed")
        shutil.copy2(HERE.parent / "Justfile", self.repo / "Justfile")
        result = subprocess.run(["just", "_install-dev", "yolo-dev"], cwd=self.repo, env=self.env(),
                                text=True, capture_output=True)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertEqual(Path(os.readlink(self.gobin / "yolo-dev")), self.wrapper)
        self.assertFalse((self.gobin / "yolo").exists())
        self.assertIn("`just deploy yolo-dev` switches yolo-dev back", result.stdout)

    def test_host_state_primes_the_named_launcher_not_yolo_on_path(self):
        """`just _host-state <name>` runs the broker priming through GOBIN/<name>."""
        if shutil.which("just") is None:
            self.skipTest("just is not installed")
        if shutil.which("openssl") is None:
            self.skipTest("openssl is not installed, so _host-state skips the priming")
        shutil.copy2(HERE.parent / "Justfile", self.repo / "Justfile")
        fake_yolo(self.gobin / "yolo-x")
        decoys = self.root / "decoys"
        decoys.mkdir()
        decoy_log = self.root / "decoy.log"
        for tool in ("yolo", "systemctl"):
            (decoys / tool).write_text(f"#!/bin/sh\necho {tool} \"$@\" >>'{decoy_log}'\nexit 1\n")
            (decoys / tool).chmod(0o755)
        env = self.env(PATH=f"{decoys}{os.pathsep}{os.environ['PATH']}")
        result = subprocess.run(["just", "_host-state", "yolo-x"], cwd=self.repo, env=env, text=True,
                                capture_output=True)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        calls = (self.root / "home/fake-yolo.log").read_text().splitlines()
        self.assertIn("yolo-x internal daemon claude-oauth-broker --init-ca", calls)
        decoy_calls = decoy_log.read_text() if decoy_log.exists() else ""
        self.assertNotIn("yolo ", decoy_calls, "the priming ran the yolo on PATH")
        self.assertIn(f"deployed as {self.gobin / 'yolo-x'}", result.stdout)
        self.assertIn("yolo-x loopholes list", result.stdout)

    def test_recipes_take_a_name_and_share_the_host_state(self):
        just = (HERE.parent / "Justfile").read_text()
        self.assertIn('deploy name="yolo": (install name) (_host-state name)', just)
        self.assertIn('deploy-dev name="yolo": (_install-dev name) (_host-state name)', just)
        self.assertIn('scripts/launcher-slot.py deploy --repo', just)
        self.assertIn('scripts/launcher-slot.py deploy-dev --repo', just)
        self.assertIn('sh scripts/undeploy.sh "$@"', just)


if __name__ == "__main__":
    unittest.main()
