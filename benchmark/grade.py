#!/usr/bin/env python3
"""Per-task graders. Runs the success criteria in each task dir and writes
grade.json next to result.json. Does NOT modify the workspace beyond running
short verification commands.

Usage: grade.py <tier_dir>
"""
import json, os, subprocess, sys, shlex, tempfile, shutil

def run(cmd, cwd, timeout=20, shell=False, input_text=None):
    try:
        p = subprocess.run(
            cmd if shell else shlex.split(cmd) if isinstance(cmd, str) else cmd,
            cwd=cwd, capture_output=True, timeout=timeout, shell=shell,
            input=input_text.encode() if input_text else None,
        )
        return p.returncode, p.stdout.decode(errors="replace"), p.stderr.decode(errors="replace")
    except subprocess.TimeoutExpired:
        return -1, "", "TIMEOUT"
    except Exception as e:
        return -2, "", repr(e)

def check(name, ok, detail=""):
    return {"name": name, "pass": bool(ok), "detail": detail}

# ---- per-task graders ----

def grade_tier1_01_hello(d):
    rc, out, err = run("python3 hello.py", d)
    return [check("runs", rc == 0, err),
            check("output equals 'hello, world\\n'", out == "hello, world\n", repr(out))]

def grade_tier1_02_word_count(d):
    sample = os.path.join(d, "sample.txt")
    if not os.path.exists(sample):
        return [check("sample.txt exists", False)]
    rc, out, err = run("python3 wc.py sample.txt", d)
    if rc != 0:
        return [check("script runs", False, err)]
    rc2, ref, _ = run(f"wc -w {shlex.quote(sample)}", d, shell=True)
    expected = ref.strip().split()[0] if ref.strip() else ""
    return [check("output is integer matching wc -w",
                  out.strip() == expected, f"got={out.strip()!r} want={expected!r}")]

def grade_tier1_03_json_pretty(d):
    rc, out, err = run('python3 pretty.py', d, input_text='{"b":2,"a":1}')
    if rc != 0:
        return [check("runs", False, err)]
    try:
        j = json.loads(out)
    except Exception as e:
        return [check("valid JSON", False, str(e))]
    keys_in_order = list(j.keys())
    return [check("valid JSON", True),
            check("keys sorted (a before b)", keys_in_order == ["a", "b"], str(keys_in_order)),
            check("2-space indent present", '  "a"' in out or '\n  "' in out, repr(out[:80]))]

def grade_tier1_04_grep_pattern(d):
    data = os.path.join(d, "data.txt")
    if not os.path.exists(data):
        return [check("data.txt exists", False)]
    rc, out, err = run("python3 mygrep.py '(?i)error' data.txt", d, shell=True)
    if rc != 0:
        return [check("runs", False, err)]
    lines = [l for l in out.split("\n") if l]
    return [check("only matching lines",
                  all("error" in l.lower() for l in lines) and len(lines) >= 1,
                  repr(lines))]

def grade_tier1_05_csv_column(d):
    csvf = os.path.join(d, "people.csv")
    if not os.path.exists(csvf):
        return [check("people.csv exists", False)]
    rc, out, err = run("python3 col.py people.csv age", d)
    if rc != 0:
        return [check("runs", False, err)]
    lines = [l for l in out.strip().split("\n") if l]
    return [check("4 lines printed", len(lines) == 4, repr(lines)),
            check("all integers", all(l.strip().isdigit() for l in lines), repr(lines))]

def grade_tier1_06_dedupe(d):
    rc, out, err = run("python3 dedupe.py", d, input_text="a\nb\na\nc\nb\n")
    return [check("output abc preserve order",
                  out == "a\nb\nc\n", repr(out))]

