#!/usr/bin/env python3
"""Workload-level robustness of the W1-W6 adaptive comparisons.

The reported pooled CI (analysis/w1_w6_adaptive.json, `pooled`) treats the 60
workload-blocks as independent. Two alternative summaries:
  * stratified bootstrap: resample blocks within each workload (the six
    workloads stay fixed) -- uncertainty from block noise only;
  * six-cluster t-interval: the six per-workload geometric means are the unit
    of replication -- the honest CI for generalisation across these workloads.
Writes analysis/w1_w6_robustness.json.
"""
import json
import math
import os
import random
import statistics as st

BASE = "/workspaces/mvp/testdata/n_dependent_server_joint_ns"
A = json.load(open(os.path.join(BASE, "analysis", "w1_w6_adaptive.json")))
T975_DF5 = 2.5706  # two-sided 95 % t quantile, df = 5 (six clusters)


def gmean(xs):
    return math.exp(st.mean(math.log(x) for x in xs))


out = {"method": "see module docstring", "comparisons": {}}
for base in ("per_workload_best_fixed", "family_best_fixed", "aria2"):
    R = {wid: A["workloads"][wid]["paired"][base]["ratios"] for wid in sorted(A["workloads"])}
    pooled = [x for v in R.values() for x in v]
    rng = random.Random(20261011)
    boot = []
    for _ in range(10000):
        pick = []
        for v in R.values():
            pick += [rng.choice(v) for _ in v]
        boot.append(gmean(pick))
    boot.sort()
    cl = [gmean(v) for v in R.values()]
    m = st.mean(math.log(c) for c in cl)
    sd = st.stdev(math.log(c) for c in cl)
    half = T975_DF5 * sd / math.sqrt(len(cl))
    out["comparisons"][base] = {
        "n_blocks": len(pooled),
        "pooled_geo": gmean(pooled),
        "stratified_bootstrap_ci95": [boot[249], boot[9749]],
        "per_workload_geo": {k: gmean(v) for k, v in R.items()},
        "six_cluster_t_ci95": [math.exp(m - half), math.exp(m + half)],
    }

json.dump(out, open(os.path.join(BASE, "analysis", "w1_w6_robustness.json"), "w"), indent=1)
for k, v in out["comparisons"].items():
    print(k, "pooled %.3f" % v["pooled_geo"],
          "strat-boot [%.3f, %.3f]" % tuple(v["stratified_bootstrap_ci95"]),
          "six-cluster [%.3f, %.3f]" % tuple(v["six_cluster_t_ci95"]))
