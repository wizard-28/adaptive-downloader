"""pytest: tools/analyze_live_pilot.py on synthetic records."""
import importlib.util, json, os, subprocess, sys

HERE = os.path.dirname(os.path.abspath(__file__))
spec = importlib.util.spec_from_file_location("alp", os.path.join(HERE, "analyze_live_pilot.py"))
alp = importlib.util.module_from_spec(spec)
spec.loader.exec_module(alp)

CELLS = [(1, 1), (4, 1), (16, 1)]


def rec(server, slot, arm, dur, rep=0, valid=True, kind="ours", tl=None):
    r = {"server": server, "slot": slot, "arm": arm, "kind": kind, "rep": rep,
         "duration_s": dur, "valid": valid}
    if tl is not None:
        r["timeline_bytes"], r["timeline_ms"] = tl, 100
    return r


def arm(n, s):
    return f"ours_N{n}_S{s}M"


def synthetic():
    # server A: N16 best in both slots; server B: N4 best in S1, link-bound in S2.
    times = {("A", "S1"): [100, 50, 20], ("A", "S2"): [110, 55, 22],
             ("B", "S1"): [60, 30, 40], ("B", "S2"): [30, 30.5, 30.8]}
    recs = []
    for (srv, slot), ts in times.items():
        for (n, s), t in zip(CELLS, ts):
            for rep in (0, 1):
                recs.append(rec(srv, slot, arm(n, s), t, rep))
        recs.append(rec(srv, slot, "aria2", ts[2] * 1.1, kind="aria2"))
    recs.append(rec("A", "S1", arm(4, 1), 999, rep=2, valid=False))
    return recs


def test_conditions_best_and_link_bound():
    res = alp.analyze(synthetic())
    by = {(e["server"], e["slot"]): e for e in res["conditions"]}
    assert by[("A", "S1")]["best_cell"] == arm(16, 1)
    assert by[("B", "S1")]["best_cell"] == arm(4, 1)
    assert by[("B", "S2")]["status"] == "ACCESS_LINK_BOUND"
    assert by[("A", "S1")]["status"] == "OK"
    assert res["n_invalid"] == 1 and res["invalid_by_condition"]["A|S1"][0]["arm"] == arm(4, 1)
    assert abs(by[("A", "S1")]["aria2_over_best_fixed"] - 1.1) < 1e-9
    assert res["top_cell_stability"]["per_server_distinct"]["A"] == 1


def test_headroom_matches_hand_computation():
    res = alp.analyze(synthetic())
    h = res["headroom_all_complete"]
    # Global choice N16: ratios A:1,1; B-S1: 40/30; B-S2: 30.8/30 -> gmean.
    want = alp.gmean([1, 1, 40 / 30, 30.8 / 30])
    # N4 alternative: A 2.5,2.5; B 1, 30.5/30 -> larger; so N16 is the global cell.
    assert h["global_cell"] == arm(16, 1)
    assert abs(h["headroom_pct"] - 100 * (want - 1)) < 1e-9
    assert h["boot95_pct"][0] <= h["headroom_pct"] <= h["boot95_pct"][1]
    hn = res["headroom_excluding_access_link_bound"]
    assert hn["n_conditions"] == 3


def test_incomplete_condition_excluded():
    recs = [r for r in synthetic() if not (r["server"] == "B" and r["slot"] == "S2" and r["arm"] == arm(1, 1))]
    res = alp.analyze(recs)
    by = {(e["server"], e["slot"]): e for e in res["conditions"]}
    assert by[("B", "S2")]["status"] == "INCOMPLETE"
    assert res["headroom_all_complete"]["n_conditions"] == 3


def test_variability():
    flat = [100] * 100            # 10 s of constant 1 kB/s bins
    bursty = ([100] * 10 + [0] * 10) * 5
    recs = [rec("A", "S1", arm(1, 1), 10, tl=flat), rec("B", "S1", arm(1, 1), 10, tl=bursty)]
    v = alp.variability(recs)
    assert v["A"]["median_cv"] == 0 and v["A"]["median_frac_below_half_median"] == 0
    assert v["B"]["median_cv"] > 0.9


def test_cli(tmp_path):
    raw = tmp_path / "raw"
    raw.mkdir()
    with open(raw / "S1.jsonl", "w") as f:
        for r in synthetic():
            f.write(json.dumps(r) + "\n")
    out = tmp_path / "an"
    p = subprocess.run([sys.executable, os.path.join(HERE, "analyze_live_pilot.py"), "--raw", str(raw),
                        "--out", str(out), "--no-figures"], capture_output=True, text=True)
    assert p.returncode == 0, p.stderr
    assert json.load(open(out / "summary.json"))["label"] == "Descriptive"
