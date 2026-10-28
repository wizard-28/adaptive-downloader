#!/usr/bin/env python3
"""Recompute every quantitative claim in reports/paper_draft.md from raw artifacts.

Usage: python3 tools/claims_check.py [--print]
Exit status 0 iff every value in tools/paper_numbers.json matches the value
recomputed from raw run files within its stated tolerance. Standard library only.
Bootstrap intervals use a fixed seed; their tolerance is stated per number.
"""
import glob
import json
import math
import os
import random
import statistics as st
import sys

ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), ".."))
T = os.path.join(ROOT, "testdata")


def J(*p):
    return json.load(open(os.path.join(T, *p)))


def geo(xs):
    return math.exp(sum(math.log(x) for x in xs) / len(xs))


def boot(rs, seed=1, n=5000):
    rng = random.Random(seed)
    b = sorted(geo([rng.choice(rs) for _ in rs]) for _ in rs and range(n))
    return b[int(0.025 * n)], b[int(0.975 * n) - 1]


def t_ci_geo(xs, tcrit):
    l = [math.log(x) for x in xs]
    m, se = st.mean(l), st.stdev(l) / math.sqrt(len(l))
    return math.exp(m - tcrit * se), math.exp(m + tcrit * se)


out = {}

# ---- D2 frozen benchmark: 30 fresh pairs -----------------------------------
fr = os.path.join(T, "aria2_competitive_benchmark", "final_reproduction")
ours, ar = {}, {}
req = {"ours": [0, 0, 0], "aria2": [0, 0, 0]}  # total, ranged, early(<2s)
verified = True
for sysn, store in (("ours", ours), ("aria2", ar)):
    for f in sorted(glob.glob(os.path.join(fr, f"final-p*-{sysn}.json"))):
        d = json.load(open(f))
        i = int(os.path.basename(f).split("-p")[1][:2])
        store[i] = d["completion_ms"] / 1000
        verified &= d["content_verified"] is True and not d["err"]
        for r in d["server_requests"]:
            req[sysn][0] += 1
            req[sysn][1] += bool(r["has_range"])
            req[sysn][2] += r["start_ms"] < 2000
ps = sorted(set(ours) & set(ar))
rs = [ours[i] / ar[i] for i in ps]
out["d2.pairs"] = len(ps)
out["d2.ours_mean_s"] = st.mean(ours[i] for i in ps)
out["d2.aria2_mean_s"] = st.mean(ar[i] for i in ps)
out["d2.geo_ratio"] = geo(rs)
out["d2.ci_lo"], out["d2.ci_hi"] = t_ci_geo(rs, 2.0452)  # t-interval, df=29 (as in the frozen analysis)
out["d2.wins"] = sum(r < 1 for r in rs)
out["d2.all_content_verified"] = verified
out["d2.improvement_pct"] = (1 - geo(rs)) * 100
out["d2.ours_requests_per_run"] = req["ours"][0] / len(ps)
out["d2.aria2_requests_per_run"] = req["aria2"][0] / len(ps)
out["d2.ours_early_per_run"] = req["ours"][2] / len(ps)

# ---- Track 2-B live adaptive (P1/P2) -------------------------------------
R = {}
for f in glob.glob(os.path.join(T, "n_dependent_server_joint_ns", "adaptive", "*.json")):
    d = json.load(open(f))
    name, r = os.path.basename(f)[:-5].rsplit("_r", 1)
    R.setdefault(name, {})[int(r)] = d


def pair(a, b):
    rr = [R[a][r]["completion_ms"] / R[b][r]["completion_ms"] for r in sorted(set(R[a]) & set(R[b]))]
    return geo(rr), sum(x < 1 for x in rr), len(rr), rr


for key, a, b in [
    ("p1.healthy_vs_fixed", "p1_healthy_selector", "p1_healthy_fixed"),
    ("p1.capped_vs_fixed", "p1_capped_selector", "p1_capped_fixed"),
    ("p1.healthy_vs_aria2", "p1_healthy_selector", "p1_healthy_aria2"),
    ("p1.capped_vs_aria2", "p1_capped_selector", "p1_capped_aria2"),
    ("p2.joint_vs_fixed", "p2_causal_joint", "p2_fixed_16_1M"),
    ("p2.joint_vs_nonly", "p2_causal_joint", "p2_causal_n_only"),
    ("p2.joint_vs_sonly", "p2_causal_joint", "p2_causal_s_only"),
    ("p2.joint_vs_aria2", "p2_causal_joint", "p2_aria2"),
    ("p2.nonly_vs_fixed", "p2_causal_n_only", "p2_fixed_16_1M"),
    ("p2.sonly_vs_fixed", "p2_causal_s_only", "p2_fixed_16_1M"),
]:
    g, w, n, rr = pair(a, b)
    out[key + ".geo"], out[key + ".wins"] = g, w
    if key == "p2.joint_vs_fixed":
        out[key + ".ci_lo"], out[key + ".ci_hi"] = boot(rr)