def grade_tier1_07_extract_emails(d):
    txt = os.path.join(d, "text.txt")
    if not os.path.exists(txt):
        return [check("text.txt exists", False)]
    rc, out, err = run("python3 emails.py < text.txt", d, shell=True)
    if rc != 0:
        return [check("runs", False, err)]
    lines = [l.strip() for l in out.strip().split("\n") if l.strip()]
    return [check("3 unique emails", len(set(lines)) == 3 and len(lines) == 3, repr(lines)),
            check("sorted", lines == sorted(lines), repr(lines)),
            check("look like emails", all("@" in l and "." in l for l in lines), repr(lines))]

def grade_tier1_08_reverse_lines(d):
    rc, out, err = run("python3 revlines.py", d, input_text="1\n2\n3\n")
    return [check("reversed", out == "3\n2\n1\n", repr(out))]

def grade_tier1_09_dir_size(d):
    sample = os.path.join(d, "sample")
    if not os.path.isdir(sample):
        return [check("sample/ exists", False)]
    rc, out, err = run("python3 dirsize.py sample", d)
    if rc != 0:
        return [check("runs", False, err)]
    rc2, ref, _ = run("du -sb sample | cut -f1", d, shell=True)
    return [check("matches du -sb",
                  out.strip() == ref.strip(),
                  f"got={out.strip()!r} want={ref.strip()!r}")]

def grade_tier1_10_temp_convert(d):
    rc1, o1, _ = run("python3 temp.py 100 C", d)
    rc2, o2, _ = run("python3 temp.py 32 F", d)
    return [check("100C -> 212.00", rc1 == 0 and o1.strip() == "212.00", repr(o1)),
            check("32F -> 0.00", rc2 == 0 and o2.strip() == "0.00", repr(o2))]

# Tier 2

def grade_tier2_01_fix_off_by_one(d):
    rc, out, err = run("node buggy.js", d)
    if rc != 0:
        return [check("runs", False, err)]
    expected = "[4,5]\n[1,2,3]\n[]\n[]\n"
    return [check("output matches", out == expected, repr(out))]

def grade_tier2_02_csv_stats(d):
    rc, out, err = run("python3 stats.py", d)
    if rc != 0:
        return [check("runs", False, err)]
    lines = out.strip().split("\n")
    keys = [l.split(":", 1)[0] for l in lines]
    return [check("5 lines", len(lines) == 5, repr(lines)),
            check("keys correct",
                  keys == ["count", "mean", "median", "stdev", "top3"], repr(keys)),
            check("top3 has 3 names",
                  "top3:" in out and len(out.split("top3:")[1].strip().split(",")) == 3,
                  out)]

def grade_tier2_03_log_parser(d):
    rc, out, err = run("python3 parse.py", d)
    if rc != 0:
        return [check("runs", False, err)]
    return [check("expected output",
                  out.strip() == "bob: 2\ndave: 1", repr(out))]

def grade_tier2_04_recursive_fib(d):
    rc, out, _ = run('python3 -c "from fib import fib; print(fib(10))"', d)
    rc2, out2, err2 = run('python3 -c "from fib import fib; print(fib(100))"', d, timeout=2)
    src = ""
    p = os.path.join(d, "fib.py")
    if os.path.exists(p):
        src = open(p).read()
    return [check("fib(10)==55", out.strip() == "55", repr(out)),
            check("fib(100) fast & correct",
                  out2.strip() == "354224848179261915075", repr(out2) + " err=" + repr(err2)),
            check("uses def fib", "def fib" in src)]

def grade_tier2_05_url_validator(d):
    p = os.path.join(d, "urlcheck.py")
    src = open(p).read() if os.path.exists(p) else ""
    cases = [
        ("https://example.com/x", True),
        ("ftp://example.com", False),
        ("http://no dots", False),
        ("http://x.y", True),
        ("", False),
    ]
    out = []
    for u, expected in cases:
        rc, o, err = run(
            f'python3 -c "from urlcheck import is_valid; print(is_valid({u!r}))"', d, shell=True)
        got = o.strip()
        out.append(check(f"is_valid({u!r}) == {expected}",
                        got == str(expected),
                        f"got={got!r} stderr={err[:80]!r}"))
    out.append(check("no urllib in source",
                    "urllib" not in src and "urlparse" not in src))
    return out

