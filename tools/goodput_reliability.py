#!/usr/bin/env python3
"""Goodput and reliability per scenario and arm, from existing run records.

Usage: goodput_reliability.py RUNS_DIR [RUNS_DIR ...] OUT_JSON

Definitions (computed only from fields the harness records):
- goodput_mbs: useful bytes (the file size) / completion time, in MB/s (10^6 B).
- wasted_frac (ours only): bytes read by failed attempts / file size, i.e.
  (client_bytes - output size) / output size. aria2 exposes no client bytes.
- failed_attempts, retries (ours only): from the downloader's own metrics.
- server_failed: requests the test server counted as not completed (errors or
  connections the client closed early; aria2 closes pieces it re-splits).
Only valid runs (content verified, no error) are summarised; invalid runs are
counted. Standard library only.
"""
import glob, json, math, os, statistics, sys


def geo(xs):
    return math.exp(sum(math.log(x) for x in xs) / len(xs))


def summarise(dirs):
    cells, invalid = {}, {}
    for d in dirs:
        study = os.path.basename(os.path.dirname(os.path.abspath(d)))
        for p in sorted(glob.glob(os.path.join(d, "*.json"))):
            r = json.load(open(p))
            sc, _, arm = r["run_id"].split("_", 2)
            key = f"{study}/{sc}/{arm}"
            if not (r.get("content_verified") is True and not r.get("err")):
                invalid[key] = invalid.get(key, 0) + 1
                continue
            size, t = r["output_size_bytes"], r["completion_ms"] / 1000
            c = cells.setdefault(key, {"goodput": [], "wasted": [], "failed": [], "retries": [],
                                       "server_failed": [], "requests": []})
            c["goodput"].append(size / t / 1e6)
            c["server_failed"].append(r.get("failed_requests", 0))
            c["requests"].append(r.get("total_requests", 0))
            if r.get("system") == "ours":
                c["wasted"].append(max(0, r["client_bytes"] - size) / size)
                c["failed"].append(r["client_failed"])
                c["retries"].append(r["client_retries"])
    out = {}
    for k, c in sorted(cells.items()):
        out[k] = {
            "n": len(c["goodput"]),
            "goodput_mbs_geo": round(geo(c["goodput"]), 2),
            "requests_mean": round(statistics.mean(c["requests"]), 1),
            "server_failed_mean": round(statistics.mean(c["server_failed"]), 1),
            "wasted_frac_mean": round(statistics.mean(c["wasted"]), 4) if c["wasted"] else None,
            "failed_attempts_mean": round(statistics.mean(c["failed"]), 1) if c["failed"] else None,
            "retries_mean": round(statistics.mean(c["retries"]), 1) if c["retries"] else None,
        }
    return {"cells": out, "invalid_runs": invalid}


def main():
    *dirs, out = sys.argv[1:]
    res = summarise(dirs)
    os.makedirs(os.path.dirname(os.path.abspath(out)), exist_ok=True)
    json.dump(res, open(out, "w"), indent=1)
    print(f"{'cell':55s} {'n':>3s} {'MB/s':>6s} {'req':>6s} {'srvfail':>7s} {'waste%':>7s} {'fail':>5s} {'retry':>5s}")
    for k, v in res["cells"].items():
        w = "-" if v["wasted_frac_mean"] is None else f"{100 * v['wasted_frac_mean']:.2f}"
        f = "-" if v["failed_attempts_mean"] is None else v["failed_attempts_mean"]
        rt = "-" if v["retries_mean"] is None else v["retries_mean"]
        print(f"{k:55s} {v['n']:3d} {v['goodput_mbs_geo']:6.2f} {v['requests_mean']:6.1f} {v['server_failed_mean']:7.1f} {w:>7s} {f!s:>5s} {rt!s:>5s}")
    print("invalid:", res["invalid_runs"])


if __name__ == "__main__":
    main()
