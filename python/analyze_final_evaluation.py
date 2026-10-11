"""Paired analysis of the final DQN-vs-aria2 evaluation (frozen raw results).

Pre-specified methodology (locked before results):
  primary unit = matched pair; effect = log(DQN_time/aria2_time);
  paired permutation test (one-sided, DQN faster) for the aggregate and per
  condition; Holm correction across the 8 conditions. No post-hoc tuning.
"""

from __future__ import annotations

import argparse
import json
import math
import os
import random
import statistics as st

DEFAULT_CONDITIONS = ["U1", "U2", "U3", "U4", "U5", "U6", "U7", "U8"]
PERM_SEED = 20260925


def geom_mean(xs):
    return math.exp(sum(math.log(x) for x in xs) / len(xs)) if xs else 0.0


def t_crit(n: int) -> float:
    # two-sided 95% t critical, df = n-1 (small table covering n=30)
    table = {29: 2.045, 30: 2.045, 28: 2.048}
    return table.get(n - 1, 2.045)


def paired_permutation(deltas, iters=200000, seed=PERM_SEED):
    """One-sided p-value that mean(delta) < 0 (DQN faster)."""
    obs = st.mean(deltas)
    rng = random.Random(seed)
    n = len(deltas)
    count = 0
    for _ in range(iters):
        s = 0.0
        for d in deltas:
            s += -d if rng.random() < 0.5 else d
        if s / n <= obs:
            count += 1
    return (count + 1) / (iters + 1)