for name in ("p2_causal_joint", "p2_fixed_16_1M", "p2_aria2", "p2_causal_n_only", "p2_causal_s_only"):
    out[f"mean_s.{name}"] = st.mean(R[name][r]["completion_ms"] for r in R[name]) / 1000
out["p2.n_blocks"] = len(R["p2_causal_joint"])

# ---- live gate -----------------------------------------------------------
g = J("n_dependent_server_joint_ns", "analysis", "gate_b.json")
out["gate.pass"] = g["gate"]["pass"]
out["gate.s_margin"] = g["optima"]["capped"]["S_margin"]
out["gate.n_margin"] = g["optima"]["capped"]["N_margin"]
out["gate.penalty_capped_uses_healthy_opt"] = g["gate"]["penalty_capped_uses_healthy_opt"]
out["gate.penalty_healthy_uses_capped_opt"] = g["gate"]["penalty_healthy_uses_capped_opt"]
out["gate.checks_passed"] = sum(bool(v) for v in g["gate"]["checks"].values())
out["gate.invalid_cells"] = len(g["invalid"])
pr = J("n_dependent_server_joint_ns", "analysis", "precision_s_row.json")
out["gate.s_margin_precision"] = pr["local_S_margin"]["margin"]
out["gate.s_margin_precision_lo"] = pr["local_S_margin"]["ci95_low"]
out["gate.s_margin_precision_hi"] = pr["local_S_margin"]["ci95_high"]

# ---- W1-W6 adaptive ------------------------------------------------------
W = {}
for f in glob.glob(os.path.join(T, "n_dependent_server_joint_ns", "w1_w6_adaptive", "*.json")):
    d = json.load(open(f))
    w, rest = d["run_id"].split("_", 1)
    s, r = rest.rsplit("_r", 1)
    W.setdefault((w, s), {})[int(r)] = d
ws = sorted({k[0] for k in W})
out["w16.runs"] = sum(len(v) for v in W.values())
out["w16.all_content_verified"] = all(d["content_verified"] and not d["err"] for v in W.values() for d in v.values())
for base, tag in (("per_workload_best_fixed", "pw"), ("family_best_fixed", "fam"), ("aria2", "aria2")):
    allr, per = [], {}
    for w in ws:
        rr = [W[(w, "band_selector")][r]["completion_ms"] / W[(w, base)][r]["completion_ms"] for r in range(10)]
        per[w] = geo(rr)
        allr += rr
        out[f"w16.{tag}.{w}"] = per[w]
    out[f"w16.{tag}.pooled"] = geo(allr)
    out[f"w16.{tag}.wins"] = sum(x < 1 for x in allr)
    out[f"w16.{tag}.boot_lo"], out[f"w16.{tag}.boot_hi"] = boot(allr, seed=3)
    out[f"w16.{tag}.cluster_lo"], out[f"w16.{tag}.cluster_hi"] = t_ci_geo(list(per.values()), 2.5706)
    if tag == "fam":
        out["w16.fam.w2_w6_geo"] = geo([per[w] for w in ws if w != "W1"])

# ---- recorded-trace oracle gate -----------------------------------------
orr = J("final_oracle_analysis", "final_oracle_results.json")["traces"]
H = []
for k, t in orr.items():
    h = 1 - t["t_constrained_seconds"] / t["t_best_fixed_seconds"]
    assert abs(h - t["H"]) < 1e-9, k
    H.append(h)
tc = 2.1318467863266495
out["trace.n"] = len(H)
out["trace.mean_H_pct"] = st.mean(H) * 100
out["trace.max_H_pct"] = max(H) * 100
out["trace.lcb95_pct"] = (st.mean(H) - tc * st.stdev(H) / math.sqrt(len(H))) * 100

# ---- learned-policy head-to-heads ---------------------------------------
def h2h(path, dqn_names):
    d = J(*path)["results"]
    by = {}
    for r in d:
        ok = r.get("valid", r.get("ok") and r.get("output_matches"))
        by.setdefault(r["pair_id"], {})[("aria2" if r["system"] == "aria2" else "dqn")] = (r["duration_ms"], ok)
    rr = [v["dqn"][0] / v["aria2"][0] for v in by.values() if "dqn" in v and "aria2" in v and v["dqn"][1] and v["aria2"][1]]
    return geo(rr), t_ci_geo(rr, 1.97 if len(rr) > 200 else 1.976), len(rr), sum(x < 1 for x in rr)

for tag, path in (("v2", ("final_evaluation", "raw_results.json")),
                  ("v5", ("final_evaluation_v5", "raw_results_all.json")),
                  ("v6", ("final_evaluation_v6", "raw_results_all.json"))):
    g_, ci, n, w = h2h(path, None)
    out[f"h2h.{tag}.geo"], out[f"h2h.{tag}.ci_lo"], out[f"h2h.{tag}.ci_hi"] = g_, ci[0], ci[1]
    out[f"h2h.{tag}.pairs"], out[f"h2h.{tag}.dqn_faster"] = n, w

