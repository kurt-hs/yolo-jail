#!/usr/bin/env python3
"""Tests for check-answered-questions.py, and that the landing gate runs it."""
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest

HERE = Path(__file__).resolve().parent
SPEC = importlib.util.spec_from_file_location("answered", HERE / "check-answered-questions.py")
module = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(module)


def index(*questions):
    return {"index": {"documents": [{"path": "docs/design/x.md", "questions": list(questions)}]}}


class AnsweredTest(unittest.TestCase):
    def test_an_answered_question_is_reported(self):
        idx = index({"id": "OQ-X1", "state": "answered", "line": 12},
                    {"id": "OQ-X2", "state": "open", "line": 30},
                    {"id": "OQ-X3", "state": "blocked", "line": 40})
        self.assertEqual(module.answered(idx), [("docs/design/x.md", 12, "OQ-X1")])

    def test_a_saved_index_with_one_fails_and_with_none_passes(self):
        with tempfile.TemporaryDirectory() as tmp:
            p = Path(tmp) / "i.json"
            p.write_text(json.dumps(index({"id": "OQ-X1", "state": "answered", "line": 1})))
            self.assertEqual(module.main([str(p)]), 1)
            p.write_text(json.dumps(index({"id": "OQ-X2", "state": "open", "line": 1})))
            self.assertEqual(module.main([str(p)]), 0)


class CallSiteTest(unittest.TestCase):
    # A check nobody runs is not a check: the landing gate's lint-ci recipe must call it.
    def test_lint_ci_runs_the_check(self):
        justfile = (HERE.parent / "Justfile").read_text()
        recipe = justfile.split("\nlint-ci:", 1)[1].split("\n\n", 1)[0]
        self.assertIn("python3 scripts/check-answered-questions.py", recipe)
        self.assertIn("python3 scripts/test-check-answered-questions.py", recipe)


if __name__ == "__main__":
    unittest.main()
