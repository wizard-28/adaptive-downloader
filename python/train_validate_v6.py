"""Train, select, and audit DQN Candidate v6: concurrency 1-16 under a shared
time-varying bottleneck (pre-registered in PROJECT_CONTEXT §22.33).

Pipeline: pre-registered trajectories -> multi-seed Double DQN training ->
validation-only selection -> held-out dynamic test vs fixed C=1..16 ->
condition-dependence diagnostics + gates B/C -> export one candidate.

No aria2 comparison here; no performance claim beyond the held-out conditions.

Run the static sweep first (GATE A):
    python3 python/v6_static_sweep.py
then:
    python3 python/train_validate_v6.py [--quick]
"""

from __future__ import annotations

import argparse
import hashlib
import json
import math
import os
import platform
import statistics as st
import sys
import time

import numpy as np
import torch

sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))

from rl.dqn import TrainConfig, train  # noqa: E402
from rl.model import export_model  # noqa: E402
from rl.state_v6 import default_state_spec as default_state_spec_v6  # noqa: E402
from rl.sim_env_v6 import (  # noqa: E402
    ACTION_NAMES,
    FIXED_CHUNK,
    NUM_ACTIONS,
    Phase,
    SimConfig,
    SimEnv as SimEnvV6,
)

MIB = 1024 * 1024
EPISODES = 1200
MAX_STEPS = 120
DEFAULT_SEEDS = [0, 1, 2, 3, 4]
VAL_SEEDS = [100, 101]
TEST_SEEDS = [200, 201, 202]
PHASE_SECONDS_TRAIN = (1.5, 3.0)  # randomized training phase durations
MAX_CONC = 16
FIXED_CS = list(range(1, 17))


def ph(rate, link=0, lat=0.0, loss=0.0, cong=0.0):
    return {"rate": rate, "link_bandwidth": link, "latency_s": lat,
            "loss_percent": loss, "congestion": cong}


# ---- Pre-registered trajectory families (training samples within ranges) ----
def sample_family(rng, family):
    f = TRAIN_FAMILIES[family]
    phases = []
    for spec in f:
        phases.append(ph(
            rate=rng.uniform(*spec["rate"]),
            link=int(rng.uniform(*spec["link"])),
            lat=rng.uniform(*spec["lat"]),
            loss=rng.uniform(*spec["loss"]),
            cong=rng.uniform(*spec["cong"]),
        ))
    return phases


