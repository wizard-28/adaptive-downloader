#!/usr/bin/env python3
"""Descriptive analysis for the live-internet pilot L-P1.

Implements the predeclared analysis in testdata/live_pilot_l1/protocol.md:
  1. per (server, slot) condition: best fixed cell and the top cell's identity
  2. cross-condition selection headroom: one global fixed cell vs the
     per-condition clairvoyant best (geometric mean, bootstrap over conditions)
  3. within-run throughput variability from the downloader's timelines
  4. aria2 and curl relative to the condition's best fixed cell (context only)

Usage: python3 tools/analyze_live_pilot.py [--raw DIR] [--out DIR] [--no-figures]
Every number is Descriptive; nothing here ranks modes or claims superiority.
"""
import argparse, glob, json, math, os, random, statistics, sys

ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), ".."))
DEF_DIR = os.path.join(ROOT, "testdata", "live_pilot_l1")
LINK_BOUND_SPREAD = 1.05  # all fixed cells within 5 %  -> ACCESS_LINK_BOUND
NEAR_TIE = 1.05
BOOT = 10000
BOOT_SEED = 20261018


def load(raw_dir):
    recs = []
    for path in sorted(glob.glob(os.path.join(raw_dir, "*.jsonl"))):
        with open(path) as f:
            for line in f:
                line = line.strip()
                if line:
                    recs.append(json.loads(line))
    return recs


def gmean(xs):
    return math.exp(sum(math.log(x) for x in xs) / len(xs))


def cond_key(r):
    return (r["server"], r["slot"])


def conditions(recs):
    """Map condition -> arm -> list of valid durations, plus invalid counts."""
    cond, invalid = {}, {}
    for r in recs:
        k = cond_key(r)
        if r.get("valid"):
            cond.setdefault(k, {}).setdefault(r["arm"], []).append(r["duration_s"])
        else:
            invalid.setdefault(k, []).append({"arm": r["arm"], "rep": r["rep"], "err": r.get("err", ""),
                                              "checksum_ok": r.get("checksum_ok", False)})
    return cond, invalid


def fixed_cells(recs):
    return sorted({r["arm"] for r in recs if r["kind"] == "ours"},
                  key=lambda a: (int(a.split("_")[1][1:]), int(a.split("_")[2][1:-1])))


def global_choice(table, cells):
    """Cell minimising the geometric mean of (cell / condition best) over the
    given conditions, and that geometric mean (= 1 + headroom)."""
    best = None
    for c in cells:
        g = gmean([row[c] / min(row.values()) for row in table])
        if best is None or g < best[1] - 1e-12:
            best = (c, g)
    return best


def headroom(table, cells):
    if not table:
        return None
    cell, g = global_choice(table, cells)
    rng = random.Random(BOOT_SEED)
    boots = []
    for _ in range(BOOT):
        sample = [table[rng.randrange(len(table))] for _ in table]
        boots.append(global_choice(sample, cells)[1])
    boots.sort()
    lo, hi = boots[int(0.025 * BOOT)], boots[int(0.975 * BOOT) - 1]
    near = sum(1 for row in table if row[cell] / min(row.values()) <= NEAR_TIE)
    return {"global_cell": cell, "n_conditions": len(table),
            "headroom_pct": 100 * (g - 1), "boot95_pct": [100 * (lo - 1), 100 * (hi - 1)],
            "conditions_within_5pct_of_best": near}


def variability(recs, timeline_ms_default=100):
    """Per ours run: 1 s throughput bins (first and last second excluded)."""
    per_server = {}
    for r in recs:
        if r["kind"] != "ours" or not r.get("valid") or not r.get("timeline_bytes"):
            continue
        w = r.get("timeline_ms") or timeline_ms_default
        per = max(1, round(1000 / w))
        tl = r["timeline_bytes"]
        bins = [sum(tl[i:i + per]) for i in range(0, len(tl) - per + 1, per)]
        bins = bins[1:-1]
        if len(bins) < 5:
            continue
        mu = statistics.mean(bins)
        med = statistics.median(bins)
        if mu <= 0:
            continue
        cv = statistics.pstdev(bins) / mu
        low = sum(1 for b in bins if b < 0.5 * med) / len(bins)
        per_server.setdefault(r["server"], []).append((cv, low))
    out = {}
    for s, v in sorted(per_server.items()):
        out[s] = {"runs": len(v), "median_cv": statistics.median(x[0] for x in v),
                  "median_frac_below_half_median": statistics.median(x[1] for x in v)}
    return out


