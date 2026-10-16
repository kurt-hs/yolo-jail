#!/usr/bin/env python3
"""Fail when a document still carries an answered question in question form.

An answered question (marked ✅) is compacted into its document's Decision Ledger in the same
change that records the ruling (the `design-doc` skill's Answering Protocol). Left in question
form it keeps reading as something to decide, and nothing else notices: per-file vantage-check
does not flag it, and only `vantage-check index` lists it. This check reads that index.

    python3 scripts/check-answered-questions.py            # runs the index itself
    python3 scripts/check-answered-questions.py index.json # reads a saved index
"""
import json
import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent


def answered(index):
    """(path, line, id) for every question the index reports as answered."""
    out = []
    for doc in index.get("index", {}).get("documents", []):
        for q in doc.get("questions", []):
            if q.get("state") == "answered":
                out.append((doc["path"], q.get("line"), q.get("id")))
    return sorted(out, key=lambda x: (x[0], x[1] or 0))


def load(argv):
    if argv:
        return json.loads(Path(argv[0]).read_text())
    raw = subprocess.check_output([str(ROOT / "scripts" / "vantage-check.sh"), "index", "--format", "json"],
                                  cwd=ROOT, text=True)
    return json.loads(raw)


def main(argv):
    found = answered(load(argv))
    if not found:
        return 0
    for path, line, qid in found:
        print(f"{path}:{line}: {qid} is answered but still written as a question")
    print(f"\n{len(found)} answered question(s) not compacted. Move each into its document's "
          "Decision Ledger and repoint links to its #id at #decision-ledger (the design-doc "
          "skill's Answering Protocol; vantage-docs, \"Compaction and references\").")
    return 1


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
