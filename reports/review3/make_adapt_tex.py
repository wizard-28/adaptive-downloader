#!/usr/bin/env python3
"""Write reports/review3/adapt_results.tex from tools/adaptation_speed.py output.

Usage: python3 reports/review3/make_adapt_tex.py JSON   (from the repository root)
"""
import json, sys

d = json.load(open(sys.argv[1]))
ROWS = [
    ("split_ablation/CAP8/causal_joint_NT_split", "Causal drop controller", "Cap switches on at 8~s", "first $N$ cut after the cap"),
    ("online_tuner/CAP8/online_tuner", "Online tuner", "Cap switches on at 8~s", "first $N$ cut after the cap"),
    ("split_ablation/CAP0/selector_NT_split", "Workload selector", "Cap from the start", "first move (classification)"),
    ("online_tuner/HEALTHY/online_tuner", "Online tuner", "No cap", "climb from $N=4$ to $N=16$"),
]
lines = []
for key, who, sc, what in ROWS:
    v = d.get(key)
    if not v or v["median"] is None:
        continue
    lines.append(f"{who} & {sc} & {what} & {v['median']:.2f} [{v['min']:.2f}, {v['max']:.2f}] & {v['n']}\\\\")
tex = r"""\begin{table}[H]\centering
\caption{Adaptation speed: median seconds, with the range over runs}\label{tab:adapt}
\small
\begin{tabularx}{\linewidth}{@{}llLrr@{}}
\toprule
\textbf{Controller} & \textbf{Scenario} & \textbf{Measured} & \textbf{Seconds} & \textbf{Runs}\\
\midrule
""" + "\n".join(lines) + r"""
\bottomrule
\end{tabularx}
\end{table}
"""
open("reports/review3/adapt_results.tex", "w").write(tex)
print(tex)
