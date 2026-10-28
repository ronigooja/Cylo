#!/usr/bin/env python3
"""Run the repository's fast or complete verification plan.

The command is intentionally local and read-only: it never installs services or
publishes artifacts. Each invocation writes a timing record so CI and developers
can compare incremental and release-candidate runs.
"""

from __future__ import annotations

import argparse
import json
import os
import subprocess
import sys
import time
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]


def command_plan(mode: str) -> list[tuple[str, list[str], Path]]:
    go = ["go", "test", "./..."]
    swift = [
        "swift",
        "test",
        "--package-path",
        "clients/apple/Packages/ZoeKit",
    ]
    scripts = ["bash", "deploy/linux/test-build-release.sh"]
    cli = ["bash", "deploy/linux/test-install-cli.sh"]
    if mode == "quick":
        return [("go-incremental", go, ROOT), ("release-scripts", scripts, ROOT), ("cli-install", cli, ROOT)]
    if mode == "swift":
        return [("swift-package", swift, ROOT)]
    if mode == "full":
        return [
            ("go", go, ROOT),
            ("go-race", ["go", "test", "-race", "./internal/control", "./internal/runner", "./internal/ops", "./cmd/zoe-server", "./cmd/zoe"], ROOT),
            ("swift-package", swift, ROOT),
            ("release-scripts", scripts, ROOT),
            ("cli-install", cli, ROOT),
        ]
    raise ValueError(f"unknown mode: {mode}")


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("mode", choices=("quick", "swift", "full"), nargs="?", default="quick")
    parser.add_argument("--timing-file", type=Path, help="write JSON timing evidence to this path")
    args = parser.parse_args()
    started = time.time()
    revision = subprocess.run(["git", "rev-parse", "HEAD"], cwd=ROOT, text=True, capture_output=True, check=True).stdout.strip()
    records: list[dict[str, object]] = []
    status = 0
    for name, command, cwd in command_plan(args.mode):
        begin = time.time()
        print(f"==> {name}: {' '.join(command)}", flush=True)
        result = subprocess.run(command, cwd=cwd)
        end = time.time()
        records.append({"name": name, "command": command, "status": result.returncode, "started_at": begin, "ended_at": end, "duration_seconds": round(end - begin, 3)})
        if result.returncode:
            status = result.returncode
            break
    evidence = {
        "mode": args.mode,
        "source_revision": revision,
        "started_at": started,
        "ended_at": time.time(),
        "duration_seconds": round(time.time() - started, 3),
        "cache": {"gocache": os.environ.get("GOCACHE", "default"), "buildkit": os.environ.get("ZOE_BUILDKIT_CACHE", "unspecified")},
        "checks": records,
        "status": status,
    }
    if args.timing_file:
        args.timing_file.parent.mkdir(parents=True, exist_ok=True)
        args.timing_file.write_text(json.dumps(evidence, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    return status


if __name__ == "__main__":
    sys.exit(main())
