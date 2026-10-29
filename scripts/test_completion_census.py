#!/usr/bin/env python3
"""Regressions for completion-census.py's test-log parsing: what counts as a repository read.

The census must not depend on where the checkout lives. Production code probes the jail's own
workspace path, the literal `/workspace`, so the same test logs `stat /workspace` from every
checkout; only in the checkout that IS `/workspace` does that path name the repository root.
"""
import importlib.util
import os
from pathlib import Path
import tempfile
import unittest

HERE = Path(__file__).resolve().parent
SPEC = importlib.util.spec_from_file_location("completion_census", HERE / "completion-census.py")
census = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(census)


class Everything:
    """A visibility stub: every path is tracked or nonignored."""

    def contains(self, path, directory=False):
        return True


class ParseLogTest(unittest.TestCase):
    def setUp(self):
        self._root = census.ROOT
        self.addCleanup(setattr, census, "ROOT", self._root)
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        self.tmp = Path(tmp.name).resolve()
        for d in ("pkg", "other"):
            (self.tmp / d).mkdir()
        (self.tmp / "other" / "data.txt").write_text("x")

    def parse(self, root, lines):
        census.ROOT = Path(root)
        return census.parse_log("# test log\n" + "\n".join(lines) + "\n",
                                str(Path(root) / "pkg"), {"pkg", "other"}, Everything())

    def test_the_jail_workspace_probe_is_not_a_root_listing_when_the_checkout_is_workspace(self):
        probe = ["getenv YOLO_WORKSPACE", "stat /workspace"]
        self.assertEqual(self.parse("/workspace", probe), (set(), set(), set()))

    def test_the_probe_reads_the_same_from_every_checkout(self):
        probe = ["getenv YOLO_WORKSPACE", "stat /workspace", "stat " + str(self.tmp)]
        here = self.parse(self.tmp, probe)
        at_workspace = self.parse("/workspace", ["getenv YOLO_WORKSPACE", "stat /workspace"])
        self.assertEqual(here, at_workspace)
        self.assertEqual(here, (set(), set(), set()))

    def test_a_stat_of_the_root_by_a_relative_spelling_is_not_a_listing(self):
        self.assertEqual(self.parse(self.tmp, ["stat ..", "stat ../."]), (set(), set(), set()))

    def test_opening_the_root_is_still_a_listing_by_every_spelling(self):
        for spelling in (str(self.tmp), "..", "../pkg/.."):
            with self.subTest(spelling=spelling):
                self.assertEqual(self.parse(self.tmp, ["open " + spelling]), (set(), {"."}, set()))

    def test_other_directories_and_files_are_still_recorded(self):
        got = self.parse(self.tmp, ["stat " + str(self.tmp / "other"),
                                    "open ../other/data.txt", "stat ../missing"])
        self.assertEqual(got, ({"other/data.txt"}, {"other"}, {"missing"}))


if __name__ == "__main__":
    unittest.main()
