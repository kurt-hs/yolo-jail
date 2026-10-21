#!/usr/bin/env python3
"""Snapshot, packaging, and standalone wrapper regressions."""
import importlib.util
import json
import os
from pathlib import Path
import shutil
import signal
import subprocess
import tempfile
import unittest
from types import SimpleNamespace

HERE = Path(__file__).resolve().parent
SPEC = importlib.util.spec_from_file_location("matched_bundle", HERE / "matched-bundle.py")
mb = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(mb)


def clean_git_env():
    return {k: v for k, v in os.environ.items() if not k.startswith("GIT_")}


def repo_fixture(root: Path, *, real_scripts=False):
    repo = root / "checkout with spaces"
    repo.mkdir(parents=True)
    (repo / ".gitignore").write_text("ignored.txt\ninternal/generated/\n")
    for rel, text in {"go.mod": "module test.invalid\n", "flake.nix": "{}\n",
                      "flake.lock": "{}\n", "Justfile": "set positional-arguments\ndev *args:\n    ./scripts/dev-wrapper.sh \"$@\"\n",
                      "cmd/yolo/main.go": "package main\n",
                      "internal/generated/tracked.go": "package generated\n"}.items():
        path = repo / rel
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(text)
    scripts = repo / "scripts"
    scripts.mkdir()
    for name in ("build-go.sh", "stage-source-bundle.sh", "matched-bundle.py", "dev-wrapper.sh"):
        if real_scripts:
            shutil.copy2(HERE / name, scripts / name)
        elif name == "stage-source-bundle.sh":
            (scripts / name).write_text("SHIPPED_BINARIES=(yolo-helper)\nGUEST_BINARIES=(guest-helper)\n")
        else:
            (scripts / name).write_text(f"fixture {name}\n")
    env = clean_git_env()
    subprocess.run(["git", "init", "-q"], cwd=repo, env=env, check=True)
    subprocess.run(["git", "add", "go.mod", "flake.nix", "flake.lock", "Justfile", "scripts", "cmd"],
                   cwd=repo, env=env, check=True)
    subprocess.run(["git", "add", "-f", "internal/generated/tracked.go"], cwd=repo, env=env, check=True)
    subprocess.run(["git", "-c", "user.name=test", "-c", "user.email=test@example.invalid",
                    "commit", "-qm", "base"], cwd=repo, env=env, check=True)
    return repo


def _fake_helper_dirs(bundle: Path, snapshot: Path):
    shipped, guest = mb._binary_lists(snapshot / "scripts/stage-source-bundle.sh")
    host_os, host_arch = mb._platform(snapshot)
    targets = {f"linux-{host_arch}": shipped}
    if host_os == "darwin":
        targets[f"darwin-{host_arch}"] = guest
    for dirname, names in targets.items():
        directory = bundle / "bin" / dirname
        directory.mkdir(parents=True)
        for name in names:
            binary = directory / name
            binary.write_bytes(b"helper")
            binary.chmod(0o755)


def fake_assembler(snapshot, output, *, identity, build_kind, **_):
    output.mkdir()
    (output / "bin").mkdir()
    launcher = output / "bin/yolo"
    launcher.write_text("#!/bin/sh\nprintf '%s\\n' 'snapshot identity: " + identity + "' 'build kind: " + build_kind + "'\n")
    launcher.chmod(0o755)
    bundle = output / "share/yolo-jail"
    bundle.mkdir(parents=True)
    _fake_helper_dirs(bundle, snapshot)
    (bundle / "flake.nix").write_text("flake")
    (bundle / "flake.lock").write_text("lock")
    (bundle / "yolo-build-stamp.json").write_text(json.dumps({"snapshotIdentity": identity, "buildKind": build_kind}))


class SnapshotTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.root = Path(self.tmp.name)
        self.repo = repo_fixture(self.root)

    def tearDown(self):
        self.tmp.cleanup()

    def test_manifest_captures_dirty_and_new_source_but_excludes_ignored_tracked_and_untracked(self):
        (self.repo / "cmd/yolo/main.go").write_text("package main // dirty\n")
        (self.repo / "internal/new.go").write_text("package internal\n")
        (self.repo / "ignored.txt").write_text("secret\n")
        (self.repo / "internal/generated/ignored.txt").write_text("secret\n")
        with tempfile.TemporaryDirectory() as td:
            snapshot = Path(td) / "snapshot"
            identity = mb.freeze_snapshot(self.repo, snapshot)
            self.assertEqual((snapshot / "cmd/yolo/main.go").read_text(), "package main // dirty\n")
            self.assertTrue((snapshot / "internal/new.go").is_file())
            self.assertFalse((snapshot / "ignored.txt").exists())
            self.assertFalse((snapshot / "internal/generated/ignored.txt").exists())
            self.assertFalse((snapshot / "internal/generated/tracked.go").exists())
            self.assertFalse((snapshot / ".git").exists())
            self.assertEqual(identity, mb.snapshot_identity(snapshot))

    def test_git_state_env_cannot_redirect_the_manifest(self):
        other = self.root / "other"
        other.mkdir()
        subprocess.run(["git", "init", "-q"], cwd=other, check=True, env=clean_git_env())
        keys = ("GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE")
        old = {k: os.environ.get(k) for k in keys}
        os.environ.update({"GIT_DIR": str(other / ".git"), "GIT_WORK_TREE": str(other),
                           "GIT_INDEX_FILE": str(other / "index")})
        try:
            paths = mb.collect_inputs(self.repo)
        finally:
            for k, v in old.items():
                if v is None:
                    os.environ.pop(k, None)
                else:
                    os.environ[k] = v
        self.assertIn("cmd/yolo/main.go", paths)

    def test_external_and_ignored_symlinks_fail_before_following_targets(self):
        source = self.repo / "cmd/yolo/main.go"
        outside = self.root / "outside.go"
        outside.write_text("package main\n")
        source.unlink()
        source.symlink_to(outside)
        with self.assertRaisesRegex(ValueError, "symlink"):
            mb.collect_inputs(self.repo)
        source.unlink()
        source.symlink_to("../../internal/generated/tracked.go")
        with self.assertRaisesRegex(ValueError, "not in snapshot"):
            mb.collect_inputs(self.repo)

    def test_copied_build_scripts_run_from_frozen_tree_after_live_edit(self):
        self.repo = repo_fixture(self.root / "second")
        with tempfile.TemporaryDirectory() as td:
            snapshot = Path(td) / "snapshot"
            identity = mb.freeze_snapshot(self.repo, snapshot)
            (self.repo / "scripts/build-go.sh").write_text("changed live script\n")
            goos = subprocess.check_output(["go", "env", "GOOS"], text=True).strip()
            goarch = subprocess.check_output(["go", "env", "GOARCH"], text=True).strip()
            seen = []
            def run(argv, *, cwd, env, check):
                seen.append(Path(argv[0]))
                self.assertEqual(cwd, snapshot)
                self.assertTrue(Path(argv[0]).is_relative_to(snapshot))
                if str(argv[0]).endswith("build-go.sh"):
                    self.assertEqual(Path(argv[0]).read_text(), "fixture build-go.sh\n")
                    binary = snapshot / "dist-go" / f"{goos}-{goarch}" / "yolo"
                    binary.parent.mkdir(parents=True)
                    binary.write_text("#!/bin/sh\nprintf '%s\\n' 'snapshot identity: " + identity + "' 'build kind: development'\n")
                    binary.chmod(0o755)
                else:
                    dest = Path(argv[1]); dest.mkdir(parents=True)
                    (dest / "flake.nix").write_text("flake")
                    (dest / "flake.lock").write_text("lock")
                    _fake_helper_dirs(dest, snapshot)
                    (dest / "yolo-build-stamp.json").write_text(json.dumps({"snapshotIdentity": identity, "buildKind": "development"}))
                return SimpleNamespace(returncode=0)
            output = Path(td) / "bundle"
            mb.assemble(snapshot, output, identity=identity, build_kind="development",
                        version="v", commit="c", runner=run)
            self.assertEqual(len(seen), 2)

    def test_build_go_omits_whitespace_source_dir_but_stamps_identity(self):
        root = self.root / "build"
        (root / "cmd/yolo").mkdir(parents=True); (root / "scripts").mkdir()
        shutil.copy2(HERE / "build-go.sh", root / "scripts/build-go.sh")
        fakebin = root / "fakebin"; fakebin.mkdir()
        log = root / "args"
        fake = fakebin / "go"
        fake.write_text("""#!/usr/bin/env python3
import os, pathlib, sys
args=sys.argv[1:]
if args[0]=='build':
 pathlib.Path(os.environ['LOG']).write_text(' '.join(args))
 out=pathlib.Path(args[args.index('-o')+1]); out.parent.mkdir(parents=True, exist_ok=True); out.write_bytes(b'x')
"""); fake.chmod(0o755)
        env = os.environ.copy(); env.update({"PATH": str(fakebin)+os.pathsep+env["PATH"], "LOG": str(log),
            "GOOS":"linux", "GOARCH":"amd64", "SOURCE_DIR":"/tmp/space path", "SOURCE_BRANCH":"topic",
            "SNAPSHOT_IDENTITY":"a"*64, "BUILD_KIND":"installed"})
        result = subprocess.run([str(root/"scripts/build-go.sh")], cwd=root, env=env,
                                text=True, capture_output=True, check=True)
        args = log.read_text()
        self.assertIn("SnapshotIdentity="+"a"*64, args); self.assertIn("BuildKind=installed", args)
        self.assertNotIn("version.SourceDir=", args); self.assertIn("version.SourceBranch=topic", args)
        self.assertIn("whitespace", result.stderr)


