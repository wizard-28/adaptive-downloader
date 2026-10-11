"""v5 simulator correctness audit report (simulator only; no training).

Runs the audit test modules, the real-server timing calibration, a
reproduction check of the static concurrency sweep, and a single-parameter
attribution of what creates the best-C variation. Writes
testdata/diagnostics/dqn_validation_v5/simulator_correctness_report.{json,txt}.
The existing static_concurrency_sweep_v5.* (produced by train_validate_v5.py)
is NOT overwritten; it is recomputed and compared.
"""

import json
import os
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)
OUT = os.path.join(HERE, "..", "testdata", "diagnostics", "dqn_validation_v5")
MIB = 1024 * 1024

import pytest  # noqa: E402

from rl.sim_env_v5 import Phase, SimConfig, SimEnv  # noqa: E402
import train_validate_v5 as tv  # noqa: E402


class _Collect:
    def __init__(self):
        self.results = {}

    def pytest_runtest_logreport(self, report):
        if report.when == "call" or (report.when == "setup" and report.failed):
            self.results[report.nodeid.split("::", 1)[1]] = report.outcome


def run_tests():
    c = _Collect()
    mods = [
        os.path.join(HERE, "tests", f)
        for f in ("test_sim_env_v5.py", "test_sim_env_v5_audit.py")
    ]
    rc = pytest.main(["-q", "-p", "no:cacheprovider", *mods], plugins=[c])
    return rc, c.results


def calibrate():
    # Reference: frozen real server, 1 MiB @ 512 KiB/s, one sequential request.
    # PROJECT_CONTEXT.md: matched internal 1 MiB/4C 2052.9 ms; v5-fixed ~2.05 s.
    cfg = SimConfig(
        file_size=MIB,
        phases=[
            Phase(
                rate=512 * 1024,
                link_bandwidth=0,
                latency_s=0.0,
                loss_percent=0.0,
                congestion=0.0,
                overhead_s=0.0,
            )
        ],
        decision_interval=1,
        max_episode_steps=10,
        min_concurrency=1,
        max_concurrency=1,
    )
    e = SimEnv(cfg, seed=0)
    e.reset()
    e.step(0)
    sim = e.elapsed
    real = 2.0529
    return {
        "scenario": "1 MiB @ 512 KiB/s, C=1, no latency/loss",
        "sim_seconds": sim,
        "real_server_seconds_reference": real,
        "absolute_gap_seconds": real - sim,
        "relative_gap": (real - sim) / real,
        "interpretation": (
            "The simulator is an ideal pacing model (bytes/rate); the real server "
            "adds fixed per-request HTTP/pacing overhead of ~50 ms. The gap is "
            "recorded, not fitted; full simulator fidelity is NOT claimed."
        ),
    }


def attribution():
    """Vary one mechanism at a time; report best fixed C and per-C mean seconds."""
    base = dict(rate=4 * MIB, link=0, lat=0.0, loss=0.0, cong=0.0)

    def sweep(**kw):
        p = {**base, **kw}
        ph = Phase(
            rate=p["rate"],
            link_bandwidth=p["link"],
            latency_s=p["lat"],
            loss_percent=p["loss"],
            congestion=p["cong"],
            overhead_s=0.02,
        )
        by_c = {}
        for c in (1, 2, 3, 4):
            vals = []
            for s in (300, 301, 302):
                cfg = SimConfig(
                    file_size=16 * MIB,
                    phases=[ph],
                    decision_interval=1,
                    max_episode_steps=2000,
                    min_concurrency=c,
                    max_concurrency=c,
                )
                e = SimEnv(cfg, seed=s)
                e.reset()
                while not e.done():
                    e.step(0)
                vals.append(e.elapsed)
            by_c[str(c)] = sum(vals) / len(vals)
        return {
            "mean_seconds_by_c": by_c,
            "best_c": min(range(1, 5), key=lambda c: by_c[str(c)]),
        }

    rows = {
        "no_shared_cap (per-response throttle only)": sweep(),
        "shared_cap link=1.0x rate": sweep(link=4 * MIB),
        "shared_cap link=2.0x rate": sweep(link=8 * MIB),
        "shared_cap link=3.0x rate": sweep(link=12 * MIB),
        "shared_cap link=2.0x rate + loss 1% cong 0.03": sweep(
            link=8 * MIB, loss=1.0, cong=0.03
        ),
        "no_cap + loss 2% cong 0.06": sweep(loss=2.0, cong=0.06),
        "no_cap + latency 75 ms": sweep(lat=0.075),
    }
    return rows


def sweep_reproduction():
    seeds = tv.DEFAULT_SWEEP_SEEDS
    new = tv.run_static_sweep(seeds, 1)
    old = json.load(open(os.path.join(OUT, "static_concurrency_sweep_v5.json")))
    same = (
        new["regimes"] == old["regimes"]
        and new["distinct_best_c"] == old["distinct_best_c"]
    )
    return {
        "seeds": seeds,
        "decision_interval": 1,
        "identical_to_existing_artifact": same,
        "distinct_best_c": new["distinct_best_c"],
        "best_c_by_regime": {k: v["best_c"] for k, v in new["regimes"].items()},
    }