TRAIN_FAMILIES = {
    # A: bandwidth collapse / recovery
    "A": [
        {"rate": (8 * MIB, 8 * MIB), "link": (3.5 * MIB, 5.5 * MIB), "lat": (0.02, 0.05), "loss": (0.2, 0.8), "cong": (0.002, 0.008)},
        {"rate": (8 * MIB, 8 * MIB), "link": (0.8 * MIB, 1.5 * MIB), "lat": (0.02, 0.05), "loss": (0.2, 0.8), "cong": (0.002, 0.008)},
        {"rate": (8 * MIB, 8 * MIB), "link": (3.5 * MIB, 5.5 * MIB), "lat": (0.02, 0.05), "loss": (0.2, 0.8), "cong": (0.002, 0.008)},
    ],
    # B: latency spike / recovery
    "B": [
        {"rate": (8 * MIB, 8 * MIB), "link": (3.5 * MIB, 4.5 * MIB), "lat": (0.01, 0.02), "loss": (0.2, 0.8), "cong": (0.002, 0.008)},
        {"rate": (8 * MIB, 8 * MIB), "link": (3.5 * MIB, 4.5 * MIB), "lat": (0.10, 0.15), "loss": (0.2, 0.8), "cong": (0.002, 0.008)},
        {"rate": (8 * MIB, 8 * MIB), "link": (3.5 * MIB, 4.5 * MIB), "lat": (0.01, 0.02), "loss": (0.2, 0.8), "cong": (0.002, 0.008)},
    ],
    # C: burst-loss episode
    "C": [
        {"rate": (8 * MIB, 8 * MIB), "link": (3.5 * MIB, 5 * MIB), "lat": (0.02, 0.05), "loss": (0.2, 0.5), "cong": (0.01, 0.02)},
        {"rate": (8 * MIB, 8 * MIB), "link": (3.5 * MIB, 5 * MIB), "lat": (0.02, 0.05), "loss": (3.0, 4.5), "cong": (0.01, 0.02)},
        {"rate": (8 * MIB, 8 * MIB), "link": (3.5 * MIB, 5 * MIB), "lat": (0.02, 0.05), "loss": (0.2, 0.5), "cong": (0.01, 0.02)},
    ],
    # D: server throttle / release (no shared cap; per-response pacing)
    "D": [
        {"rate": (512 * 1024, 1 * MIB), "link": (0, 0), "lat": (0.02, 0.05), "loss": (0.2, 0.5), "cong": (0.0, 0.005)},
        {"rate": (128 * 1024, 256 * 1024), "link": (0, 0), "lat": (0.02, 0.05), "loss": (0.2, 0.5), "cong": (0.0, 0.005)},
        {"rate": (4 * MIB, 8 * MIB), "link": (0, 0), "lat": (0.02, 0.05), "loss": (0.2, 0.5), "cong": (0.0, 0.005)},
    ],
    # E: combined deterioration / partial recovery
    "E": [
        {"rate": (8 * MIB, 8 * MIB), "link": (3.5 * MIB, 4.5 * MIB), "lat": (0.01, 0.03), "loss": (0.2, 0.5), "cong": (0.002, 0.005)},
        {"rate": (8 * MIB, 8 * MIB), "link": (1.2 * MIB, 2.0 * MIB), "lat": (0.10, 0.15), "loss": (3.0, 4.5), "cong": (0.015, 0.03)},
        {"rate": (8 * MIB, 8 * MIB), "link": (3.0 * MIB, 4.5 * MIB), "lat": (0.04, 0.07), "loss": (1.0, 2.0), "cong": (0.008, 0.015)},
    ],
}

# ---- Held-out fixed trajectories (pinned durations) ----
def fixed_condition(name, family, phase_params, secs=2.0):
    return {
        "name": name, "family": family, "phase_seconds": secs,
        "phases": phase_params,
    }


VALIDATION_CONDITIONS = [
    fixed_condition("A1", "A", [
        ph(8 * MIB, link=4 * MIB, lat=0.03, loss=0.5, cong=0.005),
        ph(8 * MIB, link=1.2 * MIB, lat=0.03, loss=0.5, cong=0.005),
        ph(8 * MIB, link=3.5 * MIB, lat=0.03, loss=0.5, cong=0.005)]),
    fixed_condition("B1", "B", [
        ph(8 * MIB, link=4 * MIB, lat=0.015, loss=0.5, cong=0.005),
        ph(8 * MIB, link=4 * MIB, lat=0.13, loss=0.5, cong=0.005),
        ph(8 * MIB, link=4 * MIB, lat=0.015, loss=0.5, cong=0.005)]),
    fixed_condition("C1", "C", [
        ph(8 * MIB, link=4 * MIB, lat=0.03, loss=0.3, cong=0.015),
        ph(8 * MIB, link=4 * MIB, lat=0.03, loss=4.0, cong=0.015),
        ph(8 * MIB, link=4 * MIB, lat=0.03, loss=0.3, cong=0.015)]),
    fixed_condition("D1", "D", [
        ph(768 * 1024, lat=0.03, loss=0.3),
        ph(192 * 1024, lat=0.03, loss=0.3),
        ph(3 * MIB, lat=0.03, loss=0.3)]),
    fixed_condition("E1", "E", [
        ph(8 * MIB, link=4 * MIB, lat=0.02, loss=0.3, cong=0.003),
        ph(8 * MIB, link=1.5 * MIB, lat=0.12, loss=3.5, cong=0.02),
        ph(8 * MIB, link=3.2 * MIB, lat=0.05, loss=1.5, cong=0.01)]),
]