def holm(pvals):
    order = sorted(range(len(pvals)), key=lambda i: pvals[i])
    m = len(pvals)
    out = [0.0] * m
    prev = 0.0
    for rank, i in enumerate(order):
        adj = min(1.0, (m - rank) * pvals[i])
        adj = max(adj, prev)
        out[i] = adj
        prev = adj
    return out


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--dir", default="testdata/final_evaluation")
    ap.add_argument("--conditions", default=",".join(DEFAULT_CONDITIONS),
                    help="comma-separated condition IDs (default: the U1-U8 matrix)")
    args = ap.parse_args()
    CONDITIONS = [c for c in args.conditions.split(",") if c]
    root = os.path.abspath(os.path.join(os.path.dirname(__file__), ".."))
    base = os.path.join(root, args.dir)

    raw = json.load(open(os.path.join(base, "raw_results.json")))
    runs = raw["results"]
    by_pair = {}
    for r in runs:
        by_pair.setdefault(r["pair_id"], {})[r["system"]] = r

    pairs = []
    for pid, m in by_pair.items():
        if "dqn" not in m or "aria2" not in m:
            continue
        d, a = m["dqn"], m["aria2"]
        pairs.append(
            {
                "pair_id": pid,
                "condition": d["condition"],
                "dqn_ms": d["duration_ms"],
                "aria2_ms": a["duration_ms"],
                "delta_ms": d["duration_ms"] - a["duration_ms"],
                "ratio": d["duration_ms"] / a["duration_ms"],
                "reduction": (a["duration_ms"] - d["duration_ms"]) / a["duration_ms"],
            }
        )

    # ---- aggregate ----
    logratios = [math.log(p["ratio"]) for p in pairs]
    deltas = [p["delta_ms"] for p in pairs]
    n = len(pairs)
    mean_lr = st.mean(logratios)
    sd_lr = st.stdev(logratios)
    se = sd_lr / math.sqrt(n)
    ci = (mean_lr - t_crit(n) * se, mean_lr + t_crit(n) * se)
    geo = math.exp(mean_lr)
    p_agg = paired_permutation(deltas)
    dqn_mean = st.mean([p["dqn_ms"] for p in pairs])
    aria2_mean = st.mean([p["aria2_ms"] for p in pairs])

    # ---- per condition ----
    per = {}
    for c in CONDITIONS:
        cp = [p for p in pairs if p["condition"] == c]
        lr = [math.log(p["ratio"]) for p in cp]
        de = [p["delta_ms"] for p in cp]
        sd = st.stdev(lr) if len(lr) > 1 else 0.0
        se_c = sd / math.sqrt(len(lr))
        per[c] = {
            "n": len(cp),
            "dqn_mean_ms": st.mean([p["dqn_ms"] for p in cp]),
            "aria2_mean_ms": st.mean([p["aria2_ms"] for p in cp]),
            "dqn_median_ms": st.median([p["dqn_ms"] for p in cp]),
            "aria2_median_ms": st.median([p["aria2_ms"] for p in cp]),
            "dqn_sd_ms": st.pstdev([p["dqn_ms"] for p in cp]),
            "aria2_sd_ms": st.pstdev([p["aria2_ms"] for p in cp]),
            "paired_mean_delta_ms": st.mean(de),
            "mean_log_ratio": st.mean(lr),
            "geometric_ratio": math.exp(st.mean(lr)),
            "reduction": 1 - math.exp(st.mean(lr)),
            "ci95_log_ratio": [
                st.mean(lr) - t_crit(len(lr)) * se_c,
                st.mean(lr) + t_crit(len(lr)) * se_c,
            ],
            "p_one_sided": paired_permutation(de),
        }
    ps = [per[c]["p_one_sided"] for c in CONDITIONS]
    adj = holm(ps)
    for c, a in zip(CONDITIONS, adj):
        per[c]["p_holm"] = a

    # ---- effect distribution ----
    dqn_faster = sum(1 for p in pairs if p["dqn_ms"] < p["aria2_ms"])
    aria_faster = sum(1 for p in pairs if p["aria2_ms"] < p["dqn_ms"])
    ties = n - dqn_faster - aria_faster
    cond_wins = sum(1 for c in CONDITIONS if per[c]["paired_mean_delta_ms"] < 0)

    criteria = {
        "1_ratio_below_1": geo < 1.0,
        "2_ci_excludes_zero_dqn_faster": ci[1] < 0,
        "3_perm_test_supports_dqn_faster": p_agg < 0.05,
        # majority = strictly more than half of the conditions (5/8 for U1-U8)
        "4_majority_conditions_dqn_faster": cond_wins >= len(CONDITIONS) // 2 + 1,
    }
    claim_supported = all(criteria.values())

    analysis = {
        "method": "matched pairs; effect=log(DQN/aria2); one-sided paired permutation test; Holm across 8 conditions",
        "aggregate": {
            "pairs": n,
            "dqn_mean_ms": dqn_mean,
            "aria2_mean_ms": aria2_mean,
            "geometric_ratio": geo,
            "relative_reduction": 1 - geo,
            "mean_log_ratio": mean_lr,
            "ci95_log_ratio": list(ci),
            "ratio_ci95": [math.exp(ci[0]), math.exp(ci[1])],
            "p_one_sided": p_agg,
        },
        "per_condition": per,
        "effect_distribution": {
            "dqn_faster_pairs": dqn_faster,
            "aria2_faster_pairs": aria_faster,
            "ties": ties,
            "dqn_faster_pct": 100 * dqn_faster / n,
            "conditions_dqn_faster": cond_wins,
        },
        "claim_criteria": criteria,
        "claim_supported": claim_supported,
    }
    with open(os.path.join(base, "statistical_analysis.json"), "w") as f:
        json.dump(analysis, f, indent=2, sort_keys=True)
        f.write("\n")
    with open(os.path.join(base, "claim_assessment.json"), "w") as f:
        json.dump(
            {
                "claim_supported": claim_supported,
                "criteria": criteria,
                "aggregate_reduction": 1 - geo,
                "geometric_ratio": geo,
                "p_one_sided": p_agg,
            },
            f,
            indent=2,
            sort_keys=True,
        )
        f.write("\n")

    # ---- tables + report ----
    with open(os.path.join(base, "summary.txt"), "w") as f:
        f.write("Final DQN vs aria2 evaluation - paired summary\n")
        f.write(
            f"pairs={n} aggregate DQN mean={dqn_mean:.1f}ms aria2 mean={aria2_mean:.1f}ms\n"
        )
        f.write(
            f"geometric ratio={geo:.4f} relative reduction={100*(1-geo):.2f}% "
            f"ratio 95%CI=[{math.exp(ci[0]):.4f},{math.exp(ci[1]):.4f}] p(one-sided)={p_agg:.4g}\n"
        )
        f.write(
        f"pairs DQN faster={dqn_faster}/{n} ({100*dqn_faster/n:.1f}%), aria2 faster={aria_faster}, ties={ties}; "
        f"conditions DQN faster={cond_wins}/{len(CONDITIONS)}\n"
        )
        f.write(f"claim supported={claim_supported}\n\n")
        f.write("per-condition:\n")
        for c in CONDITIONS:
            p = per[c]
            f.write(
                f"  {c}: DQN {p['dqn_mean_ms']:.1f}ms vs aria2 {p['aria2_mean_ms']:.1f}ms "
                f"red={100*p['reduction']:.1f}% ratio={p['geometric_ratio']:.3f} "
                f"CI=[{math.exp(p['ci95_log_ratio'][0]):.3f},{math.exp(p['ci95_log_ratio'][1]):.3f}] "
                f"p={p['p_one_sided']:.4g} p_holm={p['p_holm']:.4g}\n"
            )

    print(
        f"pairs={n} ratio={geo:.4f} reduction={100*(1-geo):.2f}% p={p_agg:.4g} "
        f"claim_supported={claim_supported} cond_wins={cond_wins}/8"
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