def main():
    rc, results = run_tests()
    n_fail = sum(1 for v in results.values() if v != "passed")
    cal = calibrate()
    attr = attribution()
    rep = sweep_reproduction()
    report = {
        "label": "v5 simulator correctness audit (simulator only; NO training, NO performance claim)",
        "bug_fixed_this_audit": {
            "id": "retry-attempt-accounting",
            "summary": (
                "A failed range was logged as ONE window sample "
                "(failure=1, retry=1, completed=0), while the live runner "
                "(policy_runner.observe) records two attempts: a failed attempt "
                "and a successful retry attempt (retry=1, completed=1). The "
                "simulator now pushes both samples. Bytes were already credited "
                "exactly once and elapsed already included the retransmission; "
                "those were unchanged."
            ),
            "effect": (
                "recent_failure_rate/recent_retry_rate/recent_completed_norm and "
                "window attempts now follow the live definition. Elapsed time, "
                "bytes, rewards, RNG draw order and the static sweep are unchanged."
            ),
            "consequence_for_existing_candidate": (
                "final_dqn_candidate_v5.json was trained BEFORE this fix, on the "
                "old window semantic. It is not modified and must not be treated "
                "as trained on the corrected state distribution; any further use "
                "requires retraining (not done in this task)."
            ),
        },
        "verified_semantics": [
            "fixed 1 MiB chunk; partial final range credited once",
            "concurrency always within [1,4]; invalid action raises with no state mutation; boundary actions are no-ops",
            "one step = decision_interval requests except the final partial step",
            "C=4 runs 4 requests in parallel (pipeline time = sum(service)/active)",
            "reward == -step elapsed exactly; sum(step elapsed) == episode elapsed",
            "phase selected by elapsed wall-clock time at each request start; transition affects later work in the same step/episode",
            "transition request index depends on per-request timing (time-based, not count-based)",
            "failed range: bytes credited once, elapsed includes one retransmission, windows record failed attempt + successful retry",
            "terminal exactly when done_bytes == total; truncation only from max_episode_steps; finishing on the last allowed step is terminal",
            "randomize()/reset()/step() deterministic for a fixed seed",
            "single-phase episodes never transition; multi-phase episodes transition in >= 50% of sampled episodes",
        ],
        "modelled_limitations": [
            "a failed request is retransmitted exactly once and the retransmission cannot fail",
            "effective loss is capped at 0.9; loss/congestion are an expected-cost model, not a TCP model",
            "per-extra-connection overhead (0.02 s) and congestion increments are MODELLED, not measured",
            "dedicated real-server C=1..4 completion measurements are UNAVAILABLE; the concurrency relationship is qualitative",
            "no max-retries exhaustion: a request never permanently fails",
            "loss/congestion alone never lowers best C in this model (see attribution: no_cap + loss -> C=4); a lower best C needs a shared bottleneck, so 'loss => fewer connections' is NOT a modelled rule",
        ],
        "tests": {
            "pytest_exit_code": rc,
            "total": len(results),
            "not_passed": n_fail,
            "results": results,
        },
        "calibration": cal,
        "static_sweep_reproduction": rep,
        "best_c_attribution_single_mechanism": attr,
        "static_sweep_note": (
            "Variation in best fixed C comes from the simulator mechanics: shared "
            "bottleneck vs per-response throttle (link_bandwidth/rate), latency "
            "overlap, and loss/congestion + per-connection overhead. No optimal-C "
            "rule is hard-coded. These are modelled behaviors, not real-server evidence."
        ),
        "ready_to_train": rc == 0
        and n_fail == 0
        and rep["identical_to_existing_artifact"]
        and len(rep["distinct_best_c"]) > 1,
    }
    with open(os.path.join(OUT, "simulator_correctness_report.json"), "w") as f:
        json.dump(report, f, indent=2)
        f.write("\n")
    L = [report["label"], ""]
    L.append(f"audit tests: {len(results)} run, {n_fail} not passed")
    L.append("")
    L.append("BUG FIXED: " + report["bug_fixed_this_audit"]["id"])
    L.append("  " + report["bug_fixed_this_audit"]["summary"])
    L.append(
        "  Existing candidate: "
        + report["bug_fixed_this_audit"]["consequence_for_existing_candidate"]
    )
    L.append("")
    L.append("CALIBRATION (" + cal["scenario"] + ")")
    L.append(
        f"  sim={cal['sim_seconds']:.4f}s real~{cal['real_server_seconds_reference']:.4f}s gap={cal['absolute_gap_seconds']*1000:.1f}ms ({cal['relative_gap']*100:.1f}%)"
    )
    L.append("  " + cal["interpretation"])
    L.append("")
    L.append(
        "STATIC SWEEP reproduction: identical=%s distinct best C=%s"
        % (rep["identical_to_existing_artifact"], rep["distinct_best_c"])
    )
    for k, v in rep["best_c_by_regime"].items():
        L.append(f"  {k:28s} best C={v}")
    L.append("")
    L.append(
        "BEST-C ATTRIBUTION (single mechanism, 16 MiB, rate 4 MiB/s, seeds 300-302; mean s)"
    )
    for k, v in attr.items():
        m = v["mean_seconds_by_c"]
        L.append(
            f"  {k:48s} "
            + " ".join(f"C{c}={m[c]:6.3f}" for c in "1234")
            + f"  best C={v['best_c']}"
        )
    L.append("")
    L.append("MODELLED LIMITATIONS")
    L += ["  - " + x for x in report["modelled_limitations"]]
    L.append("")
    L.append(
        "READY TO TRAIN (simulator correctness gate): %s" % report["ready_to_train"]
    )
    open(os.path.join(OUT, "simulator_correctness_report.txt"), "w").write(
        "\n".join(L) + "\n"
    )
    print("\n".join(L))
    return 0 if report["ready_to_train"] else 1


if __name__ == "__main__":
    sys.exit(main())