TEST_CONDITIONS = [
    fixed_condition("A2", "A", [
        ph(8 * MIB, link=5 * MIB, lat=0.05, loss=0.4, cong=0.006),
        ph(8 * MIB, link=0.9 * MIB, lat=0.05, loss=0.4, cong=0.006),
        ph(8 * MIB, link=4 * MIB, lat=0.05, loss=0.4, cong=0.006)]),
    fixed_condition("B2", "B", [
        ph(8 * MIB, link=4.5 * MIB, lat=0.02, loss=0.4, cong=0.006),
        ph(8 * MIB, link=4.5 * MIB, lat=0.11, loss=0.4, cong=0.006),
        ph(8 * MIB, link=4.5 * MIB, lat=0.02, loss=0.4, cong=0.006)]),
    fixed_condition("C2", "C", [
        ph(8 * MIB, link=4.5 * MIB, lat=0.04, loss=0.4, cong=0.025),
        ph(8 * MIB, link=4.5 * MIB, lat=0.04, loss=3.2, cong=0.025),
        ph(8 * MIB, link=4.5 * MIB, lat=0.04, loss=0.4, cong=0.025)]),
    fixed_condition("D2", "D", [
        ph(640 * 1024, lat=0.04, loss=0.4, cong=0.004),
        ph(160 * 1024, lat=0.04, loss=0.4, cong=0.004),
        ph(2.5 * MIB, lat=0.04, loss=0.4, cong=0.004)]),
    fixed_condition("E2", "E", [
        ph(8 * MIB, link=4.5 * MIB, lat=0.02, loss=0.4, cong=0.004),
        ph(8 * MIB, link=1.8 * MIB, lat=0.11, loss=3.8, cong=0.018),
        ph(8 * MIB, link=3.5 * MIB, lat=0.06, loss=1.7, cong=0.012)]),
]


class TrajectoryEnv(SimEnvV6):
    """Training env: samples a pre-registered trajectory family per episode."""

    def randomize(self) -> None:
        family = self.rng.choice(sorted(TRAIN_FAMILIES))
        self.cfg.phases = [Phase(**p) for p in sample_family(self.rng, family)]
        self.cfg.file_size = self.rng.randint(16, 32) * MIB


def train_cfg():
    return SimConfig(
        file_size=16 * MIB,
        min_concurrency=1,
        max_concurrency=MAX_CONC,
        decision_interval=1,
        max_episode_steps=MAX_STEPS,
        rand_phase_seconds=PHASE_SECONDS_TRAIN,
    )


def make_env(cond, seed, fixed_c=None):
    lo, hi = (fixed_c, fixed_c) if fixed_c else (1, MAX_CONC)
    cfg = SimConfig(
        file_size=16 * MIB,
        min_concurrency=lo,
        max_concurrency=hi,
        decision_interval=1,
        max_episode_steps=MAX_STEPS,
        phases=[Phase(**p) for p in cond["phases"]],
        rand_phase_seconds=(cond["phase_seconds"], cond["phase_seconds"]),
    )
    return SimEnvV6(cfg, seed=seed)


