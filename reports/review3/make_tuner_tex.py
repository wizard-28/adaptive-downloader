#!/usr/bin/env python3
"""Write reports/review3/tuner_results.tex from the online-tuner analysis.

Usage: python3 reports/review3/make_tuner_tex.py   (from the repository root)
The wording follows the interpretation rules in testdata/online_tuner/protocol.md.
"""
import json

S = json.load(open("testdata/online_tuner/analysis/summary.json"))
NAMES = {"CAP8": "Cap from 8~s", "HEALTHY": "No cap"}
ARM = {"fixed_16_1M_NT_split": "16~$\\times$~1~MiB", "fixed_4_1M_NT_split": "4~$\\times$~1~MiB"}


def fmt(r):
    return f"{r['geo_ratio']:.3f} [{r['ci95'][0]:.3f}, {r['ci95'][1]:.3f}]"


rows, faster_any, faster_cap8 = [], False, False
for sc in ("CAP8", "HEALTHY"):
    o = S["scenarios"][sc]
    b, a = o["T1_vs_best_stationary"], o["T2_vs_aria2"]
    g = o["geo_mean_s"]
    tr = o["tuner_trajectory"]
    rows.append(f"{NAMES[sc]} & {g['online_tuner']:.1f} & {ARM[b['best_stationary']]}, {g[b['best_stationary']]:.1f} & "
                f"{fmt(b)} ({b['verdict']}) & {fmt(a)} ({a['verdict']}) & {tr['median_final_n']:g}\\\\")
    faster_any |= b["verdict"] == "faster"
    faster_cap8 |= sc == "CAP8" and b["verdict"] == "faster"

if not faster_any:
    concl = ("The tuner was not faster than the best fixed setting in either scenario. So an online tuner of the kind "
             "used in prior work also did not beat a well-chosen fixed setting on this platform.")
elif faster_cap8:
    concl = ("The tuner was faster than the best fixed setting with the cap switching on at 8~s, the one scenario "
             "built so that the best $N$ changes during a download.")
else:
    concl = "The tuner was faster than the best fixed setting only without a cap."

n_inv = len(S["invalid"])
tex = f"""The online tuner (Section~3.1) was compared with two fixed settings on the same engine and with aria2. The protocol was written down before the first run: 2 scenarios $\\times$ 4 arms $\\times$ 8 blocks, with {n_inv} invalid run{'s' if n_inv != 1 else ''}.

\\IfFileExists{{figs/tuner.png}}{{\\fig{{tuner.png}}{{0.9}}{{Online concurrency tuner against fixed settings and aria2 (8 blocks per scenario).}}}}{{}}

\\begin{{table}}[H]\\centering
\\caption{{Online tuner results; ratios are tuner / other}}\\label{{tab:tuner}}
\\small
\\begin{{tabularx}}{{\\linewidth}}{{@{{}}lrlLLr@{{}}}}
\\toprule
\\textbf{{Scenario}} & \\textbf{{Tuner (s)}} & \\textbf{{Best fixed (s)}} & \\textbf{{vs best fixed}} & \\textbf{{vs aria2}} & \\textbf{{Final $N$}}\\\\
\\midrule
{chr(10).join(rows)}
\\bottomrule
\\end{{tabularx}}
\\end{{table}}
{concl}
"""
open("reports/review3/tuner_results.tex", "w").write(tex)
print(tex)