# ---- optional: pacing-schedule sweep ------------------------------------
sw = os.path.join(T, "d2_pacing_sweep", "analysis", "sweep_results.json")
if os.path.exists(sw):
    s = json.load(open(sw))
    for name, e in s["schedules"].items():
        if e["n_valid_pairs"]:
            out[f"sweep.{name}.geo"] = e["geo_mean_ratio"]
            out[f"sweep.{name}.n"] = e["n_valid_pairs"]

ng = os.path.join(T, "nginx_limit_rate", "analysis", "nginx_results.json")
if os.path.exists(ng):
    for name, e in json.load(open(ng))["configs"].items():
        if e["n_valid_pairs"]:
            out[f"nginx.{name}.geo"] = e["geo_mean_ratio"]
            out[f"nginx.{name}.n"] = e["n_valid_pairs"]
            out[f"nginx.{name}.ours_faster"] = e["pairs_ours_faster"]



# ======================= additional recomputations =========================
TCRIT = {4: 2.7764, 9: 2.2622, 14: 2.1448, 29: 2.0452, 149: 1.9760, 239: 1.9699}  # two-sided 95 %


def tci(rr):
    """95 % t-interval for the geometric mean of ratios rr (correct df)."""
    l = [math.log(x) for x in rr]
    t = TCRIT[len(l) - 1]
    m, se = st.mean(l), st.stdev(l) / math.sqrt(len(l))
    return math.exp(m - t * se), math.exp(m + t * se)


def raw_pairs(path):
    """Matched aria2/dqn pairs from a final-evaluation raw_results file."""
    d = J(*path)["results"]
    by = {}
    for r in d:
        ok = r.get("valid", r.get("ok") and r.get("output_matches"))
        by.setdefault(r["pair_id"], {})[("aria2" if r["system"] == "aria2" else "dqn")] = (r["duration_ms"], ok, r.get("condition"))
    return [(v["dqn"][0] / v["aria2"][0], v["dqn"][0], v["aria2"][0], v["dqn"][2])
            for v in by.values() if "dqn" in v and "aria2" in v and v["dqn"][1] and v["aria2"][1]]


for tag, path in (("v2", ("final_evaluation", "raw_results.json")),
                  ("v5", ("final_evaluation_v5", "raw_results_all.json")),
                  ("v6", ("final_evaluation_v6", "raw_results_all.json"))):
    pr = raw_pairs(path)
    lo, hi = tci([x[0] for x in pr])
    out[f"h2h.{tag}.ci_lo"], out[f"h2h.{tag}.ci_hi"] = lo, hi   # correct-df interval (overrides the earlier approximation)
    if tag == "v6":
        for cond in sorted({x[3] for x in pr}):
            rr = [x[0] for x in pr if x[3] == cond]
            out[f"h2h.v6.{cond}.geo"] = geo(rr)
            out[f"h2h.v6.{cond}.ci_lo"], out[f"h2h.v6.{cond}.ci_hi"] = tci(rr)
            out[f"h2h.v6.{cond}.n"] = len(rr)

# long-duration diagnostic (raw)
for sz in ("16MiB", "128MiB", "512MiB"):
    pr = raw_pairs(("diagnostics", "dqn_long_duration_v6", f"size_{sz}", "raw_results_all.json"))
    out[f"long.{sz}.pooled"] = geo([x[0] for x in pr])
    out[f"long.{sz}.n"] = len(pr)
    for cond in sorted({x[3] for x in pr}):
        out[f"long.{sz}.{cond}"] = geo([x[0] for x in pr if x[3] == cond])
    if sz == "512MiB":
        out["long.512MiB.ci_lo"], out["long.512MiB.ci_hi"] = tci([x[0] for x in pr])

# P1 absolute times and the best single fixed config over the two workloads
for name in ("p1_healthy_selector", "p1_healthy_fixed", "p1_healthy_aria2", "p1_capped_selector", "p1_capped_fixed", "p1_capped_aria2"):
    out[f"mean_s.{name}"] = st.mean(R[name][r]["completion_ms"] for r in R[name]) / 1000
cells = g["cells"]
out["p1.single_fixed_8_4M.healthy_s"] = cells["healthy"]["8/4194304"] / 1000
out["p1.single_fixed_8_4M.capped_s"] = cells["capped"]["8/4194304"] / 1000
out["p1.gain_vs_single_healthy_pct"] = (1 - out["mean_s.p1_healthy_selector"] / out["p1.single_fixed_8_4M.healthy_s"]) * 100
out["p1.gain_vs_single_capped_pct"] = (1 - out["mean_s.p1_capped_selector"] / out["p1.single_fixed_8_4M.capped_s"]) * 100
out["p1.healthy_fixed_best_s"] = min(v for v in cells["healthy"].values()) / 1000
out["p1.capped_fixed_best_s"] = min(v for v in cells["capped"].values()) / 1000