def grade_tier2_06_flatten_json(d):
    rc, out, err = run("python3 flatten.py", d)
    expected = ("active=True\ntags.0=a\ntags.1=b\nuser.addr.city=paris\n"
                "user.addr.zip=75001\nuser.name=alice\n")
    return [check("exact output",
                  out == expected or out == expected.rstrip() + "\n" or out == expected.rstrip(),
                  repr(out))]

def grade_tier2_07_topo_sort(d):
    rc, out, err = run("python3 topo.py", d)
    return [check("output abcde",
                  out.strip() == "a\nb\nc\nd\ne", repr(out))]

def grade_tier2_08_balanced_parens(d):
    cases = [("([]{})", True), ("([)]", False), ("hello (world)", True),
             ("(", False), ("", True)]
    res = []
    for s, expected in cases:
        rc, o, _ = run(f'python3 -c "from balanced import balanced; print(balanced({s!r}))"', d, shell=True)
        res.append(check(f"balanced({s!r})=={expected}", o.strip() == str(expected), repr(o)))
    return res

def grade_tier2_09_sql_to_csv(d):
    if not os.path.exists(os.path.join(d, "people.db")):
        return [check("people.db exists", False)]
    rc, out, err = run("python3 dump.py", d)
    if rc != 0:
        return [check("runs", False, err)]
    csv_p = os.path.join(d, "people.csv")
    if not os.path.exists(csv_p):
        return [check("people.csv produced", False)]
    lines = open(csv_p).read().strip().split("\n")
    return [check("header", lines[0] == "id,name,age,city", repr(lines[0])),
            check("ordered eve,bob,alice,dave,carol",
                  [l.split(",")[1] for l in lines[1:]] ==
                  ["eve", "bob", "alice", "dave", "carol"], repr(lines))]

def grade_tier2_10_state_machine(d):
    rc, out, err = run('python3 -c "from vending import Vending; v=Vending(); v.insert_coin(); v.select(\'soda\'); v.dispense(); print(v.state)"', d, shell=True)
    rc2, _, err2 = run('python3 -c "from vending import Vending; v=Vending(); v.dispense()"', d, shell=True)
    return [check("happy path -> IDLE", out.strip() == "IDLE", repr(out) + repr(err)),
            check("dispense from idle raises", rc2 != 0 and "ValueError" in err2, repr(err2))]

# Tier 3

def grade_tier3_01_rename_symbol(d):
    rc, out, err = run("grep -r 'compute_total' . --exclude-dir=_seed --exclude=TASK.md --exclude='result.json' --exclude='events.jsonl' --exclude='grade.json'", d, shell=True)
    rc2, out2, err2 = run("python3 main.py", d)
    return [check("compute_total fully renamed", rc != 0 or out.strip() == "", repr(out)),
            check("main.py runs", rc2 == 0, err2),
            check("output looks like 'total: <num>'", "total:" in out2, repr(out2))]

def grade_tier3_02_write_test_suite(d):
    rc, out, err = run("python3 -m pytest -q test_calc.py --no-header", d, timeout=60)
    return [check("pytest exits 0", rc == 0, err[-300:]),
            check(">=8 tests passed",
                  any(f"{n} passed" in out for n in range(8, 50)), out[-300:])]

def grade_tier3_03_http_server(d):
    rc, out, err = run("bash verify.sh", d, timeout=30)
    return [check("verify.sh exits 0 with OK",
                  rc == 0 and "OK" in out, f"out={out[-200:]!r} err={err[-200:]!r}")]

