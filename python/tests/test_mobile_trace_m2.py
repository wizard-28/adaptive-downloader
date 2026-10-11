import os
import sys
import tempfile

sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))

from rl.mobile_trace import (  # noqa: E402
    canonicalize,
    parse_nyu_csv,
    parse_ugent_log,
    quality,
    sha256_file,
)

ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", ".."))
NYU = os.path.join(ROOT, "testdata", "external", "mobile_traces", "raw", "nyu_mets", "7Train1.csv")
UGENT = os.path.join(ROOT, "testdata", "external", "mobile_traces", "raw", "ugent_4g", "report_train_0001.log")


def test_nyu_units_order_and_density():
    rows = parse_nyu_csv(NYU)
    assert len(rows) > 4000
    assert rows[0][0] == 0.0 and rows[1][0] == 1.0
    # 1 Mbit/s == 125000 bytes/s (M2 canonical unit conversion)
    assert abs(parse_nyu_csv.__globals__["parse_nyu_csv"](NYU)[0][1] - 0.57e6 / 8) < 1e-9
    ts = [r[0] for r in rows]
    assert ts == sorted(ts)


def test_ugent_units_and_order():
    rows = parse_ugent_log(UGENT)
    assert len(rows) > 100
    ts = [r[0] for r in rows]
    assert ts == sorted(ts)
    # deterministic unit conversion on a synthetic record: 1048576 bytes / 0.5 s
    with tempfile.NamedTemporaryFile("w", suffix=".log", delete=False) as f:
        f.write("1000000000000 0 51.0 3.7 1048576 500\n")
        p = f.name
    try:
        r = parse_ugent_log(p)
        assert len(r) == 1
        assert abs(r[0][1] - 1048576 / 0.5) < 1e-6
        assert r[0][2] == 51.0 and r[0][3] == 3.7
    finally:
        os.unlink(p)


def test_malformed_lines_skipped_deterministically():
    with tempfile.NamedTemporaryFile("w", suffix=".csv", delete=False) as f:
        f.write("1.5,Mbits/sec\n\nnot-a-number,Mbits/sec\n2.5,Mbits/sec\n")
        p = f.name
    try:
        rows = parse_nyu_csv(p)
        assert [r[1] for r in rows] == [1.5e6 / 8, 2.5e6 / 8]
        # elapsed keeps the original 1 Hz line grid (missing lines remain gaps)
        assert [r[0] for r in rows] == [0.0, 3.0]
    finally:
        os.unlink(p)


def test_canonicalize_is_deterministic_and_order_preserving():
    rows = parse_ugent_log(UGENT)[:20]
    a = canonicalize(rows)
    b = canonicalize(parse_ugent_log(UGENT)[:20])
    assert a == b
    assert [x["elapsed_s"] for x in a] == [r[0] for r in rows]


def test_quality_reports_duplicates_and_zeros():
    rows = [(0.0, 100.0), (1.0, 0.0), (1.0, 50.0), (0.9, 10.0)]
    q = quality(rows)
    assert q["samples"] == 4
    assert q["duplicate_timestamps"] == 1
    assert q["non_monotonic"] == 1
    assert q["zero_samples"] == 1


def test_hash_stable_and_content_sensitive():
    h1 = sha256_file(NYU)
    h2 = sha256_file(NYU)
    assert h1 == h2 and len(h1) == 64
    with tempfile.NamedTemporaryFile("w", delete=False) as f:
        f.write("x")
        p = f.name
    try:
        assert sha256_file(p) != h1
    finally:
        os.unlink(p)
