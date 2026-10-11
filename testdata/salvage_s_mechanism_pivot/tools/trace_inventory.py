#!/usr/bin/env python3
"""Trace inventory for the adaptive-S mechanism pivot.

Reads existing trace files only (no network, no replay). Two trace kinds are
kept strictly separate:

  - Mahimahi delivery-opportunity traces: each line is a timestamp (ms) at
    which one 1500-byte packet can cross the bottleneck; repeated timestamps
    mean multiple packets in that millisecond. mm-link repeats the file at EOF
    by default. These define a time-varying link *capacity schedule*.
  - Throughput-trajectory traces (NYU-METS / UGent): measured end-to-end
    throughput samples (1 Hz / ~1 s), explicitly NOT path capacity.

Outputs: inventory/trace_inventory.json, inventory/trace_inventory.md
"""
import csv
import json
import math
import os
import statistics

BASE = "/workspaces/mvp/testdata/salvage_s_mechanism_pivot"
NATIVE = "/usr/local/share/mahimahi/traces"
NYU = "/workspaces/mvp/testdata/external/mobile_traces/raw/nyu_mets"
UGENT = "/workspaces/mvp/testdata/external/mobile_traces/raw/ugent_4g"
MIB = 1024 ** 2


def bin_rates_native(path, bin_s=1.0):
    """Delivery-opportunity trace -> per-bin aggregate capacity (bytes/s)."""
    lines = [int(x) for x in open(path).read().split()]
    if not lines:
        return None
    dur_s = lines[-1] / 1000.0
    bins = [0] * (int(dur_s / bin_s) + 1)
    for ms in lines:
        bins[int(ms / 1000.0 / bin_s)] += 1
    rates = [b * 1500 / bin_s for b in bins]
    return {"lines": len(lines), "duration_s": dur_s, "bin_rates": rates}


def summarize(rates, duration_s, lines=None):
    nz = [r for r in rates]
    mean = sum(nz) / len(nz)
    med = statistics.median(nz)
    return {
        "duration_s": duration_s,
        "mean_bytes_per_s": mean,
        "min_bytes_per_s": min(nz),
        "p10_bytes_per_s": sorted(nz)[max(0, int(0.10 * len(nz)) - 1)],
        "median_bytes_per_s": med,
        "p90_bytes_per_s": sorted(nz)[min(len(nz) - 1, int(0.90 * len(nz)))],
        "max_bytes_per_s": max(nz),
        "cv": (statistics.pstdev(nz) / mean) if mean > 0 else None,
        "p90_over_p10": (sorted(nz)[min(len(nz) - 1, int(0.90 * len(nz)))] /
                         sorted(nz)[max(0, int(0.10 * len(nz)) - 1)]) if sorted(nz)[max(0, int(0.10 * len(nz)) - 1)] > 0 else None,
        "frac_bins_below_25pct_median": sum(1 for r in nz if r < 0.25 * med) / len(nz),
        "frac_bins_above_2x_median": sum(1 for r in nz if r > 2 * med) / len(nz),
        "lines": lines,
    }