class PackageTests(unittest.TestCase):
    def test_build_cache_reuses_complete_identity_and_rebuilds_dirty_or_incomplete(self):
        with tempfile.TemporaryDirectory() as td:
            root=Path(td); repo=repo_fixture(root); home=root/"home"; calls=[]
            def builder(snapshot, output, *, identity, build_kind, **kw):
                calls.append(identity); fake_assembler(snapshot, output, identity=identity, build_kind=build_kind)
            first=mb.build_dev_bundle(repo, home, builder=builder)
            self.assertEqual(first, mb.build_dev_bundle(repo, home, builder=builder))
            self.assertEqual(len(calls), 1)
            (repo/"cmd/yolo/main.go").write_text("package main // dirty\n")
            dirty=mb.build_dev_bundle(repo, home, builder=builder)
            self.assertNotEqual(first, dirty); self.assertNotEqual(calls[0], calls[1])
            cache=dirty.parent.parent
            _,host_arch=mb._platform(repo)
            for directory in (cache,cache/"share",cache/"share/yolo-jail",cache/"share/yolo-jail/bin",
                              cache/f"share/yolo-jail/bin/linux-{host_arch}"):
                directory.chmod(0o755)
            helper=cache/f"share/yolo-jail/bin/linux-{host_arch}/yolo-helper"
            helper.chmod(0o644); helper.unlink()
            rebuilt=mb.build_dev_bundle(repo, home, builder=builder)
            self.assertNotEqual(dirty, rebuilt); self.assertEqual(len(calls), 3)

    def test_failed_and_concurrent_publication_is_atomic_and_unique(self):
        from concurrent.futures import ThreadPoolExecutor
        with tempfile.TemporaryDirectory() as td:
            home=Path(td)/"home"
            with self.assertRaises(RuntimeError):
                mb.validate_and_publish_dev(home, {"bin/yolo": (b"bad",0o755)}, "x")
            root=home/".local/share/yolo-jail/dev-bundles"
            self.assertFalse(root.exists() and list(root.iterdir()))
            payload={"bin/yolo": (b"#!/bin/sh\nprintf '%s\\n' 'snapshot identity: x' 'build kind: development'\n",0o755),
                     "share/yolo-jail/flake.nix":(b"f",0o444), "share/yolo-jail/flake.lock":(b"l",0o444),
                     "share/yolo-jail/yolo-build-stamp.json":(b'{"snapshotIdentity":"x","buildKind":"development"}',0o444)}
            with ThreadPoolExecutor(max_workers=4) as pool:
                paths=list(pool.map(lambda _: mb.publish_dev_bundle(home,payload,identity="x"),range(4)))
            self.assertEqual(len({str(p) for p in paths}),4)
            self.assertTrue(all(not p.stat().st_mode & 0o222 for p in paths))

    def test_install_generation_activates_only_identity_match(self):
        with tempfile.TemporaryDirectory() as td:
            root=Path(td); identity="b"*64; launcher=root/"yolo"
            launcher.write_text("#!/bin/sh\nprintf '%s\\n' 'snapshot identity: "+identity+"' 'build kind: installed'\n"); launcher.chmod(0o755)
            bundle=root/"bundle"; bundle.mkdir()
            for name,value in (("flake.nix","f"),("flake.lock","l")): (bundle/name).write_text(value)
            (bundle/"yolo-build-stamp.json").write_text(json.dumps({"snapshotIdentity":identity,"buildKind":"installed"}))
            generation=root/"generation"; calls=[]
            def run(argv,**kw):
                calls.append(argv)
                if "--stage" in argv: generation.mkdir(); return SimpleNamespace(stdout=str(generation))
                return SimpleNamespace(stdout="")
            mb.publish_install_generation(launcher,bundle,identity,{},runner=run)
            self.assertEqual(len(calls),2)
            bad=root/"bad"; shutil.copytree(bundle,bad); (bad/"yolo-build-stamp.json").write_text('{"snapshotIdentity":"wrong","buildKind":"installed"}')
            generation2=root/"generation2"
            def mismatch(argv,**kw):
                calls.append(argv)
                if "--stage" in argv: generation2.mkdir(); return SimpleNamespace(stdout=str(generation2))
                return SimpleNamespace(stdout="")
            with self.assertRaisesRegex(RuntimeError,"identity"): mb.publish_install_generation(launcher,bad,identity,{},runner=mismatch)
            self.assertEqual(len(calls),3)


