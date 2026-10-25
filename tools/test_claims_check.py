#!/usr/bin/env python3
"""Self-test: the claims checker must FAIL when a paper number or table entry
is deliberately wrong. Usage: python3 tools/test_claims_check.py"""
import json, os, subprocess, sys, tempfile

ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), ".."))
CHK = [sys.executable, os.path.join(ROOT, "tools", "claims_check.py")]
paper = open(os.path.join(ROOT, "reports", "paper_draft.md")).read()
nums = json.load(open(os.path.join(ROOT, "tools", "paper_numbers.json")))
fail = 0

def run(args):
    return subprocess.run(CHK + args, capture_output=True, text=True)

def expect(cond, msg):
    global fail
    print(("PASS " if cond else "FAIL ") + msg)
    fail += (not cond)

r = run([])
expect(r.returncode == 0, "unmodified paper and numbers pass")

with tempfile.TemporaryDirectory() as d:
    # 1. wrong expected value in the numbers file
    bad = json.loads(json.dumps(nums)); bad["numbers"]["d2.geo_ratio"]["value"] = 0.41
    pth = os.path.join(d, "n.json"); json.dump(bad, open(pth, "w"))
    expect(run(["--numbers", pth]).returncode != 0, "wrong expected number is rejected")
    # 2. paper with a changed headline ratio
    for old, new, label in (("0.3756", "0.3956", "D2 headline ratio"),
                            ("0.766 [0.762, 0.769]" if "0.766 [0.762, 0.769]" in paper else "0.766", "0.716", "P2 ratio"),
                            ("28 of 30", "29 of 30", "sweep win count"),
                            ("0.863", "0.763", "NGINX ratio"),
                            ("0.770", "0.670", "family ratio"),
                            ("(0.599\n  [0.598, 0.601])", "(0.699\n  [0.598, 0.601])", "engine ablation splitting ratio"),
                            ("1.356 [1.252, 1.472]", "1.256 [1.252, 1.472]", "frozen DQN on capped server")):
        if old not in paper:
            expect(False, f"{label}: anchor {old!r} not found in paper"); continue
        pp = os.path.join(d, "p.md"); open(pp, "w").write(paper.replace(old, new, 1))
        r = run(["--paper", pp])
        expect(r.returncode != 0 and ("UNVERIFIED" in r.stdout or "ANCHOR MISSING" in r.stdout), f"paper edit rejected: {label} {old} -> {new}")
sys.exit(1 if fail else 0)
