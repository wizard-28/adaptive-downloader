#!/usr/bin/env python3
"""Capacity swing of the five replayed Mahimahi traces (paper §8).

Reads each trace's downlink file (one line per 1500-byte delivery
opportunity, in ms), bins it into 1 s capacity samples and reports the
90th/10th percentile ratio. Writes testdata/final_oracle_analysis/trace_swing.json,
which tools/claims_check.py reads (the trace files are not in the repository).

Usage: python3 tools/trace_swing.py [TRACE_DIR]
"""
import json
import os
import sys

TRACES = ["Verizon-EVDO-driving", "Verizon-LTE-driving", "ATT-LTE-driving", "TMobile-LTE-driving",
          "TMobile-UMTS-driving"]
OUT = "testdata/final_oracle_analysis/trace_swing.json"


def swing(path):
    with open(path) as f:
        ts = [int(x) for x in f if x.strip()]
    bins = [0] * (ts[-1] // 1000 + 1)
    for t in ts:
        bins[t // 1000] += 1
    s = sorted(b * 1500 * 8 / 1e6 for b in bins)
    p10, p90 = s[int(0.10 * len(s))], s[min(len(s) - 1, int(0.90 * len(s)))]
    return {"p10_mbps": p10, "p90_mbps": p90, "p90_over_p10": p90 / p10, "seconds": len(s)}


def main():
    d = sys.argv[1] if len(sys.argv) > 1 else "mahimahi-clone-dir/mahimahi/traces"
    res = {t: swing(os.path.join(d, t + ".down")) for t in TRACES}
    out = {"label": "downlink capacity, 1 s bins, p90/p10 per trace", "source_dir": d, "traces": res,
           "min_swing": min(r["p90_over_p10"] for r in res.values()),
           "max_swing": max(r["p90_over_p10"] for r in res.values())}
    with open(OUT, "w") as f:
        json.dump(out, f, indent=2)
    print(json.dumps({t: round(r["p90_over_p10"], 2) for t, r in res.items()}))


if __name__ == "__main__":
    main()