def analyze(recs):
    cells = fixed_cells(recs)
    cond, invalid = conditions(recs)
    per_cond, table, table_nolink, incomplete = [], [], [], []
    for k in sorted(cond):
        arms = cond[k]
        row = {c: gmean(arms[c]) for c in cells if c in arms}
        entry = {"server": k[0], "slot": k[1], "valid_runs": sum(len(v) for v in arms.values()),
                 "invalid_runs": len(invalid.get(k, []))}
        if len(row) < len(cells):
            entry["status"] = "INCOMPLETE"
            entry["missing_cells"] = [c for c in cells if c not in row]
            incomplete.append(entry)
            per_cond.append(entry)
            continue
        best = min(row, key=row.get)
        spread = max(row.values()) / min(row.values())
        ranked = sorted(row, key=row.get)
        entry.update({
            "status": "ACCESS_LINK_BOUND" if spread <= LINK_BOUND_SPREAD else "OK",
            "best_cell": best, "best_s": row[best], "spread_max_over_min": spread,
            "ranking": ranked,
            "cells_within_5pct": [c for c in ranked if row[c] / row[best] <= NEAR_TIE],
            "time_s": row,
        })
        for ext in ("aria2", "curl"):
            if ext in arms:
                entry[ext + "_over_best_fixed"] = gmean(arms[ext]) / row[best]
        per_cond.append(entry)
        table.append(row)
        if entry["status"] == "OK":
            table_nolink.append(row)

    complete = [e for e in per_cond if e["status"] != "INCOMPLETE"]
    by_server = {}
    for e in complete:
        by_server.setdefault(e["server"], []).append(e["best_cell"])
    stability = {
        "distinct_top_cells_overall": sorted({e["best_cell"] for e in complete}),
        "per_server_top_cells": {s: v for s, v in sorted(by_server.items())},
        "per_server_distinct": {s: len(set(v)) for s, v in sorted(by_server.items())},
    }
    return {
        "pilot": "live_pilot_l1",
        "label": "Descriptive",
        "fixed_cells": cells,
        "n_records": len(recs),
        "n_valid": sum(1 for r in recs if r.get("valid")),
        "n_invalid": sum(1 for r in recs if not r.get("valid")),
        "invalid_by_condition": {f"{k[0]}|{k[1]}": v for k, v in sorted(invalid.items())},
        "conditions": per_cond,
        "top_cell_stability": stability,
        "headroom_all_complete": headroom(table, cells),
        "headroom_excluding_access_link_bound": headroom(table_nolink, cells),
        "within_run_variability": variability(recs),
    }


def figures(res, out_dir):
    try:
        import matplotlib
        matplotlib.use("Agg")
        import matplotlib.pyplot as plt
    except ImportError:
        print("matplotlib unavailable; skipping figures", file=sys.stderr)
        return
    conds = [e for e in res["conditions"] if e["status"] != "INCOMPLETE"]
    if not conds:
        return
    cells = res["fixed_cells"]
    mat = [[e["time_s"][c] / e["best_s"] for e in conds] for c in cells]
    fig, ax = plt.subplots(figsize=(max(6, 0.9 * len(conds) + 2), 0.45 * len(cells) + 1.6))
    im = ax.imshow(mat, aspect="auto", cmap="viridis_r", vmin=1.0, vmax=max(2.0, max(max(r) for r in mat)) if mat else 2)
    ax.set_yticks(range(len(cells)), [c.replace("ours_", "") for c in cells])
    ax.set_xticks(range(len(conds)), [f"{e['server']}\n{e['slot']}" for e in conds], fontsize=8)
    for j, e in enumerate(conds):
        i = cells.index(e["best_cell"])
        ax.text(j, i, "best", ha="center", va="center", color="white", fontsize=7)
        if e["status"] == "ACCESS_LINK_BOUND":
            ax.text(j, -0.75, "link-bound", ha="center", fontsize=6)
    fig.colorbar(im, ax=ax, label="completion time / condition best")
    ax.set_title("L-P1 live pilot: fixed (N,S) cells per condition (Descriptive)")
    fig.tight_layout()
    fig.savefig(os.path.join(out_dir, "fig_lp1_cells_by_condition.png"), dpi=150)
    plt.close(fig)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--raw", default=os.path.join(DEF_DIR, "raw"))
    ap.add_argument("--out", default=os.path.join(DEF_DIR, "analysis"))
    ap.add_argument("--no-figures", action="store_true")
    a = ap.parse_args()
    recs = load(a.raw)
    if not recs:
        sys.exit("no records under " + a.raw)
    res = analyze(recs)
    os.makedirs(a.out, exist_ok=True)
    with open(os.path.join(a.out, "summary.json"), "w") as f:
        json.dump(res, f, indent=2, sort_keys=True)
        f.write("\n")
    if not a.no_figures:
        figures(res, a.out)
    h = res["headroom_all_complete"]
    print(f"records {res['n_records']} valid {res['n_valid']} invalid {res['n_invalid']}")
    for e in res["conditions"]:
        if e["status"] == "INCOMPLETE":
            print(f"  {e['server']:>8} {e['slot']}: INCOMPLETE missing {e['missing_cells']}")
        else:
            print(f"  {e['server']:>8} {e['slot']}: best {e['best_cell']} {e['best_s']:.1f}s "
                  f"spread {e['spread_max_over_min']:.2f} {e['status']}")
    if h:
        print(f"headroom (global {h['global_cell']}): {h['headroom_pct']:.1f}% "
              f"boot95 [{h['boot95_pct'][0]:.1f}, {h['boot95_pct'][1]:.1f}] over {h['n_conditions']} conditions")


if __name__ == "__main__":
    main()
