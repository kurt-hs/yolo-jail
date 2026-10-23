#!/usr/bin/env python3
"""Run a matched build from the checkout this resolved wrapper belongs to."""
# The shebang finds whatever python3 is first on PATH, which on macOS can be the system's 3.9;
# deferred annotations keep `str | Path` from being evaluated there.
from __future__ import annotations

import importlib.util
from pathlib import Path
import sys


def resolve_checkout(script_path: str | Path) -> Path:
    try:
        wrapper = Path(script_path).resolve(strict=True)
    except (OSError, RuntimeError) as exc:
        raise RuntimeError(f"cannot resolve this wrapper's symlink chain ({exc}); relink it to a live checkout's scripts/dev-wrapper.sh") from exc
    return wrapper.parent.parent


def main() -> int:
    try:
        repo = resolve_checkout(__file__)
    except RuntimeError as exc:
        print(f"dev-wrapper: {exc}", file=sys.stderr)
        return 1
    helper = repo / "scripts/matched-bundle.py"
    spec = importlib.util.spec_from_file_location("matched_bundle", helper)
    if spec is None or spec.loader is None:
        print(f"dev-wrapper: cannot load matched bundle helper: {helper}", file=sys.stderr)
        return 1
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    sys.argv = [str(helper), "dev", "--repo", str(repo), "--home", str(Path.home()), "--", *sys.argv[1:]]
    return module.main()


if __name__ == "__main__":
    raise SystemExit(main())
