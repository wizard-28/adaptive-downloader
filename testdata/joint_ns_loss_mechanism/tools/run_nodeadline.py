#!/usr/bin/env python3
"""Re-run of run_screen.py / run_robustness.py with a configurable deadline.
usage: run_nodeadline.py <timeout_seconds|inf> <outdir>"""
import json, math, os, sys
here = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, here)
import loss_sim_nodeadline as L
sys.modules["loss_sim"] = L          # frozen runners import loss_sim
L.DEFAULT_TIMEOUT = float(sys.argv[1])
out = sys.argv[2]; os.makedirs(out, exist_ok=True)
import run_screen, run_robustness
run_screen.OUT = out; run_robustness.OUT = out
import contextlib
with open(os.path.join(out, "run_screen.log"), "w") as f, contextlib.redirect_stdout(f):
    run_screen.main()
with open(os.path.join(out, "run_robustness.log"), "w") as f, contextlib.redirect_stdout(f):
    run_robustness.main()
