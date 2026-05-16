#!/usr/bin/env python3
"""Run all tasks in a tier in parallel via axon --prompt.

Usage: run_tier.py <tier_dir> [max_parallel]
Each task dir must contain TASK.md. Runner writes events.jsonl + result.json
in each task dir. Workspace is preserved (artifacts the agent created stay)
so the user can inspect.
"""
import json, os, subprocess, sys, time, shutil
from concurrent.futures import ThreadPoolExecutor, as_completed

AXON = "/home/zperson/axon/agent/axon"
PROMPT = (
    "Read TASK.md in the current directory. Complete the task end-to-end "
    "without asking any questions. When all success criteria are met, stop."
)
PROVIDER = "openrouter/deepseek/deepseek-v3.2"
TIMEOUT = 600  # seconds per task

def reset_workspace(task_dir):
    """Remove all artifacts; restore seed files from _seed/ if present.
    Always preserves TASK.md and _seed/."""
    keep = {"TASK.md", "_seed"}
    for name in os.listdir(task_dir):
        if name in keep:
            continue
        p = os.path.join(task_dir, name)
        if os.path.isdir(p):
            shutil.rmtree(p)
        else:
            os.remove(p)
    seed = os.path.join(task_dir, "_seed")
    if os.path.isdir(seed):
        for name in os.listdir(seed):
            src = os.path.join(seed, name)
            dst = os.path.join(task_dir, name)
            if os.path.isdir(src):
                shutil.copytree(src, dst)
            else:
                shutil.copy2(src, dst)

def run_task(task_dir):
    name = os.path.basename(task_dir)
    reset_workspace(task_dir)
    log_path = os.path.join(task_dir, "events.jsonl")
    result_path = os.path.join(task_dir, "result.json")
    data_dir = f"/tmp/axon-bench/{name}"
    if os.path.exists(data_dir):
        shutil.rmtree(data_dir)
    os.makedirs(data_dir, exist_ok=True)

    env = os.environ.copy()
    env["AXON_DATA_DIR"] = data_dir
    env["LLM_PROVIDER"] = PROVIDER
    env["LLM_PRUNER_PROVIDER"] = "off"

    t0 = time.time()
    timed_out = False
    try:
        proc = subprocess.run(
            [AXON, "--prompt", PROMPT, "--log-json", log_path],
            cwd=task_dir, env=env,
            stdout=subprocess.DEVNULL, stderr=subprocess.PIPE,
            timeout=TIMEOUT,
        )
        rc = proc.returncode
        stderr = proc.stderr.decode("utf-8", errors="replace")[-4000:]
    except subprocess.TimeoutExpired as e:
        rc = -1
        stderr = (e.stderr.decode("utf-8", errors="replace") if e.stderr else "") + "\n[TIMEOUT]"
        timed_out = True
    duration = round(time.time() - t0, 2)

    # Summarize events.
    events = []
    if os.path.exists(log_path):
        with open(log_path) as f:
            for line in f:
                line = line.strip()
                if not line:
                    continue
                try:
                    events.append(json.loads(line))
                except Exception:
                    pass

    counts = {}
    tool_calls = []
    final_text = ""
    turns = 0
    for ev in events:
        k = ev.get("kind", "")
        counts[k] = counts.get(k, 0) + 1
        if k == "tool_call":
            tool_calls.append(ev.get("name"))
        elif k == "assistant_text":
            final_text = ev.get("text", "")
        elif k == "turn_end":
            turns = max(turns, ev.get("turn", 0))

    result = {
        "task": name,
        "duration_sec": duration,
        "exit_code": rc,
        "timed_out": timed_out,
        "turns": turns,
        "event_counts": counts,
        "tool_call_sequence": tool_calls,
        "final_assistant_text": final_text,
        "stderr_tail": stderr if stderr.strip() else None,
    }
    with open(result_path, "w") as f:
        json.dump(result, f, indent=2)
    return result

def main():
    if len(sys.argv) < 2:
        print("usage: run_tier.py <tier_dir> [max_parallel]", file=sys.stderr)
        sys.exit(2)
    tier_dir = os.path.abspath(sys.argv[1])
    max_parallel = int(sys.argv[2]) if len(sys.argv) > 2 else 5

    tasks = sorted(
        os.path.join(tier_dir, d)
        for d in os.listdir(tier_dir)
        if os.path.isdir(os.path.join(tier_dir, d))
        and os.path.exists(os.path.join(tier_dir, d, "TASK.md"))
    )
    print(f"running {len(tasks)} tasks with parallel={max_parallel}")
    results = []
    with ThreadPoolExecutor(max_workers=max_parallel) as ex:
        futs = {ex.submit(run_task, t): t for t in tasks}
        for fut in as_completed(futs):
            r = fut.result()
            print(f"  [{r['exit_code']:>3}]  {r['task']:<25} "
                  f"{r['turns']:>2} turns  {r['duration_sec']:>5.1f}s  "
                  f"tools={','.join(r['tool_call_sequence']) or '-'}")
            results.append(r)

    summary_path = os.path.join(tier_dir, "tier_summary.json")
    with open(summary_path, "w") as f:
        json.dump(sorted(results, key=lambda r: r["task"]), f, indent=2)
    print(f"summary: {summary_path}")

if __name__ == "__main__":
    main()
