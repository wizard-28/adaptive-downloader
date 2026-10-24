#!/usr/bin/env python3
"""Write reports/review3/rl_results.tex from testdata/rl_capped/analysis/summary.json.

Usage: python3 reports/review3/make_rl_tex.py   (from the repository root)
The conclusion wording follows testdata/rl_capped/protocol.md.
"""
import json

S = json.load(open("testdata/rl_capped/analysis/summary.json"))
NAMES = {"CAP8": "Cap from 8~s", "HEALTHY": "No cap"}
ARMS = {"dqn6_start16": "DQN v6, start $N=16$", "dqn6_start1": "DQN v6, start $N=1$"}
FIX = {"fixed_16_1M_NT_split": "16~$\\times$~1~MiB", "fixed_16_32M_NT_split": "16~$\\times$~32~MiB"}


def fmt(r):
    return f"{r['geo_ratio']:.3f} [{r['ci95'][0]:.3f}, {r['ci95'][1]:.3f}]"


rows, cap8_faster = [], []
for sc in ("CAP8", "HEALTHY"):
    o = S["scenarios"][sc]
    g = o["geo_mean_s"]
    for a, lab in ARMS.items():
        b, ar = o[a]["vs_best_fixed"], o[a]["vs_aria2"]
        rows.append(f"{NAMES[sc]} & {lab} & {g[a]:.1f} & {FIX[b['best_fixed']]}, {g[b['best_fixed']]:.1f} & "
                    f"{fmt(b)} {b['verdict']} & {fmt(ar)} {ar['verdict']}\\\\")
        if sc == "CAP8" and b["verdict"] == "faster":
            cap8_faster.append(lab)

if cap8_faster:
    concl = (f"With the cap from 8~s, {' and '.join(cap8_faster)} was faster than the best fixed setting. "
             "This is a 5-block screen of a frozen agent that was not trained for this server.")
else:
    concl = ("The frozen agent did not beat a well-chosen fixed setting, even with a fair start, a long file "
             "and the scenario built so that the best setting changes. This is a 5-block screen.")
n_inv = len(S["invalid"])
tex = f"""To remove the handicaps of the earlier head-to-heads, the frozen DQN v6 agent was also run on the capped server with a 512~MiB file, on the fixed engine, starting either at one connection (as trained) or at 16 (as aria2 does). The protocol was written down before the first run: 2 scenarios $\\times$ 5 arms $\\times$ 5 blocks, with {n_inv} invalid run{'s' if n_inv != 1 else ''}. The agent was not retrained for this server.

\\begin{{table}}[H]\\centering
\\caption{{Frozen DQN agent on the capped and uncapped server (512~MiB; ratios are agent / other)}}\\label{{tab:rlcap}}
\\small
\\begin{{tabularx}}{{\\linewidth}}{{@{{}}llrlLL@{{}}}}
\\toprule
\\textbf{{Scenario}} & \\textbf{{Agent}} & \\textbf{{Time (s)}} & \\textbf{{Best fixed (s)}} & \\textbf{{vs best fixed}} & \\textbf{{vs aria2}}\\\\
\\midrule
{chr(10).join(rows)}
\\bottomrule
\\end{{tabularx}}
\\end{{table}}
{concl}
"""
open("reports/review3/rl_results.tex", "w").write(tex)
print(tex)
