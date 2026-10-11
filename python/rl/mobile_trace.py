"""Deterministic parsers/canonicalization for the M2-acquired real mobile traces.

NYU-METS: one ``Mbits/sec`` sample per line at an implicit 1 Hz.
UGent/imec: whitespace columns ``epoch_ms elapsed_ms lat lon bytes interval_ms``;
throughput = bytes / (interval_ms/1000).

No interpolation or smoothing is performed here. Throughput is measured
end-to-end throughput, NOT independent path capacity.
"""

from __future__ import annotations

import hashlib
import math
import statistics as st


def sha256_file(path: str) -> str:
    h = hashlib.sha256()
    with open(path, "rb") as f:
        for chunk in iter(lambda: f.read(1 << 16), b""):
            h.update(chunk)
    return h.hexdigest()


def parse_nyu_csv(path: str) -> list[tuple[float, float]]:
    """Return [(elapsed_s, throughput_bytes_per_second)] with 1 Hz implicit time."""
    rows: list[tuple[float, float]] = []
    for i, line in enumerate(open(path)):
        line = line.strip()
        if not line:
            continue
        tok = line.split(",")[0].strip()
        try:
            mbps = float(tok)
        except ValueError:
            continue
        if not math.isfinite(mbps):
            continue
        rows.append((float(i), mbps * 1e6 / 8.0))
    return rows


def parse_ugent_log(path: str) -> list[tuple[float, float, float, float]]:
    """Return [(elapsed_s, throughput_bps, lat, lon)] preserving original order."""
    rows: list[tuple[float, float, float, float]] = []
    for line in open(path):
        f = line.split()
        if len(f) < 6:
            continue
        try:
            epoch_ms = float(f[0])
            lat = float(f[2])
            lon = float(f[3])
            nbytes = float(f[4])
            itv_ms = float(f[5])
        except ValueError:
            continue
        if itv_ms <= 0:
            continue
        rows.append((epoch_ms / 1000.0, nbytes / (itv_ms / 1000.0), lat, lon))
    return rows


def canonicalize(rows: list[tuple]) -> list[dict]:
    """Canonical form: elapsed_seconds + throughput_bytes_per_second, order preserved."""
    return [
        {"elapsed_s": float(r[0]), "throughput_bytes_per_second": float(r[1])}
        for r in rows
    ]


def quality(rows: list[tuple]) -> dict:
    if not rows:
        return {"samples": 0}
    ts = [r[0] for r in rows]
    thr = [r[1] for r in rows]
    d = [ts[i + 1] - ts[i] for i in range(len(ts) - 1)]
    return {
        "samples": len(rows),
        "duration_s": ts[-1] - ts[0],
        "median_interval_s": st.median(d) if d else None,
        "min_interval_s": min(d) if d else None,
        "max_interval_s": max(d) if d else None,
        "duplicate_timestamps": sum(1 for x in d if x == 0),
        "non_monotonic": sum(1 for x in d if x < 0),
        "zero_samples": sum(1 for v in thr if v <= 0),
        "median_bytes_per_s": st.median(thr),
    }
