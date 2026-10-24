#!/usr/bin/env python3
"""Regressions for launcher-slot.py: classifying, replacing and refusing what holds the slot."""
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


def fake_yolo(path: Path, *, kind="installed", identity="a" * 64, stamp="v1-0-gabc"):
    """A stand-in yolo build: it carries the module path and answers `--version --full`."""
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text("#!/bin/sh\n# github.com/mschulkind-oss/yolo-jail/cmd/yolo\n"
                    f"printf '%s\\n' 'build stamp: {stamp}' 'commit: abc' 'binary sha256: x' "
                    f"'snapshot identity: {identity}' 'build kind: {kind}'\n")
    path.chmod(0o755)
    return path


def checkout(root: Path) -> Path:
    repo = root / "checkout with spaces"
    (repo / "scripts").mkdir(parents=True)
    wrapper = repo / "scripts/dev-wrapper.sh"
    wrapper.write_text("#!/usr/bin/env python3\n")
    wrapper.chmod(0o755)
    shutil.copy2(HERE / "launcher-slot.py", repo / "scripts/launcher-slot.py")
    return repo


def quiet(fn, *args, **kwargs):
    out, err = io.StringIO(), io.StringIO()
    with contextlib.redirect_stdout(out), contextlib.redirect_stderr(err):
        result = fn(*args, **kwargs)
    return result, out.getvalue(), err.getvalue()


class ClassifyTests(unittest.TestCase):
    def setUp(self):
        self.td = tempfile.TemporaryDirectory()
        self.root = Path(self.td.name)
        self.gobin = self.root / "gobin"
        self.gobin.mkdir()
        self.repo = checkout(self.root)

    def tearDown(self):
        self.td.cleanup()

    def kind(self, name):
        return ls.classify(self.gobin / name)[0]

    def test_every_kind(self):
        fake_yolo(self.gobin / "deployed")
        os.symlink(self.repo / "scripts/dev-wrapper.sh", self.gobin / "dev")
        os.symlink(self.root / "gone/scripts/dev-wrapper.sh", self.gobin / "devgone")
        os.symlink(fake_yolo(self.root / "elsewhere/yolo"), self.gobin / "link")
        os.symlink(self.root / "nothing", self.gobin / "dangling")
        venv = self.root / "venv"
        (venv / "bin").mkdir(parents=True)
        (venv / "pyvenv.cfg").write_text("")
        (venv / "bin/yolo").write_text("#!/usr/bin/env python3\n")
        os.symlink(venv / "bin/yolo", self.gobin / "venvlink")
        (self.gobin / "yolo").write_text("#!/usr/bin/env python3\nimport yolo_jail\n")
        (self.gobin / "pyother").write_text("#!/usr/bin/env python3\n")
        (self.gobin / "shell").write_text("#!/bin/sh\necho hi\n")
        (self.gobin / "shell").chmod(0o755)
        (self.gobin / "dir").mkdir()
        (self.root / "plain").write_text("data")
        os.symlink(self.root / "plain", self.gobin / "otherlink")
        want = {"absent": "absent", "deployed": "deployed", "dev": "dev", "devgone": "dev",
                "link": "link", "dangling": "dangling", "venvlink": "legacy", "yolo": "legacy",
                "pyother": "foreign", "shell": "foreign", "dir": "foreign", "otherlink": "foreign"}
        self.assertEqual({name: self.kind(name) for name in want}, want)

    def test_descriptions_name_what_is_there(self):
        fake_yolo(self.gobin / "yolo", stamp="v0.13.0-5-gdeadbeef")
        self.assertIn("deployed yolo launcher v0.13.0-5-gdeadbeef", ls.classify(self.gobin / "yolo")[1])
        os.symlink(self.repo / "scripts/dev-wrapper.sh", self.gobin / "yolo-dev")
        self.assertIn(str(self.repo.resolve()), ls.classify(self.gobin / "yolo-dev")[1])

    def test_names_must_be_plain(self):
        for bad in ("", ".", "..", "a/b", "-x"):
            with self.assertRaises(ls.SlotError):
                ls.validate_name(bad)
        self.assertEqual(ls.validate_name("yolo-dev"), "yolo-dev")