# analytic cross penalties (prescreen, ANALYTIC MODEL not live)
ps_ = J("n_dependent_server_joint_ns", "analysis", "prescreen_b.json")
for pr_ in ps_["pairs"]:
    if pr_["schedule"] == "T=1" and pr_["cap"] == "C=8M_g=0.05":
        out["analytic.pen_cap_pct"] = pr_["pen_cap"] * 100
        out["analytic.pen_base_pct"] = pr_["pen_base"] * 100

# D2 controls (granularity-matched control A, constant-rate control)
mech = os.path.join(T, "aria2_competitive_benchmark", "mechanism")
ca = []
for i in range(20):
    fo, fa = os.path.join(mech, f"controlA-p{i:02d}-ours.json"), os.path.join(mech, f"controlA-p{i:02d}-aria2.json")
    if os.path.exists(fo) and os.path.exists(fa):
        ca.append(json.load(open(fo))["completion_ms"] / json.load(open(fa))["completion_ms"])
if ca:
    out["controlA.n"] = len(ca)
    out["controlA.geo"] = geo(ca)
    out["controlA.ci_lo"], out["controlA.ci_hi"] = tci(ca) if len(ca) - 1 in TCRIT else (float("nan"),) * 2
for nm in ("const-aria2", "const-ours-4M", "const-ours-32M"):
    out[f"const.{nm}_s"] = json.load(open(os.path.join(mech, nm + ".json")))["completion_ms"] / 1000
out["const.gap_ratio_32M_over_4M"] = out["const.const-ours-32M_s"] / out["const.const-ours-4M_s"]
_co = [json.load(open(os.path.join(mech, f"controlA-p{i:02d}-ours.json")))["completion_ms"] / 1000 for i in range(20) if os.path.exists(os.path.join(mech, f"controlA-p{i:02d}-ours.json"))]
if _co:
    out["controlA.ours_32M_s"] = st.mean(_co)
    out["controlA.ours_32M_over_ours_4M_D2"] = st.mean(_co) / out["d2.ours_mean_s"]

# W5 live grid optimum
gr = J("n_dependent_server_joint_ns", "analysis", "w1_w6_grid.json")
def _find(o, key):
    if isinstance(o, dict):
        for k, v in o.items():
            if k == key:
                return v
            r_ = _find(v, key)
            if r_ is not None:
                return r_
    elif isinstance(o, list):
        for v in o:
            r_ = _find(v, key)
            if r_ is not None:
                return r_
    return None

# sweep and NGINX per-cell means and wins (raw)
sm = J("d2_pacing_sweep", "manifest", "sweep_manifest.json")
tw = tn = 0
for sname in sm["schedules_kib_per_s"]:
    po, pa = [], []
    for k in range(sm["pairs_per_schedule"]):
        po.append(json.load(open(os.path.join(T, "d2_pacing_sweep", "runs", f"sw-{sname[:2]}-p{k:02d}-ours.json")))["completion_ms"] / 1000)
        pa.append(json.load(open(os.path.join(T, "d2_pacing_sweep", "runs", f"sw-{sname[:2]}-p{k:02d}-aria2.json")))["completion_ms"] / 1000)
    rr = [a / b for a, b in zip(po, pa)]
    out[f"sweep.{sname}.ours_s"], out[f"sweep.{sname}.aria2_s"] = st.mean(po), st.mean(pa)
    out[f"sweep.{sname}.min"], out[f"sweep.{sname}.max"] = min(rr), max(rr)
    out[f"sweep.{sname}.wins"] = sum(x < 1 for x in rr)
    tw += sum(x < 1 for x in rr); tn += len(rr)
out["sweep.total_wins"], out["sweep.total_pairs"] = tw, tn
nm_ = J("nginx_limit_rate", "manifest", "nginx_manifest.json")
nw = nn = 0
for cname in nm_["configs"]:
    po = [json.load(open(os.path.join(T, "nginx_limit_rate", "runs", f"ng-{cname[:2]}-p{k:02d}-ours.json")))["completion_ms"] / 1000 for k in range(5)]
    pa = [json.load(open(os.path.join(T, "nginx_limit_rate", "runs", f"ng-{cname[:2]}-p{k:02d}-aria2.json")))["completion_ms"] / 1000 for k in range(5)]
    out[f"nginx.{cname}.ours_s"], out[f"nginx.{cname}.aria2_s"] = st.mean(po), st.mean(pa)
    rr = [a / b for a, b in zip(po, pa)]
    out[f"nginx.{cname}.min"], out[f"nginx.{cname}.max"] = min(rr), max(rr)
    nw += sum(x < 1 for x in rr); nn += len(rr)
out["nginx.total_wins"], out["nginx.total_pairs"] = nw, nn