def grade_tier3_04_resolve_merge(d):
    p = os.path.join(d, "conflicted.py")
    src = open(p).read() if os.path.exists(p) else ""
    cases = [
        ("greet('alice','en')", "Hello, alice!"),
        ("greet('alice','fr')", "Bonjour, alice!"),
        ("greet('alice','jp')", "Hi, alice"),
        ("shout('alice')", "HELLO, ALICE!"),
    ]
    res = [check("no conflict markers",
                 not any(m in src for m in ("<<<<<<<", "=======", ">>>>>>>")))]
    for expr, expected in cases:
        rc, o, _ = run(f'python3 -c "from conflicted import greet, shout; print({expr})"', d, shell=True)
        res.append(check(f"{expr} == {expected!r}", o.strip() == expected, repr(o)))
    return res

def grade_tier3_05_json_to_yaml(d):
    p = os.path.join(d, "j2y.py")
    src = open(p).read() if os.path.exists(p) else ""
    res = [check("source has no 'yaml'", "yaml" not in src)]
    rc, out, err = run("python3 j2y.py", d)
    if rc != 0:
        res.append(check("runs", False, err))
        return res
    # Try parse-back via pyyaml.
    rc2, parsed, err2 = run(
        "python3 -c \"import sys,yaml,json; print(json.dumps(yaml.safe_load(sys.stdin.read()), sort_keys=True))\"",
        d, shell=True, input_text=out)
    rc3, ref, _ = run(
        "python3 -c \"import json; print(json.dumps(json.load(open('data.json')), sort_keys=True))\"",
        d, shell=True)
    res.append(check("yaml parses back to same dict",
                    rc2 == 0 and parsed.strip() == ref.strip(),
                    f"parsed={parsed.strip()[:120]!r} ref={ref.strip()[:120]!r}"))
    return res

def grade_tier3_06_find_dead_code(d):
    p = os.path.join(d, "dead.txt")
    if not os.path.exists(p):
        return [check("dead.txt exists", False)]
    content = open(p).read().strip().split("\n")
    expected = ["mod_a:dead_helper", "mod_b:orphan"]
    return [check("contents match",
                  sorted(l.strip() for l in content if l.strip()) == expected,
                  repr(content))]

def grade_tier3_07_add_cli_flag(d):
    sample = os.path.join(d, "sample.txt")
    with open(sample, "w") as f:
        f.write("hello\nworld\n")
    rc, out1, _ = run("python3 script.py sample.txt", d)
    rc2, out2, _ = run("python3 script.py --upper sample.txt", d)
    rc3, _, _ = run("python3 script.py", d)
    return [check("plain echo", rc == 0 and out1 == "hello\nworld\n", repr(out1)),
            check("--upper", rc2 == 0 and out2 == "HELLO\nWORLD\n", repr(out2)),
            check("no args -> nonzero", rc3 != 0)]

def grade_tier3_08_dependency_graph(d):
    p = os.path.join(d, "deps.txt")
    if not os.path.exists(p):
        return [check("deps.txt exists", False)]
    lines = sorted(l.strip() for l in open(p).read().strip().split("\n") if l.strip())
    expected = sorted(["a -> b", "a -> c", "b -> c", "b -> d", "c -> d"])
    return [check("edges match", lines == expected, repr(lines))]

def grade_tier3_09_extract_helper(d):
    p = os.path.join(d, "dup.py")
    if not os.path.exists(p):
        return [check("dup.py exists", False)]
    src = open(p).read()
    has_helper = ("def _report" in src) or ("def _render" in src)
    rc, out, _ = run("python3 -c \"from dup import report_users; print(report_users(['a','b']))\"", d, shell=True)
    expected = "REPORT:\n========\n- a\n- b\n========"
    return [check("private helper defined", has_helper, src[:300]),
            check("report_users behavior preserved", out.strip() == expected, repr(out))]

def grade_tier3_10_instrument_logging(d):
    rc, out, err = run("python3 svc.py", d)
    info_count = sum(1 for l in err.split("\n") if l.startswith("INFO"))
    rc2, out2, err2 = run('python3 -c "from svc import pipeline; print(pipeline(7))"', d, shell=True)
    return [check("6 INFO lines on stderr", info_count == 6, f"got={info_count} err={err[-200:]!r}"),
            check("import path returns True cleanly",
                  out2.strip() == "True", repr(out2) + " err=" + repr(err2))]