def rollout(cond, seed, agent=None, fixed_c=None):
    env = make_env(cond, seed, fixed_c=fixed_c)
    env.reset()
    n_phases = len(cond["phases"])
    total, steps = 0.0, 0
    action_counts = [0] * NUM_ACTIONS
    phase_time = [0.0] * n_phases
    phase_decisions = [0] * n_phases
    phase_conc_time = [0.0] * n_phases
    conc_time = [0.0] * (MAX_CONC + 1)
    changes = []
    while not env.done() and steps < MAX_STEPS + 5:
        pi = env.phase_index
        action = agent.greedy(env.state()) if agent is not None else 0
        c_before = env.concurrency
        res = env.step(action)
        elapsed = res["elapsed"]
        total += elapsed
        steps += 1
        phase_time[pi] += elapsed
        phase_decisions[pi] += 1
        phase_conc_time[pi] += env.concurrency * elapsed
        conc_time[env.concurrency] += elapsed
        action_counts[action] += 1
        if env.concurrency != c_before:
            changes.append({"phase": pi, "t": total - elapsed, "from": c_before, "to": env.concurrency})
    mi = float(env.total) / MIB
    mean_conc_phase = [
        (phase_conc_time[i] / phase_time[i] if phase_time[i] > 0 else float("nan"))
        for i in range(n_phases)
    ]
    return {
        "completion_s": total,
        "steps": steps,
        "requests": env.requests,
        "file_mib": mi,
        "action_counts": action_counts,
        "final_concurrency": env.concurrency,
        "max_concurrency": env.max_conc,
        "time_weighted_concurrency": (
            sum(c * t for c, t in enumerate(conc_time)) / total if total > 0 else 0.0
        ),
        "effective_changes": len(changes),
        "changes": changes,
        "changes_in_phase": [sum(1 for c in changes if c["phase"] == i) for i in range(n_phases)],
        "phase_time": phase_time,
        "phase_decisions": phase_decisions,
        "mean_concurrency_phase": mean_conc_phase,
        "reaction_latency_s": _first_change_after(changes, cond["phase_seconds"]),
        "constant_run": len(changes) == 0,
        "ramp_to_max": _ramp_to_max(changes, env.concurrency),
    }


def _first_change_after(changes, transition_s):
    for c in changes:
        if c["t"] >= transition_s:
            return c["t"] - transition_s
    return -1.0


def _ramp_to_max(changes, final_c):
    if not changes:
        return False
    if any(c["to"] < c["from"] for c in changes):
        return False
    return final_c == MAX_CONC


def summarize(rows):
    comp = [r["completion_s"] for r in rows]
    return {
        "n": len(rows),
        "mean_completion_s": float(st.mean(comp)),
        "median_completion_s": float(st.median(comp)),
        "sd_completion_s": float(st.pstdev(comp)) if len(comp) > 1 else 0.0,
        "min_completion_s": float(min(comp)),
        "max_completion_s": float(max(comp)),
        "mean_requests": float(st.mean([r["requests"] for r in rows])),
        "mean_effective_changes": float(st.mean([r["effective_changes"] for r in rows])),
        "constant_fraction": float(st.mean([1.0 if r["constant_run"] else 0.0 for r in rows])),
        "ramp_to_max_fraction": float(st.mean([1.0 if r["ramp_to_max"] else 0.0 for r in rows])),
        "final_concurrency_counts": {
            str(c): sum(1 for r in rows if r["final_concurrency"] == c) for c in range(1, MAX_CONC + 1)
        },
    }


def evaluate_conditions(conditions, seeds, agent=None, fixed_c=None):
    rows = []
    for cond in conditions:
        for s in seeds:
            r = rollout(cond, s, agent=agent, fixed_c=fixed_c)
            r["condition"] = cond["name"]
            rows.append(r)
    return rows


def evaluate_fixed_all(conditions, seeds):
    out = {}
    for c in FIXED_CS:
        rows = evaluate_conditions(conditions, seeds, fixed_c=c)
        out[str(c)] = {"rows": rows, "summary": summarize(rows)}
    return out