# remaining quantities quoted in the manuscript
for key, a, b in (("p1.healthy_vs_fixed", "p1_healthy_selector", "p1_healthy_fixed"),
                  ("p2.joint_vs_nonly", "p2_causal_joint", "p2_causal_n_only")):
    _, _, _, rr = pair(a, b)
    out[key + ".ci_lo"], out[key + ".ci_hi"] = boot(rr)
w5 = gr["workloads"]["W5"]
out["w5.live_argmin_s"] = w5["live_argmin_s"]
out["w5.live_model_target_s"] = w5["cells"]["4/4194304"]["median_s"]
_man = J("n_dependent_server_joint_ns", "manifest", "w1_w6_manifest.json")
out["w5.model_prediction_s"] = _man["workloads"]["W5"]["optimum_B"]["t"]
out["w16.blocks"] = len(ws) * 10
out["sweep.valid_runs"] = sum(1 for o_ in sm["order"] if json.load(open(os.path.join(T, "d2_pacing_sweep", "runs", o_["run_id"] + ".json"))).get("content_verified") is True)
out["nginx.valid_runs"] = sum(1 for o_ in nm_["order"] if json.load(open(os.path.join(T, "nginx_limit_rate", "runs", o_["run_id"] + ".json"))).get("content_verified") is True)
for k_, t_ in orr.items():
    out[f"trace.H_pct.{k_}"] = t_["H"] * 100

# live-internet pilot L-P1 (§8a): point values recomputed here from raw JSONL;
# bootstrap bounds and timeline CVs via the frozen tools/analyze_live_pilot.py.
sys.path.insert(0, os.path.join(ROOT, "tools"))
import analyze_live_pilot as _lp
for _tag, _dir in (("lp1", "raw"), ("lp1x", "raw_other_network")):
    _recs = []
    for _p in sorted(glob.glob(os.path.join(T, "live_pilot_l1", _dir, "*.jsonl"))):
        _recs += [json.loads(l) for l in open(_p) if l.strip()]
    out[f"{_tag}.runs"] = len(_recs)
    out[f"{_tag}.valid"] = sum(1 for r in _recs if r["valid"])
    out[f"{_tag}.invalid"] = out[f"{_tag}.runs"] - out[f"{_tag}.valid"]
    _cells = sorted({r["arm"] for r in _recs if r["kind"] == "ours"})
    _cond = {}
    for r in _recs:
        if r["valid"]:
            _cond.setdefault((r["server"], r["slot"]), {}).setdefault(r["arm"], []).append(r["duration_s"])
    _rows = {k: {c: geo(v[c]) for c in _cells} for k, v in _cond.items() if all(c in v for c in _cells)}
    _ctx = {k: _cond[k] for k in _rows}
    _tab = [_rows[k] for k in sorted(_rows)]
    _g = min(_cells, key=lambda c: geo([row[c] / min(row.values()) for row in _tab]))
    _rat = [row[_g] / min(row.values()) for row in _tab]
    out[f"{_tag}.n_cond"] = len(_tab)
    out[f"{_tag}.servers"] = len({k[0] for k in _rows})
    out[f"{_tag}.headroom_pct"] = (geo(_rat) - 1) * 100
    out[f"{_tag}.global_max_pct"] = (max(_rat) - 1) * 100
    out[f"{_tag}.near"] = sum(1 for x in _rat if x <= 1.05)
    out[f"{_tag}.global_is_N8_S1M"] = _g == "ours_N8_S1M"
    _h = _lp.headroom(_tab, _cells)
    assert abs(_h["headroom_pct"] - out[f"{_tag}.headroom_pct"]) < 1e-9 and _h["global_cell"] == _g
    out[f"{_tag}.boot_lo_pct"], out[f"{_tag}.boot_hi_pct"] = _h["boot95_pct"]
    _a = [geo(_ctx[k]["aria2"]) / min(_rows[k].values()) for k in _rows]
    out[f"{_tag}.aria2_min"], out[f"{_tag}.aria2_max"] = min(_a), max(_a)
    _cv = [v["median_cv"] for v in _lp.variability(_recs).values()]
    out[f"{_tag}.cv_min"], out[f"{_tag}.cv_max"] = min(_cv), max(_cv)
    _top = {}
    for k in sorted(_rows):
        _top.setdefault(k[0], set()).add(min(_rows[k], key=_rows[k].get))
    out[f"{_tag}.servers_top_changed"] = sum(1 for v in _top.values() if len(v) > 1)
    _ok = [r for r in _recs if r["valid"]]
    out[f"{_tag}.max_MBps"] = max(r["bytes"] / r["duration_s"] for r in _ok) / 1e6
    if _tag == "lp1":
        out["lp1.tuna_403_invalid"] = sum(1 for r in _recs if not r["valid"] and r["server"] == "tuna" and "403" in (r.get("status_codes") or {}))
        out["lp1.s5_invalid"] = sum(1 for r in _recs if not r["valid"] and r["slot"] == "S5")
        _k4 = [min(_rows[k]["ours_N4_S1M"], _rows[k]["ours_N4_S4M"]) / min(_rows[k].values()) for k in _rows if k[0] == "kedge"]
        out["lp1.kedge_N4_min"], out["lp1.kedge_N4_max"] = min(_k4), max(_k4)