# Tier 4

def grade_tier4_01_todo_cli(d):
    script = """
set -e
rm -f todos.json
python3 todo.py list >/dev/null
[ "$(python3 todo.py add 'buy milk')" = "1" ]
[ "$(python3 todo.py add 'walk dog')" = "2" ]
python3 todo.py done 1
out="$(python3 todo.py list)"
echo "$out" | grep -q '^1 \\[x\\] buy milk$'
echo "$out" | grep -q '^2 \\[ \\] walk dog$'
python3 todo.py rm 2
[ "$(python3 todo.py list | wc -l)" = "1" ]
python3 todo.py add 'task3' >/dev/null
python3 todo.py clear
out="$(python3 todo.py list)"
echo "$out" | grep -q '^3 \\[ \\] task3$'
[ "$(echo "$out" | wc -l)" = "1" ]
echo OK
"""
    with tempfile.NamedTemporaryFile("w", suffix=".sh", delete=False) as f:
        f.write(script); name=f.name
    try:
        rc, out, err = run(f"bash {shlex.quote(name)}", d, shell=True, timeout=30)
    finally:
        os.unlink(name)
    return [check("end-to-end script passes",
                  rc == 0 and "OK" in out,
                  f"rc={rc} out={out[-300:]!r} err={err[-300:]!r}")]

def grade_tier4_02_lru_cache(d):
    rc, out, err = run("python3 -m pytest -q test_lru.py --no-header", d, timeout=30)
    rc2, o2, _ = run('python3 -c "from lru import LRU; c=LRU(2); c.put(1,\'a\'); c.put(2,\'b\'); c.get(1); c.put(3,\'c\'); print(c.keys_in_order())"', d, shell=True)
    return [check("pytest passes", rc == 0, err[-300:]),
            check(">=6 tests passed",
                  any(f"{n} passed" in out for n in range(6, 50)), out[-300:]),
            check("eviction order correct", o2.strip() == "[3, 1]", repr(o2))]

def grade_tier4_03_markdown_subset(d):
    code = """from md import to_html
out = to_html('# Hi\\n\\nHello **world** and *peace* and `code`.\\n\\n- one\\n- two\\n\\n[link](http://x.y)')
print(out)"""
    rc, out, err = run(f'python3 -c {shlex.quote(code)}', d, shell=True)
    if rc != 0:
        return [check("runs", False, err)]
    needles = ["<h1>Hi</h1>", "<strong>world</strong>", "<em>peace</em>",
               "<code>code</code>", "<ul>", "<li>one</li>", "<li>two</li>",
               '<a href="http://x.y">link</a>']
    return [check(f"contains {n!r}", n in out, out[-300:]) for n in needles]

def grade_tier4_04_job_queue_go(d):
    if not os.path.exists(os.path.join(d, "queue.go")):
        return [check("queue.go exists", False)]
    rc, out, err = run("go run queue.go", d, timeout=60)
    if rc != 0:
        return [check("go run exits 0", False, err[-300:])]
    import re
    m = re.match(r"submitted=(\d+) completed=(\d+) failed=(\d+) panicked=(\d+)", out.strip())
    res = [check("output format", bool(m), repr(out))]
    if m:
        s, c, f, p = map(int, m.groups())
        res.append(check("submitted=100", s == 100, str(s)))
        res.append(check("c+f+p == 100", c + f + p == 100, f"{c}+{f}+{p}"))
    rc2, _, err2 = run("go run -race queue.go", d, timeout=120)
    res.append(check("no -race failures", rc2 == 0, err2[-300:]))
    return res

