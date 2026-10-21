#!/usr/bin/env python3
"""Cylo isolated research PoC; upstream source clone required; no real model calls.

Uses a fresh temporary CODEX_HOME/repo and an unavailable loopback-only provider.
Deletes only the disposable worktree it creates. Does not install dependencies.
"""
import importlib.util, json, os, pathlib, select, subprocess, tempfile, time

root = tempfile.mkdtemp(prefix="cylo-research-repro-")
repo = root + "/repo"
os.mkdir(repo)
subprocess.run(["git", "init", "-q", repo], check=True)
subprocess.run(["git", "-C", repo, "-c", "user.name=PoC", "-c",
                "user.email=poc@example.invalid", "commit", "-qm", "init",
                "--allow-empty"], check=True)
offline = ['-c', 'model_provider="cylo_offline"', '-c',
    'model_providers.cylo_offline={name="Cylo offline",'
    'base_url="http://127.0.0.1:1/v1",wire_api="responses",'
    'requires_openai_auth=false,request_max_retries=0,stream_max_retries=0}']

def start(home):
    # Do not inherit provider credentials or user Codex overrides.
    env = {k: v for k, v in os.environ.items()
           if not k.startswith(("OPENAI_", "CODEX_", "AZURE_OPENAI_"))}
    env["CODEX_HOME"] = home
    os.makedirs(home, exist_ok=True)
    return subprocess.Popen(["codex", "app-server", *offline], cwd=repo, env=env,
        stdin=subprocess.PIPE, stdout=subprocess.PIPE, text=True,
        stderr=open(root + "/stderr", "a"))

def rpc(p, number, method, params):
    p.stdin.write(json.dumps({"id": number, "method": method, "params": params}) + "\n")
    p.stdin.flush()
    end = time.monotonic() + 15
    while time.monotonic() < end:
        if select.select([p.stdout], [], [], 1)[0]:
            value = json.loads(p.stdout.readline())
            if value.get("id") == number:
                print(method, json.dumps(value, ensure_ascii=False))
                return value
    raise TimeoutError(method)

def init(p):
    rpc(p, 0, "initialize", {"clientInfo": {"name": "cylo_research", "version": "0.0.0"}})
    p.stdin.write('{"method":"initialized"}\n')
    p.stdin.flush()

for with_turn in [False, True]:
    home = root + ("/with-turn" if with_turn else "/empty")
    p = start(home)
    init(p)
    value = rpc(p, 1, "thread/start", {"cwd": repo, "approvalPolicy": "never",
                "sandbox": "read-only", "ephemeral": False})
    tid = value["result"]["thread"]["id"]
    if with_turn:
        rpc(p, 2, "turn/start", {"threadId": tid, "input": [{"type": "text",
            "text": "Cylo isolated offline persistence probe; no external operation requested."}]})
        time.sleep(1)
    rpc(p, 3, "thread/list", {"sourceKinds": ["cli", "vscode", "appServer", "exec"]})
    p.terminate()
    p.wait(timeout=15)
    p = start(home)
    init(p)
    rpc(p, 4, "thread/resume", {"threadId": tid, "excludeTurns": True})
    if with_turn:
        rpc(p, 5, "thread/turns/list", {"threadId": tid, "itemsView": "full"})
    p.terminate()
    p.wait(timeout=15)
    print("CASE", with_turn, "CODEX_HOME", home, "THREAD", tid, "REPO", repo)

spec = importlib.util.spec_from_file_location("upstream_worktree",
    "/tmp/cylo-research-cao/src/cli_agent_orchestrator/services/worktree_service.py")
w = importlib.util.module_from_spec(spec)
spec.loader.exec_module(w)
worktree = w.create_worktree(repo, "pocworker")
pathlib.Path(worktree + "/uncommitted.txt").write_text("disposable PoC")
print("worktree-before", os.path.exists(worktree + "/uncommitted.txt"))
w.remove_worktree(repo, "pocworker")
print("worktree-after", os.path.exists(worktree))