class ReplaceTests(unittest.TestCase):
    def setUp(self):
        self.td = tempfile.TemporaryDirectory()
        self.root = Path(self.td.name)
        self.gobin = self.root / "gobin"
        self.gobin.mkdir()
        self.repo = checkout(self.root)
        self.wrapper = self.repo.resolve() / "scripts/dev-wrapper.sh"
        self.saved_path = os.environ.get("PATH", "")
        os.environ["PATH"] = str(self.gobin) + os.pathsep + self.saved_path

    def tearDown(self):
        os.environ["PATH"] = self.saved_path
        self.td.cleanup()

    def installer(self, name="yolo", identity="b" * 64, calls=None):
        def run():
            if calls is not None:
                calls.append(name)
            staged = fake_yolo(self.root / "staged" / name, identity=identity)
            os.replace(staged, self.gobin / name)
        return run

    def test_deploy_dev_into_an_empty_slot(self):
        slot, out, _ = quiet(ls.deploy_dev, self.repo, self.gobin, "yolo")
        self.assertEqual(Path(os.readlink(slot)), self.wrapper)
        self.assertIn("empty", out)
        self.assertEqual([p.name for p in self.gobin.iterdir()], ["yolo"], "a staged link was left behind")

    def test_deploy_dev_replaces_a_deployed_launcher_and_says_so(self):
        fake_yolo(self.gobin / "yolo")
        slot, out, _ = quiet(ls.deploy_dev, self.repo, self.gobin, "yolo")
        self.assertTrue(slot.is_symlink())
        self.assertIn("replacing the deployed yolo launcher", out)
        self.assertIn("`just deploy` switches back", out)

    def test_deploy_replaces_the_dev_link_and_says_so(self):
        os.symlink(self.wrapper, self.gobin / "yolo")
        calls = []
        slot, out, _ = quiet(ls.deploy, self.repo, self.root / "home", self.gobin, "yolo",
                             installer=self.installer(calls=calls))
        self.assertEqual(calls, ["yolo"])
        self.assertFalse(slot.is_symlink())
        self.assertIn("replacing the dev launcher for the checkout at", out)
        self.assertIn("Switched yolo from the dev launcher to a deployed one.", out)

    def test_foreign_is_refused_untouched_in_both_modes(self):
        foreign = self.gobin / "yolo"
        foreign.write_text("#!/bin/sh\necho mine\n")
        foreign.chmod(0o755)
        calls = []
        with self.assertRaisesRegex(ls.SlotError, "(?s)left untouched.*just deploy yolo-stable"):
            quiet(ls.deploy, self.repo, self.root / "home", self.gobin, "yolo",
                  installer=self.installer(calls=calls))
        with self.assertRaisesRegex(ls.SlotError, "just deploy-dev yolo-dev"):
            quiet(ls.deploy_dev, self.repo, self.gobin, "yolo")
        self.assertEqual(calls, [], "the installer ran over a foreign file")
        self.assertEqual(foreign.read_text(), "#!/bin/sh\necho mine\n")

    def test_side_by_side_names_leave_each_other_alone(self):
        fake_yolo(self.gobin / "yolo")
        before = (self.gobin / "yolo").read_bytes()
        quiet(ls.deploy_dev, self.repo, self.gobin, "yolo-dev")
        self.assertEqual((self.gobin / "yolo").read_bytes(), before)
        self.assertEqual(Path(os.readlink(self.gobin / "yolo-dev")), self.wrapper)
        # And back the other way: redeploying yolo keeps yolo-dev.
        quiet(ls.deploy, self.repo, self.root / "home", self.gobin, "yolo", installer=self.installer())
        self.assertTrue((self.gobin / "yolo-dev").is_symlink())

    def test_a_second_deployed_launcher_is_flagged(self):
        fake_yolo(self.gobin / "yolo-stable", identity="c" * 64)
        _, _, err = quiet(ls.deploy, self.repo, self.root / "home", self.gobin, "yolo",
                          installer=self.installer(identity="b" * 64))
        self.assertIn(str(self.gobin / "yolo-stable"), err)
        self.assertIn("just deploy-dev yolo-stable", err)

    def test_path_warning_when_the_slot_is_shadowed(self):
        shadow = self.root / "shadow"
        fake_yolo(shadow / "yolo")
        os.environ["PATH"] = str(shadow) + os.pathsep + os.environ["PATH"]
        _, _, err = quiet(ls.deploy_dev, self.repo, self.gobin, "yolo")
        self.assertIn(f"PATH resolves yolo to {shadow / 'yolo'}", err)


class CommandLineTests(unittest.TestCase):
    def env(self, gobin, **extra):
        env = {k: v for k, v in os.environ.items() if k not in ("YOLO_VERSION", "YOLO_REPO_ROOT")}
        env.update({"GOBIN": str(gobin)}, **extra)
        return env

    def test_refuses_inside_a_jail(self):
        with tempfile.TemporaryDirectory() as td:
            root = Path(td)
            repo = checkout(root)
            result = subprocess.run(["python3", str(repo / "scripts/launcher-slot.py"), "deploy-dev",
                                     "--repo", str(repo), "--gobin", str(root / "gobin")],
                                    env=self.env(root / "gobin", YOLO_VERSION="1"), text=True, capture_output=True)
            self.assertEqual(result.returncode, 1)
            self.assertIn("host-only", result.stderr)
            self.assertFalse((root / "gobin").exists())

    def test_just_install_dev_passes_the_launcher_name(self):
        """The production Justfile's dev install, under a name, through `just`."""
        if shutil.which("just") is None:
            self.skipTest("just is not installed")
        with tempfile.TemporaryDirectory() as td:
            root = Path(td)
            repo = checkout(root)
            shutil.copy2(HERE.parent / "Justfile", repo / "Justfile")
            gobin = root / "gobin"
            result = subprocess.run(["just", "_install-dev", "yolo-dev"], cwd=repo, env=self.env(gobin),
                                    text=True, capture_output=True)
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
            self.assertEqual(Path(os.readlink(gobin / "yolo-dev")), repo.resolve() / "scripts/dev-wrapper.sh")
            self.assertFalse((gobin / "yolo").exists())

    def test_recipes_take_a_name_and_share_the_host_state(self):
        just = (HERE.parent / "Justfile").read_text()
        self.assertIn('deploy name="yolo": (install name) (_host-state name)', just)
        self.assertIn('deploy-dev name="yolo": (_install-dev name) (_host-state name)', just)
        self.assertIn('scripts/launcher-slot.py deploy --repo', just)
        self.assertIn('scripts/launcher-slot.py deploy-dev --repo', just)


if __name__ == "__main__":
    unittest.main()