def grade_tier4_05_mini_lexer(d):
    code = ("from lex import tokenize\n"
            "toks = tokenize('let x = 3.5 + \"hi\" // comment\\nprint(x)')\n"
            "print([t[0] for t in toks])\n"
            "print([t[1] for t in toks if t[0]=='KEYWORD'])\n")
    rc, out, err = run(f'python3 -c {shlex.quote(code)}', d, shell=True)
    if rc != 0:
        return [check("runs", False, err)]
    lines = out.strip().split("\n")
    types_ok = lines and lines[0] == "['KEYWORD', 'IDENT', 'OP', 'NUMBER', 'OP', 'STRING', 'KEYWORD', 'OP', 'IDENT', 'OP']"
    kw_ok = len(lines) > 1 and lines[1] == "['let', 'print']"
    rc2, _, err2 = run("python3 -c \"from lex import tokenize; tokenize('@')\"", d, shell=True)
    return [check("token types in order", types_ok, repr(lines)),
            check("keywords let, print", kw_ok, repr(lines)),
            check("unexpected char raises ValueError",
                  rc2 != 0 and "ValueError" in err2 and "unexpected char" in err2, err2[-200:])]

def grade_tier4_06_diff_dirs(d):
    rc, out, err = run("python3 diff_dirs.py", d)
    if rc != 0:
        return [check("runs", False, err)]
    try:
        j = json.loads(out)
    except Exception as e:
        return [check("valid JSON", False, str(e) + " " + out[:200])]
    return [check("only_in_a", j.get("only_in_a") == ["only_a.txt"], repr(j.get("only_in_a"))),
            check("only_in_b", j.get("only_in_b") == ["only_b.txt"], repr(j.get("only_in_b"))),
            check("changed", j.get("changed") == ["changed.txt"], repr(j.get("changed"))),
            check("same", j.get("same") == ["keep.txt", "sub/nested.txt"], repr(j.get("same")))]

def grade_tier4_07_config_loader(d):
    with open(os.path.join(d, "local.yaml"), "w") as f:
        f.write("port: 9090\n")
    cmd = ('APP__DEBUG=true APP__DB__URL=postgres://x '
           'python3 -c "import json,config; print(json.dumps(config.load_config(), sort_keys=True))"')
    rc, out, err = run(cmd, d, shell=True)
    expected = '{"db": {"pool": 5, "url": "postgres://x"}, "debug": true, "host": "localhost", "port": 9090}'
    return [check("runs", rc == 0, err[-300:]),
            check("merged config exact", out.strip() == expected, repr(out))]

def grade_tier4_08_fix_broken_repo(d):
    rc, out, err = run("python3 -m pytest -q --no-header", d, timeout=30)
    return [check("pytest exits 0", rc == 0, err[-300:] + out[-300:]),
            check("4 tests passed", "4 passed" in out, out[-300:])]

def grade_tier4_09_retry_lib(d):
    rc, out, err = run("python3 -m pytest -q test_retry.py --no-header", d, timeout=30)
    return [check("pytest exits 0", rc == 0, err[-300:]),
            check(">=5 tests passed",
                  any(f"{n} passed" in out for n in range(5, 50)), out[-300:])]

def grade_tier4_10_minishell(d):
    script = "# demo\necho $NAME\nset NAME=alice\necho $NAME\ncd /tmp\npwd\nexit 0\n"
    p = os.path.join(d, "_t.sh")
    with open(p, "w") as f:
        f.write(script)
    rc, out, err = run("python3 minish.py _t.sh", d)
    expected = "\nalice\n/tmp\n"
    res = [check("happy path output", rc == 0 and out == expected, f"rc={rc} out={out!r}")]
    p2 = os.path.join(d, "_t2.sh")
    with open(p2, "w") as f:
        f.write("exit 7\n")
    rc2, _, _ = run("python3 minish.py _t2.sh", d)
    res.append(check("exit 7 propagates", rc2 == 7, str(rc2)))
    try: os.unlink(p); os.unlink(p2)
    except: pass
    return res

