#!/usr/bin/env python3
"""Amendment A2 link gate: exit 0 only if each server's 1-byte range probe and
a google.com fetch respond within 3 s, and an 8 MB single-stream fetch from
kcdn reaches 1.5 MB/s. Prints each measurement.

Usage: python3 testdata/live_mirror_check/tools/link_gate.py   (from the repository root)
"""
import json, subprocess, sys

M = json.load(open("testdata/live_mirror_check/manifest.json"))
UA = M["user_agent"]


def curl(url, rng, timeout):
    p = subprocess.run(["curl", "-s", "-o", "/dev/null", "-A", UA, "-r", rng, "--max-time", str(timeout),
                        "-w", "%{http_code} %{time_total} %{speed_download}", url],
                       capture_output=True, text=True)
    code, t, speed = p.stdout.split()
    return code, float(t), float(speed)


ok = True
for s in M["servers"]:
    code, t, _ = curl(s["url"], "0-0", 10)
    good = code == "206" and t <= 3
    ok &= good
    print(f"{s['id']}: {code} in {t:.2f} s {'ok' if good else 'FAIL'}")
code, t, _ = curl("https://www.google.com/", "0-0", 10)
good = code in ("200", "206") and t <= 3
ok &= good
print(f"google: {code} in {t:.2f} s {'ok' if good else 'FAIL'}")
kcdn = next(s["url"] for s in M["servers"] if s["id"] == "kcdn")
code, t, speed = curl(kcdn, "0-8388607", 60)
good = code == "206" and speed >= 1.5e6
ok &= good
print(f"kcdn 8 MB: {speed / 1e6:.2f} MB/s {'ok' if good else 'FAIL'}")
print("GATE", "PASS" if ok else "FAIL")
sys.exit(0 if ok else 1)