def main():
    out = {"native_delivery_opportunity": [], "throughput_trajectory": [], "semantics": {
        "native": "delivery-opportunity trace: one line = one 1500-byte packet opportunity at the given ms; mm-link loops the file at EOF; defines a time-varying capacity schedule",
        "throughput": "measured end-to-end throughput samples (1 Hz / ~1 s); NOT path capacity; replaying as a rate envelope has known limitations (see PROJECT_CONTEXT M3)",
    }}
    for name in sorted(os.listdir(NATIVE)):
        if not name.endswith(".down"):
            continue
        fam = name[:-5]
        d = bin_rates_native(os.path.join(NATIVE, name))
        if d is None:
            continue
        s = summarize(d["bin_rates"], d["duration_s"], d["lines"])
        s.update({"family": fam, "path": os.path.join(NATIVE, name),
                  "full_length_ge_450s": d["duration_s"] >= 450,
                  "suitability": "eligible capacity-schedule trace (delivery-opportunity semantics)"})
        out["native_delivery_opportunity"].append(s)

    for name in sorted(os.listdir(NYU)):
        if not name.endswith(".csv"):
            continue
        rates = []
        with open(os.path.join(NYU, name)) as f:
            for line in f:
                line = line.strip()
                if not line or not line[0].isdigit():
                    continue
                try:
                    rates.append(float(line.split(",")[0]) * 1e6 / 8)  # Mbit/s -> bytes/s
                except ValueError:
                    continue
        if not rates:
            continue
        s = summarize(rates, float(len(rates)), None)
        s.update({"name": name, "path": os.path.join(NYU, name),
                  "suitability": "throughput trajectory only (NOT capacity); ODbL 1.0 + DbCL 1.0"})
        out["throughput_trajectory"].append(s)

    for name in sorted(os.listdir(UGENT)):
        if not name.endswith(".log"):
            continue
        rates = []
        with open(os.path.join(UGENT, name)) as f:
            for line in f:
                parts = line.strip().split()
                if len(parts) < 6:
                    continue
                try:
                    byts = float(parts[4]); interval_ms = float(parts[5])
                except ValueError:
                    continue
                if interval_ms > 0:
                    rates.append(byts / (interval_ms / 1000.0))
        if not rates:
            continue
        s = summarize(rates, float(len(rates)), None)
        s.update({"name": name, "path": os.path.join(UGENT, name),
                  "suitability": "throughput trajectory only (NOT capacity); license not explicit (flagged in M2)"})
        out["throughput_trajectory"].append(s)

    json.dump(out, open(os.path.join(BASE, "inventory", "trace_inventory.json"), "w"), indent=2)

    L = ["# Trace inventory — adaptive-S mechanism pivot", "",
         "## Native Mahimahi delivery-opportunity traces (capacity schedules)", "",
         "| family | duration s | mean KiB/s | min | p10 | median | p90 | max | CV | p90/p10 | frac<25% med | >=450 s |",
         "|---|---|---|---|---|---|---|---|---|---|---|---|"]
    for t in out["native_delivery_opportunity"]:
        L.append("| %s | %.1f | %.1f | %.0f | %.0f | %.0f | %.0f | %.0f | %.2f | %.1f | %.2f | %s |" % (
            t["family"], t["duration_s"], t["mean_bytes_per_s"] / 1024, t["min_bytes_per_s"],
            t["p10_bytes_per_s"], t["median_bytes_per_s"], t["p90_bytes_per_s"], t["max_bytes_per_s"],
            t["cv"], t["p90_over_p10"], t["frac_bins_below_25pct_median"], t["full_length_ge_450s"]))
    L += ["", "## Throughput-trajectory traces (NOT capacity)", "",
          "| name | samples s | mean KiB/s | min | median | max | CV | p90/p10 | note |",
          "|---|---|---|---|---|---|---|---|---|"]
    for t in out["throughput_trajectory"]:
        L.append("| %s | %d | %.1f | %.0f | %.0f | %.0f | %.2f | %.1f | %s |" % (
            t["name"], t["duration_s"], t["mean_bytes_per_s"] / 1024, t["min_bytes_per_s"],
            t["median_bytes_per_s"], t["max_bytes_per_s"], t["cv"], t["p90_over_p10"], t["suitability"]))
    L += ["", "## Replay-model statement", "",
          "- Mahimahi traces define a packet delivery schedule (capacity schedule), which mm-link replays;",
          "  the trace file is repeated at EOF by default. Requests share this schedule.",
          "- Throughput-trajectory traces record measured end-to-end throughput and are NOT path capacity;",
          "  the M3 study showed an aggregate throughput envelope imposes the same ceiling on every",
          "  concurrency level, so it cannot represent a concurrency/request-granularity problem."]
    open(os.path.join(BASE, "inventory", "trace_inventory.md"), "w").write("\n".join(L) + "\n")
    print("native traces:", len(out["native_delivery_opportunity"]),
          "| throughput traces:", len(out["throughput_trajectory"]))
    for t in out["native_delivery_opportunity"]:
        print("  %-24s dur=%7.1fs mean=%8.1f KiB/s p90/p10=%5.1f frac<25%%=%4.2f" % (
            t["family"], t["duration_s"], t["mean_bytes_per_s"] / 1024, t["p90_over_p10"],
            t["frac_bins_below_25pct_median"]))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