GRADERS = {
    "tier1": {
        "01_hello": grade_tier1_01_hello,
        "02_word_count": grade_tier1_02_word_count,
        "03_json_pretty": grade_tier1_03_json_pretty,
        "04_grep_pattern": grade_tier1_04_grep_pattern,
        "05_csv_column": grade_tier1_05_csv_column,
        "06_dedupe": grade_tier1_06_dedupe,
        "07_extract_emails": grade_tier1_07_extract_emails,
        "08_reverse_lines": grade_tier1_08_reverse_lines,
        "09_dir_size": grade_tier1_09_dir_size,
        "10_temp_convert": grade_tier1_10_temp_convert,
    },
    "tier2": {
        "01_fix_off_by_one": grade_tier2_01_fix_off_by_one,
        "02_csv_stats": grade_tier2_02_csv_stats,
        "03_log_parser": grade_tier2_03_log_parser,
        "04_recursive_fib": grade_tier2_04_recursive_fib,
        "05_url_validator": grade_tier2_05_url_validator,
        "06_flatten_json": grade_tier2_06_flatten_json,
        "07_topo_sort": grade_tier2_07_topo_sort,
        "08_balanced_parens": grade_tier2_08_balanced_parens,
        "09_sql_to_csv": grade_tier2_09_sql_to_csv,
        "10_state_machine": grade_tier2_10_state_machine,
    },
    "tier3": {
        "01_rename_symbol": grade_tier3_01_rename_symbol,
        "02_write_test_suite": grade_tier3_02_write_test_suite,
        "03_http_server": grade_tier3_03_http_server,
        "04_resolve_merge": grade_tier3_04_resolve_merge,
        "05_json_to_yaml": grade_tier3_05_json_to_yaml,
        "06_find_dead_code": grade_tier3_06_find_dead_code,
        "07_add_cli_flag": grade_tier3_07_add_cli_flag,
        "08_dependency_graph": grade_tier3_08_dependency_graph,
        "09_extract_helper": grade_tier3_09_extract_helper,
        "10_instrument_logging": grade_tier3_10_instrument_logging,
    },
    "tier4": {
        "01_todo_cli": grade_tier4_01_todo_cli,
        "02_lru_cache": grade_tier4_02_lru_cache,
        "03_markdown_subset": grade_tier4_03_markdown_subset,
        "04_job_queue_go": grade_tier4_04_job_queue_go,
        "05_mini_lexer": grade_tier4_05_mini_lexer,
        "06_diff_dirs": grade_tier4_06_diff_dirs,
        "07_config_loader": grade_tier4_07_config_loader,
        "08_fix_broken_repo": grade_tier4_08_fix_broken_repo,
        "09_retry_lib": grade_tier4_09_retry_lib,
        "10_minishell": grade_tier4_10_minishell,
    },
}

def main():
    if len(sys.argv) < 2:
        print("usage: grade.py <tier_dir>", file=sys.stderr); sys.exit(2)
    tier_dir = os.path.abspath(sys.argv[1])
    tier = os.path.basename(tier_dir)
    graders = GRADERS.get(tier, {})
    if not graders:
        print(f"unknown tier {tier}", file=sys.stderr); sys.exit(2)
    summary = []
    for name, fn in sorted(graders.items()):
        d = os.path.join(tier_dir, name)
        if not os.path.isdir(d):
            continue
        try:
            checks = fn(d)
        except Exception as e:
            checks = [check("grader crashed", False, repr(e))]
        passed = sum(1 for c in checks if c["pass"])
        total = len(checks)
        ok = passed == total
        with open(os.path.join(d, "grade.json"), "w") as f:
            json.dump({"task": name, "passed": passed, "total": total,
                       "ok": ok, "checks": checks}, f, indent=2)
        summary.append({"task": name, "passed": passed, "total": total, "ok": ok})
        print(f"  {'✓' if ok else '✗'}  {name:<28} {passed}/{total}")
    out = os.path.join(tier_dir, "grade_summary.json")
    with open(out, "w") as f:
        json.dump(summary, f, indent=2)
    n_ok = sum(1 for r in summary if r["ok"])
    print(f"{tier}: {n_ok}/{len(summary)} fully passing")
    print(f"summary: {out}")

if __name__ == "__main__":
    main()
