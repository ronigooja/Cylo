#!/usr/bin/env python3
"""Offline comparison slice: real CAO module vs AO extracted Git command contract.

No daemon, Codex, model, network or user account is started. AO Go is NOT run.
Only disposable repositories under TemporaryDirectory are mutated.
"""
import argparse
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import platform
import re
import subprocess
import tempfile

PINS = {"cao": "4e990a9d78b154ad76c6cf94c7b51e36fce69f21",
        "ao": "7c83bd68c1e60c85b6a90e3ffa1340c859e4a3ff"}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--cao", type=Path, default=Path("/tmp/cylo-research-cao"))
    parser.add_argument("--ao", type=Path, default=Path("/tmp/cylo-alt-ao"))
    args = parser.parse_args()
    sources = {"cao": args.cao.resolve(), "ao": args.ao.resolve()}
    for name, path in sources.items():
        head = subprocess.check_output(["git", "-C", str(path), "rev-parse", "HEAD"], text=True).strip()
        if head != PINS[name]:
            raise SystemExit(f"{name}: expected {PINS[name]}, got {head}")
    cao_file = sources["cao"] / "src/cli_agent_orchestrator/services/worktree_service.py"
    ao_file = sources["ao"] / "backend/internal/adapters/workspace/gitworktree/commands.go"
    for name, path, relative in [("cao", cao_file, cao_file.relative_to(sources["cao"])),
                                 ("ao", ao_file, ao_file.relative_to(sources["ao"]))]:
        committed = subprocess.check_output(["git", "-C", str(sources[name]), "show", f"HEAD:{relative}"])
        if committed != path.read_bytes():
            raise SystemExit(f"{name}: source file differs from pinned commit")
    spec = importlib.util.spec_from_file_location("cao_worktree", cao_file)
    cao = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(cao)
    ao_source = ao_file.read_text()

    # Fail closed if upstream changes these literal-return helpers. This is an
    # explicit command-contract extraction, not an implementation of Go/Destroy.
    def ao_args(function, repo, path):
        pattern = rf'func {function}\(repo, path string\) \[\]string \{{\s*return \[\]string\{{([^}}]+)\}}\s*\}}'
        match = re.search(pattern, ao_source)
        if not match:
            raise AssertionError(f"unsupported AO helper: {function}")
        values = []
        for token in match.group(1).split(","):
            token = token.strip()
            values.append({"repo": str(repo), "path": str(path)}[token]
                          if token in ("repo", "path") else json.loads(token))
        return values

    results = {"versions": {**PINS, "python": platform.python_version(),
                "git": subprocess.check_output(["git", "--version"], text=True).strip()},
               "execution": {"cao": "imported pinned upstream Python worktree adapter",
                             "ao": "extracted pinned literal Git argv; Go adapter/daemon NOT executed"},
               "model_calls": 0, "cases": []}
    results["source_sha256"] = {"cao_worktree": hashlib.sha256(cao_file.read_bytes()).hexdigest(),
                                 "ao_commands": hashlib.sha256(ao_file.read_bytes()).hexdigest()}
    with tempfile.TemporaryDirectory(prefix="cylo-cao-ao-worktree-") as temp:
        root = Path(temp)
        env = {"PATH": os.environ.get("PATH", "/usr/bin:/bin"), "HOME": str(root / "home"),
               "GIT_CONFIG_NOSYSTEM": "1", "GIT_CONFIG_GLOBAL": os.devnull,
               "GIT_TERMINAL_PROMPT": "0", "LC_ALL": "C"}
        Path(env["HOME"]).mkdir()
        # CAO internally inherits environment for Git; scope to this run only.
        previous_env = os.environ.copy()
        os.environ.clear()
        os.environ.update(env)
        try:
            def git(*argv, check=True):
                return subprocess.run(["git", *map(str, argv)], env=env, text=True,
                                      capture_output=True, check=check, timeout=20)
            for implementation in ("cao", "ao"):
                repo = root / implementation
                git("init", "-q", repo)
                git("-C", repo, "config", "user.name", "Cylo fixture")
                git("-C", repo, "config", "user.email", "fixture@example.invalid")
                (repo / "tracked.txt").write_text("base\n")
                git("-C", repo, "add", "tracked.txt")
                git("-C", repo, "commit", "-qm", "fixture base")
                paths = []
                for worker in ("worker1", "worker2"):
                    if implementation == "cao":
                        path = Path(cao.create_worktree(str(repo), worker))
                    else:
                        path = root / f"ao-{worker}"
                        # Fixture setup, NOT AO Create execution.
                        git("-C", repo, "worktree", "add", "-b", worker, path)
                    paths.append(path)
                    # Deterministic fixture writes, not agent-generated tasks.
                    (path / "tracked.txt").write_text(worker + "\n")
                    (path / "artifact.txt").write_text(worker + " untracked artifact\n")
                assert paths[0] != paths[1]
                assert (paths[0] / "tracked.txt").read_text() == "worker1\n"
                assert (paths[1] / "tracked.txt").read_text() == "worker2\n"
                assert (repo / "tracked.txt").read_text() == "base\n"
                # New git process sees registration; not daemon/session recovery.
                registered = git("-C", repo, "worktree", "list", "--porcelain").stdout
                assert all(str(path) in registered for path in paths)
                git("-C", paths[1], "add", ".")
                git("-C", paths[1], "commit", "-qm", "worker2 committed artifact")
                commit = git("-C", paths[1], "rev-parse", "HEAD").stdout.strip()
                if implementation == "cao":
                    cao.remove_worktree(str(repo), "worker1")
                    dirty_preserved = paths[0].exists()
                    cao.remove_worktree(str(repo), "worker2")
                    branch = "cao/worker2"
                    force = {"executed": False, "reason": "CAO normal teardown already forces removal"}
                    refusal = None
                else:
                    command = ao_args("worktreeRemoveArgs", repo, paths[0])
                    removed = git(*command, check=False)
                    assert removed.returncode != 0
                    dirty_preserved = (paths[0] / "artifact.txt").read_text() == "worker1 untracked artifact\n"
                    refusal = {"argv": [part.replace(str(root), "$TEMP") for part in command[2:]], "exit_code": removed.returncode,
                               "stderr": removed.stderr.strip().replace(str(root), "$TEMP")}
                    # Demonstrate protection bypass only on disposable content.
                    # AO ForceDestroy preconditions/stash path are NOT exercised.
                    git(*ao_args("worktreeForceRemoveArgs", repo, paths[0]))
                    force = {"executed": True, "worktree_removed": not paths[0].exists(),
                             "scope": "raw force helper only; no preservation precondition"}
                    git(*ao_args("worktreeRemoveArgs", repo, paths[1]))
                    branch = "worker2"
                assert dirty_preserved == (implementation == "ao")
                assert not paths[1].exists()
                assert git("-C", repo, "rev-parse", branch).stdout.strip() == commit
                assert git("-C", repo, "show", f"{branch}:artifact.txt").stdout == "worker2 untracked artifact\n"
                results["cases"].append({"implementation": implementation,
                    "isolated_two_worktrees": True, "git_registration_rediscovered": True,
                    "normal_cleanup_preserved_dirty_artifact": dirty_preserved,
                    "normal_remove_refusal": refusal, "force_helper": force,
                    "committed_artifact_readable_after_cleanup": True})
        finally:
            os.environ.clear()
            os.environ.update(previous_env)
    results["not_tested"] = ["Super/Worker dispatch or inference", "AO Go Create/Destroy/stash or daemon",
        "Codex native ID/resume", "daemon/worker crash recovery", "permissions/sandbox enforcement",
        "stop or unknown external side effects", "real account/model/cost", "macOS", "voice bridge"]
    print(json.dumps(results, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()