# Engine ablation, online tuner and capped-server learned agents (§7a-§7b,
# §9a): recomputed from the raw runs by each study's frozen analysis script.
import importlib.util as _ilu
import subprocess as _sp
import tempfile as _tf


def _study(name, script):
    spec = _ilu.spec_from_file_location(f"_an_{name}", os.path.join(T, name, "tools", script))
    mod = _ilu.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod.analyze(os.path.join(T, name, "runs"))


def _flatten(o, prefix):
    if isinstance(o, dict):
        if "geo_ratio" in o:
            out[prefix + ".geo"] = o["geo_ratio"]
            out[prefix + ".lo"], out[prefix + ".hi"] = o["ci95"]
            out[prefix + ".n"] = o["n_blocks"]
            return
        for k, v in o.items():
            if k in ("runs", "invalid", "label"):
                continue
            _flatten(v, f"{prefix}.{k}")
    elif isinstance(o, list):
        for i, v in enumerate(o):
            _flatten(v, f"{prefix}.{v.get('den', i) if isinstance(v, dict) else i}")
    elif isinstance(o, (int, float)) and not isinstance(o, bool):
        out[prefix] = o


for _tag, _name, _script in (("abl", "split_ablation", "analyze_split_ablation.py"),
                             ("tun", "online_tuner", "analyze_online_tuner.py"),
                             ("rlc", "rl_capped", "analyze_rl_capped.py")):
    _res = _study(_name, _script)
    out[f"{_tag}.invalid"] = len(_res["invalid"])
    out[f"{_tag}.runs"] = len(glob.glob(os.path.join(T, _name, "runs", "*.json")))
    _flatten(_res["scenarios"], _tag)
# DQN retrained for the capped server (§9b): live numbers from the raw runs via
# the study's frozen analysis functions; simulator predictions from its
# training report (simulator output, not live data).
_spec = _ilu.spec_from_file_location("_an_rl_cap_train", os.path.join(T, "rl_cap_train", "tools", "analyze.py"))
_rct = _ilu.module_from_spec(_spec)
_spec.loader.exec_module(_rct)
_tab, _inv, _ = _rct.load("A2")
out["rct.A2.invalid"] = len(_inv)
for _r in _rct.stage_a2(_tab)["rows"]:
    out[f"rct.A2.{_r['scenario']}.{_r['arm']}.rel_err_pct"] = _r["rel_err"] * 100
for _st in ("C", "D"):
    _tab, _inv, _traj = _rct.load(_st)
    out[f"rct.{_st}.invalid"] = len(_inv)
    out[f"rct.{_st}.runs"] = len(glob.glob(os.path.join(T, "rl_cap_train", f"runs_{_st}", "*.json")))
    _flatten(_rct.stage_cd(_tab, _traj), f"rct.{_st}")
_rep = J("rl_cap_train", "train", "report.json")
for _sc, _row in _rep["test"].items():
    for _k in ("dqnc_start16_vs_best_fixed", "dqnc_start1_vs_best_fixed"):
        out[f"rct.sim.{_sc}.{_k}"] = _row[_k]


class _Capture:
    """Stands in for a study script's json module: keeps what main() would
    write to its analysis file, so the checker recomputes without writing
    (with the module-level open() redirect in _run_main)."""

    def __init__(self):
        self.data = None

    def dump(self, obj, f, **kw):
        self.data = obj

    def __getattr__(self, name):
        return getattr(json, name)


def _run_main(name, argv):
    spec = _ilu.spec_from_file_location(f"_main_{name}_{'_'.join(argv)}", os.path.join(T, name, "tools", "analyze.py"))
    mod = _ilu.module_from_spec(spec)
    spec.loader.exec_module(mod)
    cap = _Capture()
    mod.json = cap
    # The script opens its analysis file for writing before json.dump; give it
    # a throwaway buffer so the committed file is never truncated.
    import builtins, io as _io
    mod.open = lambda f, mode="r", *a, **k: _io.StringIO() if "w" in mode else builtins.open(f, mode, *a, **k)
    saved, sys.argv = sys.argv, ["analyze.py"] + argv
    try:
        import contextlib, io
        with contextlib.redirect_stdout(io.StringIO()):
            mod.main()
    finally:
        sys.argv = saved
    return cap.data