def write_json(path, obj):
    with open(path, "w") as f:
        json.dump(obj, f, indent=2, sort_keys=True)
        f.write("\n")


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--out", default="testdata/diagnostics/dqn_validation_v6")
    ap.add_argument("--episodes", type=int, default=EPISODES)
    ap.add_argument("--seeds", default=",".join(str(s) for s in DEFAULT_SEEDS))
    ap.add_argument("--val-seeds", default=",".join(str(s) for s in VAL_SEEDS))
    ap.add_argument("--test-seeds", default=",".join(str(s) for s in TEST_SEEDS))
    ap.add_argument("--quick", action="store_true", help="tiny deterministic smoke run")
    args = ap.parse_args()
    if args.quick:
        args.episodes = min(args.episodes, 60)
        args.seeds = "0"
        args.val_seeds = "100"
        args.test_seeds = "200"
    seeds = [int(x) for x in args.seeds.split(",") if x]
    val_seeds = [int(x) for x in args.val_seeds.split(",") if x]
    test_seeds = [int(x) for x in args.test_seeds.split(",") if x]
    os.makedirs(args.out, exist_ok=True)

    # ---- multi-seed training + validation-only selection ----
    sim_cfg = train_cfg()
    seed_results = []
    agents = {}
    best_global_fixed_val = None
    for seed in seeds:
        cfg = TrainConfig(episodes=args.episodes, seed=seed, num_actions=NUM_ACTIONS)
        t0 = time.time()
        agent, report = train(cfg, sim_cfg, env_class=TrajectoryEnv)
        val_rows = evaluate_conditions(VALIDATION_CONDITIONS, val_seeds, agent=agent)
        val = summarize(val_rows)
        seed_results.append({
            "seed": seed,
            "final_loss": report["final_loss"],
            "mean_return": report["mean_return"],
            "q_min": report["q_min"],
            "q_max": report["q_max"],
            "truncated_episodes": report["truncated_episodes"],
            "validation_mean_completion_s": val["mean_completion_s"],
            "validation_constant_fraction": val["constant_fraction"],
            "wall_seconds": time.time() - t0,
        })
        agents[seed] = (agent, cfg, val)
        loss = report["final_loss"]
        print(f"seed {seed}: val_mean={val['mean_completion_s']:.3f}s "
              f"loss={loss if loss is None else round(loss, 5)}")

    best_seed = min(seed_results, key=lambda r: (r["validation_mean_completion_s"], r["seed"]))["seed"]
    agent, cfg, val_summary = agents[best_seed]

    # Best global fixed C chosen on VALIDATION only (for gate B).
    fixed_val = evaluate_fixed_all(VALIDATION_CONDITIONS, val_seeds)
    best_global_fixed_c = min(
        FIXED_CS, key=lambda c: fixed_val[str(c)]["summary"]["mean_completion_s"]
    )
    print(f"selected seed {best_seed}; best global fixed C on validation = {best_global_fixed_c}")

    # ---- held-out test: v6 + all fixed C ----
    v6_rows = evaluate_conditions(TEST_CONDITIONS, test_seeds, agent=agent)
    fixed_test = evaluate_fixed_all(TEST_CONDITIONS, test_seeds)
    v6_overall = summarize(v6_rows)
    fixed_overall = {str(c): fixed_test[str(c)]["summary"] for c in FIXED_CS}
    best_fixed_overall_c = min(
        FIXED_CS, key=lambda c: fixed_overall[str(c)]["mean_completion_s"]
    )

    by_condition = {}
    for cond in TEST_CONDITIONS:
        name = cond["name"]
        vsub = [r for r in v6_rows if r["condition"] == name]
        fsub = {
            str(c): [r for r in fixed_test[str(c)]["rows"] if r["condition"] == name]
            for c in FIXED_CS
        }
        fmeans = {str(c): summarize(fsub[str(c)])["mean_completion_s"] for c in FIXED_CS}
        by_condition[name] = {
            "v6": summarize(vsub),
            "fixed_mean_completion_s": fmeans,
            "best_fixed_c": min(FIXED_CS, key=lambda c: fmeans[str(c)]),
            "mean_concurrency_phase_v6": [float(np.nanmean([r["mean_concurrency_phase"][i] for r in vsub])) for i in range(3)],
            "changes_in_phase_v6": [int(sum(r["changes_in_phase"][i] for r in vsub)) for i in range(3)],
            "reaction_latency_s_v6": (
                sum(r["reaction_latency_s"] for r in vsub if r["reaction_latency_s"] >= 0)
                / max(1, sum(1 for r in vsub if r["reaction_latency_s"] >= 0))
            ),
            "reaction_count_v6": sum(1 for r in vsub if r["reaction_latency_s"] >= 0),
        }

    # ---- adaptation diagnostics and gates B/C (pre-registered) ----
    v6_constant = v6_overall["constant_fraction"]
    v6_ramp = v6_overall["ramp_to_max_fraction"]
    resp_conditions = 0
    for name, b in by_condition.items():
        mc = [x for x in b["mean_concurrency_phase_v6"] if not math.isnan(x)]
        if mc and max(mc) - min(mc) >= 2.0:
            resp_conditions += 1
    responsiveness = resp_conditions / len(TEST_CONDITIONS)
    fixed_test_best_global = fixed_overall[str(best_global_fixed_c)]["mean_completion_s"]

    gate_a_passed = None
    gate_a_path = os.path.join(args.out, "static_concurrency_sweep_v6.json")
    if os.path.exists(gate_a_path):
        with open(gate_a_path) as f:
            gate_a_passed = json.load(f)["gate_a"]["multiple_meaningful_optima"]

    gate_b = v6_overall["mean_completion_s"] < fixed_test_best_global
    gate_c = v6_constant < 0.9 and v6_ramp < 0.9 and responsiveness >= 0.5
    gates = {
        "GATE_A_environment_validity": {
            "passed": gate_a_passed,
            "rule": "static C=1..16 sweep shows multiple meaningful concurrency optima",
            "detail": "see static_concurrency_sweep_v6.json (run python/v6_static_sweep.py first)",
        },
        "GATE_B_training_validity": {
            "passed": bool(gate_b),
            "v6_mean_completion_s": v6_overall["mean_completion_s"],
            "best_fixed_c_on_validation": best_global_fixed_c,
            "best_fixed_test_mean_completion_s": fixed_test_best_global,
            "rule": "v6 held-out mean completion < best globally-fixed C (chosen on validation)",
        },
        "GATE_C_condition_dependent": {
            "passed": bool(gate_c),
            "constant_fraction": v6_constant,
            "ramp_to_max_fraction": v6_ramp,
            "phase_responsive_conditions_fraction": responsiveness,
            "rule": "not constant (<0.9), not ramp-to-max (<0.9), and >=50% of held-out conditions show a phase-to-phase time-weighted concurrency spread >= 2.0",
        },
    }
    verdict = "READY_FOR_LIVE" if all(g["passed"] for g in gates.values()) else "NOT_READY"

    # ---- export one candidate ----
    export_sim_cfg = train_cfg()
    export_sim_cfg.spec = default_state_spec_v6()
    candidate = export_model(
        agent, cfg, export_sim_cfg,
        label="FINAL DQN CANDIDATE v6 (experimental; concurrency 1-16; shared-bottleneck simulator)",
        action_names=list(ACTION_NAMES), num_actions=NUM_ACTIONS,
    )
    cand_bytes = json.dumps(candidate, indent=2, sort_keys=True).encode()
    with open(os.path.join(args.out, "final_dqn_candidate_v6.json"), "wb") as f:
        f.write(cand_bytes)
    cand_hash = hashlib.sha256(cand_bytes).hexdigest()
    with open(os.path.join(args.out, "final_dqn_candidate_v6.sha256"), "w") as f:
        f.write(cand_hash + "\n")

    write_json(os.path.join(args.out, "train_config_v6.json"), {
        "train": cfg.to_json(),
        "seeds": seeds, "selected_seed": best_seed,
        "episodes": args.episodes,
        "decision_interval": 1,
        "action_space": ACTION_NAMES, "num_actions": NUM_ACTIONS,
        "fixed_chunk_bytes": FIXED_CHUNK,
        "state_spec_version": export_sim_cfg.spec.version,
        "concurrency_bounds": [1, MAX_CONC],
        "training_families": {k: v for k, v in sorted(TRAIN_FAMILIES.items())},
        "phase_seconds_train": list(PHASE_SECONDS_TRAIN),
        "validation_conditions": VALIDATION_CONDITIONS,
        "test_conditions": TEST_CONDITIONS,
        "seed_separation": {"train": seeds, "validation": val_seeds, "test": test_seeds},
        "selection_criterion": "min mean validation completion time; tie-break lowest seed (test never used)",
        "python": platform.python_version(), "torch": torch.__version__, "numpy": np.__version__,
    })
    write_json(os.path.join(args.out, "seed_results_v6.json"),
               {"seeds": seed_results, "selected_seed": best_seed})
    write_json(os.path.join(args.out, "validation_results_v6.json"), {
        "conditions": VALIDATION_CONDITIONS, "seeds": val_seeds,
        "selected_seed": best_seed, "v6_summary": val_summary,
        "best_global_fixed_c": best_global_fixed_c,
        "fixed_validation": {c: fixed_val[c]["summary"] for c in fixed_val},
    })
    write_json(os.path.join(args.out, "heldout_test_results_v6.json"), {
        "v6_overall": v6_overall,
        "fixed_overall": fixed_overall,
        "best_global_fixed_c_on_test": best_fixed_overall_c,
        "by_condition": by_condition,
    })
    write_json(os.path.join(args.out, "adaptation_v6.json"), {
        "by_condition": {
            name: {
                "mean_concurrency_phase": by_condition[name]["mean_concurrency_phase_v6"],
                "changes_in_phase": by_condition[name]["changes_in_phase_v6"],
                "reaction_latency_s": by_condition[name]["reaction_latency_s_v6"],
                "reaction_count": by_condition[name]["reaction_count_v6"],
                "v6_final_concurrency_counts": by_condition[name]["v6"]["final_concurrency_counts"],
            } for name in by_condition
        },
        "constant_fraction": v6_constant,
        "ramp_to_max_fraction": v6_ramp,
        "phase_responsive_conditions_fraction": responsiveness,
    })
    write_json(os.path.join(args.out, "gates_v6.json"), gates)
    write_json(os.path.join(args.out, "report_v6.json"), {
        "label": "Candidate v6 training/held-out validation (simulator only; no aria2)",
        "candidate_sha256": cand_hash, "selected_seed": best_seed,
        "gates": gates, "verdict": verdict,
        "v6_overall": v6_overall,
        "best_fixed_c_on_validation": best_global_fixed_c,
        "live_transfer_run": False, "aria2_comparison_run": False,
    })

    # ---- human-readable report ----
    lines = []
    lines.append("Candidate v6 (concurrency 1-16, shared-bottleneck simulator)")
    lines.append("Simulator-only training + held-out validation. NO aria2 comparison; NO performance claim.")
    lines.append("")
    lines.append(f"candidate sha256 : {cand_hash}")
    lines.append(f"selected seed    : {best_seed}   episodes/seed: {args.episodes}")
    lines.append(f"verdict          : {verdict}")
    for gname, g in gates.items():
        lines.append(f"  {gname}: {'PASS' if g['passed'] else 'FAIL'} ({g['rule']})")
    lines.append("")
    lines.append("held-out test (mean completion s; best fixed C per condition):")
    lines.append(f"  {'cond':<5} {'v6':>8} {'best C':>7} {'best fixed':>11} {'meanC p0':>9} {'p1':>6} {'p2':>6}")
    for name, b in by_condition.items():
        mc = b["mean_concurrency_phase_v6"]
        lines.append(
            f"  {name:<5} {b['v6']['mean_completion_s']:>8.3f} C{b['best_fixed_c']:<6} "
            f"{b['fixed_mean_completion_s'][str(b['best_fixed_c'])]:>11.3f} "
            f"{mc[0]:>9.2f} {mc[1]:>6.2f} {mc[2]:>6.2f}"
        )
    lines.append("")
    lines.append(f"v6 overall mean: {v6_overall['mean_completion_s']:.3f}s; "
                 f"best global fixed C on validation C{best_global_fixed_c}: {fixed_test_best_global:.3f}s")
    lines.append(f"v6 constant fraction {v6_constant:.3f}; ramp-to-max {v6_ramp:.3f}; "
                 f"phase-responsive conditions {resp_conditions}/{len(TEST_CONDITIONS)}")
    lines.append("")
    lines.append("Caveats: simulator-only; controlled shared-bottleneck profiles; no real-network claim.")
    with open(os.path.join(args.out, "report_v6.txt"), "w") as f:
        f.write("\n".join(lines) + "\n")

    print(f"selected seed {best_seed}; candidate {cand_hash}; verdict {verdict}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