class WrapperTests(unittest.TestCase):
    def test_just_dev_delegates_to_checkout_wrapper(self):
        just= (HERE.parent/"Justfile").read_text()
        self.assertIn("dev *args:",just); self.assertIn('./scripts/dev-wrapper.sh "$@"',just)
        self.assertIn("set positional-arguments",just)


class StandaloneWrapperTests(unittest.TestCase):
    def setUp(self):
        self.tmp=tempfile.TemporaryDirectory(); self.root=Path(self.tmp.name)
        self.repo=repo_fixture(self.root,real_scripts=True)
        with tempfile.TemporaryDirectory() as td: self.identity=mb.freeze_snapshot(self.repo,Path(td)/"snapshot")
        self.home=self.root/"home"; self.log=self.root/"launcher.json"; self.stale_marker=self.root/"stale"
        installed=self.root/"installed-generation"; installed.mkdir()
        self.active=self.home/".local/share/yolo-jail/flake-bundle"
        self.active.parent.mkdir(parents=True); self.active.symlink_to(installed)
        self.active_target=os.readlink(self.active)
        source=f'''#!/usr/bin/env python3
import json, os, signal, sys
identity={self.identity!r}
if sys.argv[1:]==["--version","--full"]:
 print("snapshot identity: "+identity); print("build kind: development"); raise SystemExit(0)
with open(os.environ["YOLO_TEST_LOG"],"w") as f: json.dump({{"args":sys.argv[1:],"cwd":os.getcwd(),"hasRepoRoot":"YOLO_REPO_ROOT" in os.environ,"path":os.environ.get("PATH")}},f)
if os.environ.get("YOLO_TEST_SIGNAL"): os.kill(os.getpid(),getattr(signal,"SIG"+os.environ["YOLO_TEST_SIGNAL"]))
raise SystemExit(23)
'''
        payload={"bin/yolo":(source.encode(),0o755),"share/yolo-jail/flake.nix":(b"f",0o444),
            "share/yolo-jail/flake.lock":(b"l",0o444),"share/yolo-jail/yolo-build-stamp.json":
            (json.dumps({"snapshotIdentity":self.identity,"buildKind":"development"}).encode(),0o444)}
        shipped,guest=mb._binary_lists(self.repo/"scripts/stage-source-bundle.sh")
        host_os,host_arch=mb._platform(self.repo)
        targets={f"linux-{host_arch}":shipped}
        if host_os=="darwin": targets[f"darwin-{host_arch}"]=guest
        for dirname,names in targets.items():
            for name in names: payload[f"share/yolo-jail/bin/{dirname}/{name}"]=(b"helper",0o755)
        mb.publish_dev_bundle(self.home,payload,identity=self.identity)
        self.links=self.root/"path bin"; self.links.mkdir()
        script=self.repo/"scripts/dev-wrapper.sh"
        (self.links/"relative").symlink_to(os.path.relpath(script,self.links))
        (self.links/"yolo-dev").symlink_to("relative")
        (self.links/"yolo").symlink_to(str(self.links/"yolo-dev"))
        self.stale=self.root/"stale bin"; self.stale.mkdir()
        old=self.stale/"yolo"; old.write_text(f"#!/usr/bin/env python3\nfrom pathlib import Path\nPath({str(self.stale_marker)!r}).touch()\n"); old.chmod(0o755)
        self.cwd=self.root/"unrelated cwd"; self.cwd.mkdir()
        self.other=self.root/"other checkout"; self.other.mkdir()

    def tearDown(self): self.tmp.cleanup()

    def env(self):
        env=os.environ.copy(); env.update({"HOME":str(self.home),"YOLO_REPO_ROOT":str(self.other),
            "YOLO_TEST_LOG":str(self.log),"PATH":str(self.stale)+os.pathsep+str(self.links)+os.pathsep+"/usr/bin:/bin"+os.pathsep+os.environ.get("PATH","")})
        env.pop("YOLO_TEST_SIGNAL",None); return env

    def test_yolo_and_yolo_dev_symlink_names_use_selected_checkout_from_unrelated_cwd(self):
        self.assertEqual(shutil.which("yolo",path=self.env()["PATH"]),str(self.stale/"yolo"))
        for name,args in (("yolo-dev",["--","bash","-c","two words"]),("yolo",["--","bash","-c","arg with spaces"])):
            env=self.env()
            command=["yolo-dev",*args] if name=="yolo-dev" else [str(self.links/"yolo"),*args]
            result=subprocess.run(command,cwd=self.cwd,env=env,text=True,capture_output=True)
            self.assertEqual(result.returncode,23,result.stderr)
            record=json.loads(self.log.read_text()); self.assertEqual(record["args"],args)
            self.assertEqual(record["cwd"],str(self.cwd)); self.assertFalse(record["hasRepoRoot"])
            self.assertEqual(record["path"],env["PATH"]); self.assertEqual(os.readlink(self.active),self.active_target)
            self.assertIn("remove that export",result.stderr); self.assertNotIn(str(self.other),result.stderr)
            self.assertFalse(self.stale_marker.exists())
        self.assertEqual(len(list((self.home/".local/share/yolo-jail/dev-bundles").glob("*"))),1)

    def test_wrapper_without_obsolete_selector_is_silent_about_it(self):
        env=self.env(); env.pop("YOLO_REPO_ROOT")
        result=subprocess.run([str(self.links/"yolo-dev"),"--","quiet"],cwd=self.cwd,env=env,text=True,capture_output=True)
        self.assertEqual(result.returncode,23,result.stderr)
        self.assertNotIn("YOLO_REPO_ROOT",result.stderr)
        self.assertEqual(json.loads(self.log.read_text())["path"],env["PATH"])

    def test_broken_and_cyclic_aliases_fail_without_build_or_launch(self):
        import runpy
        resolver=runpy.run_path(str(HERE/"dev-wrapper.sh"))
        broken=self.links/"broken"; broken.symlink_to("missing-target")
        cycle_a=self.links/"cycle-a"; cycle_b=self.links/"cycle-b"
        cycle_a.symlink_to(cycle_b); cycle_b.symlink_to(cycle_a)
        for path in (broken,cycle_a):
            with self.subTest(path=path.name):
                with self.assertRaisesRegex(RuntimeError,"relink"):
                    resolver["resolve_checkout"](path)
                try:
                    result=subprocess.run([str(path),"--","not-run"],cwd=self.cwd,env=self.env(),capture_output=True)
                except OSError:
                    result=None  # The kernel can reject a broken/cyclic executable before Python starts.
                self.assertTrue(result is None or result.returncode != 23)
                self.assertFalse(self.log.exists())
        generations=list((self.home/".local/share/yolo-jail/dev-bundles").glob("*"))
        self.assertEqual(len(generations),1)

        env=self.env(); env["YOLO_TEST_SIGNAL"]="TERM"
        signal_run=subprocess.run([str(self.links/"yolo"),"--","signal"],cwd=self.cwd,env=env,text=True,capture_output=True)
        self.assertEqual(signal_run.returncode,-signal.SIGTERM)
        base_log=self.log.read_text()
        (self.repo/"cmd/yolo/main.go").write_text("package main // changed\n")
        dirty=subprocess.run([str(self.links/"yolo-dev"),"--","dirty"],cwd=self.cwd,env=self.env(),text=True,capture_output=True)
        self.assertNotEqual(dirty.returncode,23); self.assertEqual(self.log.read_text(),base_log)
        (self.repo/"cmd/yolo/main.go").write_text("package main\n")
        cache=next((self.home/".local/share/yolo-jail/dev-bundles").glob(f"{self.identity[:16]}-*"))
        cache.chmod(0o755); (cache/"share").chmod(0o755); (cache/"share/yolo-jail").chmod(0o755)
        (cache/"share/yolo-jail/flake.nix").unlink()
        incomplete=subprocess.run([str(self.links/"yolo-dev"),"--","incomplete"],cwd=self.cwd,env=self.env(),text=True,capture_output=True)
        self.assertNotEqual(incomplete.returncode,23); self.assertEqual(self.log.read_text(),base_log)
        self.assertFalse(self.stale_marker.exists())


if __name__ == "__main__":
    unittest.main()