# Other segmented downloaders (§4.3), delay x loss sweep (§4.4), joint agent (§9c).
_bx = _run_main("baselines_ext", [])
out["bx.invalid"] = len(_bx["invalid"])
out["bx.runs"] = len(glob.glob(os.path.join(T, "baselines_ext", "runs_E", "*.json")))
_flatten(_bx["scenarios"], "bx")
_sw = _run_main("netem_sweep", [])
out["sw.invalid"] = len(_sw["invalid"])
out["sw.runs"] = len(glob.glob(os.path.join(T, "netem_sweep", "runs_S", "*.json")))
for _k, _c in _sw["cells"].items():
    for _f in ("geo_ratio", "min", "max", "ours_s", "aria2_s"):
        out[f"sw.{_k}.{_f}"] = _c[_f]
for _wl, _sides in _sw["by_side"].items():
    for _side, _cells in _sides.items():
        out[f"sw.{_wl}.{_side}.count"] = len(_cells)
for _st in ("C", "L"):
    _j = _run_main("rl_joint", [_st])
    out[f"jnt.{_st}.invalid"] = len(_j["invalid"])
    out[f"jnt.{_st}.runs"] = len(glob.glob(os.path.join(T, "rl_joint", f"runs_{_st}", "*.json")))
    _flatten(_j["scenarios"], f"jnt.{_st}")
# DQN v2 (§9d): live C and D from the raw runs; Stage A parity from the raw
# runs against the frozen predictions; simulator numbers from its outputs.
for _st in ("C", "D"):
    _v = _run_main("rl_cap_train_v2", [_st])
    out[f"r2.{_st}.invalid"] = len(_v["invalid"])
    out[f"r2.{_st}.runs"] = len(glob.glob(os.path.join(T, "rl_cap_train_v2", f"runs_{_st}", "*.json")))
    for _sc, _o in _v["scenarios"].items():
        for _a in ("v2", "v1", "drop"):
            for _k in ("vs_best_fixed", "vs_v1", "vs_drop"):
                _r = (_o.get(_a) or {}).get(_k)
                if _r:
                    _p = f"r2.{_st}.{_sc}.{_a}.{_k}"
                    out[_p + ".geo"] = _r["geo_ratio"]
                    out[_p + ".lo"], out[_p + ".hi"] = _r["ci95"]
                    out[_p + ".tlo"], out[_p + ".thi"] = _r["t_ci95"]
        for _a, _g in _o["geo_mean_s"].items():
            out[f"r2.{_st}.{_sc}.geo_mean_s.{_a}"] = _g
_pa = json.load(open(os.path.join(T, "rl_cap_train_v2", "predictions_A.json")))["predictions"]
_live = {}
for _f in glob.glob(os.path.join(T, "rl_cap_train_v2", "runs_A", "*.json")):
    _r = json.load(open(_f))
    if _r.get("content_verified") is True and not _r.get("err"):
        _live.setdefault("_".join(_r["run_id"].split("_")[:3]), []).append(_r["completion_ms"] / 1000)
for _sc, _key in (("A_CAP0_80", "A_CAP0_8x1M_80"), ("A_HEALTHY_80", "A_HEALTHY_8x1M_80"),
                  ("A_CAP8_120", "A_CAP8_12x1M_120"), ("A_D2custom_120", "A_D2custom_16x1M_120")):
    out[f"r2.A.{_sc}.rel_err_pct"] = (geo(_live[_sc]) / _pa[_key]["gm_s"] - 1) * 100
_pcd = json.load(open(os.path.join(T, "rl_cap_train_v2", "predictions_CD.json")))["predictions_s"]
for _sc, _row in _pcd.items():
    for _a, _v in _row.items():
        out[f"r2.pred.{_sc}.{_a}"] = _v
_pH = json.load(open(os.path.join(T, "rl_cap_train_v2", "predictions_H.json")))["scenarios"]
for _sc, _row in _pH.items():
    if "headroom" in _row:
        out[f"r2.headroom.{_sc}.pct"] = _row["headroom"] * 100
# Adaptive arms on public mirrors (§8a): slot M2 from the raw JSONL.
for _slot in ("M1", "M2"):
    _m = _run_main("live_mirror_check", [_slot])
    out[f"mc.{_slot}.invalid"] = len(_m["invalid"])
    out[f"mc.{_slot}.runs"] = sum(1 for _ in open(os.path.join(T, "live_mirror_check", "raw", _slot + ".jsonl")))
    _flatten(_m["servers"], f"mc.{_slot}")
# Track 1 joint heuristic vs frozen aria2 (§10a) and the trace capacity swing (§8).
out["t1.joint_vs_aria2.geo"] = json.load(open(os.path.join(
    T, "joint_ns_d2", "analysis", "track1_analysis.json")))["comparisons"]["joint_vs_frozen_aria2"]["geo_ratio"]
_tsw = json.load(open(os.path.join(T, "final_oracle_analysis", "trace_swing.json")))
for _t, _r in _tsw["traces"].items():
    out[f"trace.{_t}.swing"] = _r["p90_over_p10"]
_jrep = json.load(open(os.path.join(T, "rl_joint", "train", "report.json")))
for _sc, _row in _jrep["test"].items():
    if "agent_vs_best_fixed" in _row:
        out[f"jnt.sim.{_sc}.agent_vs_best_fixed"] = _row["agent_vs_best_fixed"]
with _tf.NamedTemporaryFile(suffix=".json") as _f:
    _sp.run([sys.executable, os.path.join(ROOT, "tools", "adaptation_speed.py"), _f.name],
            cwd=ROOT, check=True, capture_output=True)
    for _k, _v in json.load(open(_f.name)).items():
        if _v["median"] is not None:
            out["speed." + _k.replace("/", ".") + ".median"] = _v["median"]


def scan_paper(text, out, consts):
    """Every decimal number, percentage and a/b fraction in the manuscript must
    be reproduced by a recomputed value (within one unit of its last digit) or be
    listed with a reason in tools/paper_constants.json. Returns unmatched tokens."""
    import re
    vals = [float(v) for v in out.values() if isinstance(v, (int, float)) and not isinstance(v, bool) and v == v]
    derived = set(vals)
    for v in vals:
        derived.add((1 - v) * 100)
        derived.add((v - 1) * 100)
        derived.add(v * 100)
        if v:
            derived.add(1 / v)
    ints = {int(round(v)) for v in vals if abs(v - round(v)) < 1e-9}
    allowed = set(consts.get("decimals", [])) | set(consts.get("fractions", []))
    body = re.sub(r"`[^`]*`", " ", text)           # inline code (paths, flags)
    body = re.sub(r"\]\([^)]*\)", "]", body)         # link targets
    missing = []
    for m in re.finditer(r"(?<![\w.§/-])(\d+/\d+)(?![\w.]|/\d)", body):
        tok = m.group(1)
        a, b = (int(x) for x in tok.split("/"))
        if tok in allowed or (a in ints and b in ints):
            continue
        missing.append(tok)
    for m in re.finditer(r"(?<![\w.§/-])(\d+) of (\d+)(?![\w.])", body):
        tok = f"{m.group(1)}/{m.group(2)}"
        a_, b_ = int(m.group(1)), int(m.group(2))
        if tok in allowed or (a_ in ints and b_ in ints):
            continue
        missing.append(tok + " (written as 'N of M')")
    for m in re.finditer(r"(?<![\w.§])([+\-−]?\d+\.\d+)(?![\w]|\.\d)", body):
        tok = m.group(1).replace("−", "-").lstrip("+")
        if tok in allowed:
            continue
        d = len(tok.split(".")[1])
        t = float(tok)
        tol = 10 ** (-d) * 0.5001          # exact rounding of a recomputed value
        if any(abs(abs(v) - abs(t)) <= tol for v in derived):
            continue
        missing.append(tok)
    return sorted(set(missing))

# ---- compare -------------------------------------------------------------
if "--print" in sys.argv:
    for k in sorted(out):
        print(f"{k:45s} {out[k]}")
    sys.exit(0)
paper_path = os.path.join(ROOT, "reports", "paper_draft.md")
if "--paper" in sys.argv:
    paper_path = sys.argv[sys.argv.index("--paper") + 1]
numbers_path = os.path.join(ROOT, "tools", "paper_numbers.json")
if "--numbers" in sys.argv:
    numbers_path = sys.argv[sys.argv.index("--numbers") + 1]
exp = json.load(open(numbers_path))["numbers"]
bad = 0
for k, spec in exp.items():
    if k not in out:
        print(f"MISSING  {k}")
        bad += 1
        continue
    v, want, tol = out[k], spec["value"], spec.get("tol", 0)
    ok = (v == want) if isinstance(want, (bool, str)) else abs(v - want) <= tol
    if not ok:
        print(f"MISMATCH {k}: recomputed {v!r} vs paper {want!r} (tol {tol})  [{spec.get('where','')}]")
        bad += 1
# Optional text anchors bind a named number to the exact wording in the paper
# (whitespace-normalised), so an edit that happens to match some other
# recomputed value is still caught.
_norm = lambda t: " ".join(t.split())
_ptext = _norm(open(paper_path).read())
for k, spec in exp.items():
    if "text" in spec and _norm(spec["text"]) not in _ptext:
        print(f"ANCHOR MISSING {k}: {spec['text']!r} not found in paper  [{spec.get('where','')}]")
        bad += 1
print(f"checked {len(exp)} numbers: {len(exp) - bad} ok, {bad} failed")
consts_path = os.path.join(ROOT, "tools", "paper_constants.json")
consts = json.load(open(consts_path)) if os.path.exists(consts_path) else {}
unmatched = scan_paper(open(paper_path).read(), out, consts)
for tok in unmatched:
    print(f"UNVERIFIED NUMBER IN PAPER: {tok} (no recomputed value and not in tools/paper_constants.json)")
print(f"paper scan: {len(unmatched)} unverified tokens")
sys.exit(1 if (bad or unmatched) else 0)
