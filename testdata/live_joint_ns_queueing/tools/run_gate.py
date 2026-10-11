#!/usr/bin/env python3
"""Live mechanism-gate driver: fixed-N sweep under predeclared contention states.

Design (frozen in manifest/gate_design.json):
  bottleneck   tc tbf on lo, rate 40 Mbit/s, burst 64 kb, limit 256 kb
  background   iperf3 -c 127.0.0.1 -P 4 -b B  (B = 0 / 0.5C / 0.85C)
  download     32 MiB, S = 1 MiB, N in {1,2,4,8,16}, 3 reps per cell
  order        round-robin over N within each state; states S0, S1, S2

Every run preserves: the downloader result JSON, driver metadata (ping RTTs,
qdisc backlog/drops, nstat retrans/timeouts, iperf3 interval rates, load).
"""
import argparse
import json
import os
import re
import signal
import statistics
import subprocess
import sys
import time

BASE = "/workspaces/mvp/testdata/live_joint_ns_queueing"
RUNS = os.path.join(BASE, "mechanism_gate", "runs")
META = os.path.join(BASE, "mechanism_gate", "meta")
LOGS = os.path.join(BASE, "logs")
for d in (RUNS, META, LOGS):
    os.makedirs(d, exist_ok=True)

C_BPS = 5_000_000
STATES = [
    {"id": "S0_no_background", "offered_bps": 0},
    {"id": "S1_moderate", "offered_bps": 2_500_000},
    {"id": "S2_heavy", "offered_bps": 4_250_000},
]
N_GRID = [1, 2, 4, 8, 16]
CHUNK = 1 << 20
FILE_SIZE = 32 << 20
REPS = 3
IPERF_PORT = 5201


def sh(cmd, timeout=60, **kw):
    return subprocess.run(cmd, shell=isinstance(cmd, str), capture_output=True,
                          text=True, timeout=timeout, **kw)


def apply_tbf():
    r = sh(["sudo", "-n", "tc", "qdisc", "replace", "dev", "lo", "root", "tbf",
            "rate", "40mbit", "burst", "32kb", "limit", "256kb"])
    if r.returncode != 0:
        raise RuntimeError("tbf apply failed: " + r.stderr.strip())
    return r


def clear_qdisc():
    sh(["sudo", "-n", "tc", "qdisc", "del", "dev", "lo", "root"])


def qdisc_stats():
    r = sh(["sudo", "-n", "tc", "-s", "qdisc", "show", "dev", "lo"])
    txt = r.stdout
    out = {"raw": txt}
    m = re.search(r"Sent (\d+) bytes .*?dropped (\d+), overlimits (\d+)", txt, re.S)
    if m:
        out["sent_bytes"] = int(m.group(1))
        out["dropped"] = int(m.group(2))
        out["overlimits"] = int(m.group(3))
    m = re.search(r"backlog (\d+)p (\d+)b", txt)
    if m:
        out["backlog_pkts"] = int(m.group(1))
        out["backlog_bytes"] = int(m.group(2))
    return out


def nstat():
    r = sh(["nstat", "-az", "TcpRetransSegs", "TcpExtTCPTimeouts", "TcpExtTCPLostRetransmit",
            "TcpOutSegs", "IpOutRequests"])
    out = {}
    for line in r.stdout.splitlines():
        parts = line.split()
        if len(parts) >= 2 and parts[0] != "#":
            try:
                out[parts[0]] = int(parts[1])
            except ValueError:
                pass
    return out


def start_iperf(offered_bps, duration_s):
    """Start a fresh iperf3 server + client for one cell. Returns (client, server).

    A stale iperf3 server can reset a new client's control connection; each
    cell gets a fresh server and the client is retried up to 3 times."""
    if offered_bps <= 0:
        return None, None
    per_stream_bits = max(1, offered_bps * 8 // 4)  # design B is bytes/s; iperf3 -b is bits/s per stream

    def new_server():
        srv = subprocess.Popen(["iperf3", "-s", "-p", str(IPERF_PORT)],
                               stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        time.sleep(0.8)
        return srv

    srv = new_server()
    for attempt in range(3):
        cmd = ["stdbuf", "-oL", "iperf3", "-c", "127.0.0.1", "-p", str(IPERF_PORT),
               "--connect-timeout", "3000", "-P", "4", "-b", "%d" % per_stream_bits,
               "-t", "%d" % int(duration_s + 8)]
        p = subprocess.Popen(cmd, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True)
        time.sleep(1.5)
        if p.poll() is None:
            return p, srv
        try:
            p.communicate(timeout=3)
        except Exception:
            p.kill()
        srv.kill()
        try:
            srv.wait(timeout=3)
        except Exception:
            pass
        srv = new_server()
    return None, srv


def stop_proc(p, name, logpath):
    if p is None:
        return ""
    try:
        p.send_signal(signal.SIGINT)
        out, _ = p.communicate(timeout=6)
    except Exception:
        p.kill()
        out, _ = p.communicate()
    if logpath:
        open(logpath, "w").write(out or "")
    return out or ""


def parse_iperf_intervals(out):
    """Aggregate [SUM] interval rates only (bits/s); iperf3 -b is per stream."""
    sum_rates, stream_rates = [], []
    for line in (out or "").splitlines():
        m = re.search(r"\[(SUM|\s*\d+)\]\s+\d+\.\d+-\d+\.\d+\s+sec\s+[\d.]+\s+\w+\s+([\d.]+)\s+(\w?bits)/sec", line)
        if not m:
            continue
        v = float(m.group(2))
        unit = m.group(3)
        if unit.startswith("K"):
            v *= 1e3
        elif unit.startswith("M"):
            v *= 1e6
        elif unit.startswith("G"):
            v *= 1e9
        if m.group(1) == "SUM":
            sum_rates.append(v)
        else:
            stream_rates.append(v)
    if sum_rates:
        return sum_rates
    return [sum(stream_rates[i:i + 4]) for i in range(0, len(stream_rates), 4)] if stream_rates else []


def start_ping():
    p = subprocess.Popen(["ping", "-i", "0.2", "-W", "2", "127.0.0.1"],
                         stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True)
    return p


def parse_ping(out):
    rtts, lost = [], 0
    for line in (out or "").splitlines():
        m = re.search(r"time=([\d.]+) ms", line)
        if m:
            rtts.append(float(m.group(1)))
        elif "no answer yet" in line or "100% packet loss" in line:
            lost += 1
    return rtts, lost


def background_only(st, duration=10):
    """Measure the achieved background load under the tbf bottleneck with no downloader."""
    if st["offered_bps"] <= 0:
        out = {"offered_bps": 0, "offered_bits_per_second": 0, "achieved_bits_per_second": 0.0, "rel_err": 0.0}
        json.dump(out, open(os.path.join(BASE, "mechanism_gate",
                                         "background_only_%s.json" % st["id"]), "w"), indent=2)
        return out
    r = sh(["iperf3", "-c", "127.0.0.1", "-p", str(IPERF_PORT), "-P", "4",
            "-b", str(max(1, st["offered_bps"] * 8 // 4)), "-t", str(duration), "--json"],
           timeout=duration + 30)
    try:
        j = json.loads(r.stdout)
        achieved = j.get("end", {}).get("sum_received", {}).get("bits_per_second", 0.0)
    except Exception:
        achieved = 0.0
    out = {"offered_bps": st["offered_bps"], "offered_bits_per_second": st["offered_bps"] * 8,
           "achieved_bits_per_second": achieved,
           "rel_err": (achieved - st["offered_bps"] * 8) / (st["offered_bps"] * 8) if st["offered_bps"] else 0.0}
    json.dump(out, open(os.path.join(BASE, "mechanism_gate",
                                     "background_only_%s.json" % st["id"]), "w"), indent=2)
    return out


def run_cell(state, n, rep, run_id):
    log_base = os.path.join(LOGS, run_id)
    meta = {"run_id": run_id, "state": state["id"], "offered_bps": state["offered_bps"],
            "n": n, "rep": rep, "chunk": CHUNK, "file_size": FILE_SIZE}
    meta["load_before"] = open("/proc/loadavg").read().strip()
    meta["qdisc_before"] = qdisc_stats()
    meta["nstat_before"] = nstat()

    iperf, iperf_srv = start_iperf(state["offered_bps"], 90)
    ping = start_ping()
    t0 = time.time()
    r = sh([os.path.join(BASE, "tools", "gate_run"),
            "-n", str(n), "-chunk", str(CHUNK), "-file-size", str(FILE_SIZE),
            "-run-id", run_id, "-out", os.path.join(RUNS, run_id + ".json")],
           timeout=300)
    wall = time.time() - t0
    meta["wall_s"] = wall
    meta["gate_run_rc"] = r.returncode
    meta["gate_run_stdout"] = r.stdout.strip()
    meta["gate_run_stderr"] = r.stderr.strip()

    time.sleep(0.8)  # queue drain before stopping the background
    ping_out = stop_proc(ping, "ping", log_base + ".ping.txt")
    iperf_out = stop_proc(iperf, "iperf3", log_base + ".iperf.txt")
    if iperf_srv is not None:
        iperf_srv.kill()
        try:
            iperf_srv.wait(timeout=3)
        except Exception:
            pass
    rtts, lost = parse_ping(ping_out)
    meta["ping_rtts_ms"] = rtts
    meta["ping_median_ms"] = statistics.median(rtts) if rtts else None
    meta["ping_p95_ms"] = (sorted(rtts)[int(0.95 * (len(rtts) - 1))] if rtts else None)
    meta["ping_max_ms"] = max(rtts) if rtts else None
    meta["ping_samples"] = len(rtts)
    rates = parse_iperf_intervals(iperf_out)
    meta["iperf_interval_bps"] = rates
    meta["iperf_mean_bps"] = statistics.mean(rates) if rates else None
    # the state condition is only valid if the background traffic was actually flowing
    meta["background_ok"] = (state["offered_bps"] == 0) or (rates and statistics.mean(rates) > 0.1 * state["offered_bps"] * 8)
    meta["qdisc_after"] = qdisc_stats()
    meta["nstat_after"] = nstat()
    meta["load_after"] = open("/proc/loadavg").read().strip()
    d = {}
    for k in meta["nstat_after"]:
        if k in meta["nstat_before"]:
            d[k] = meta["nstat_after"][k] - meta["nstat_before"][k]
    meta["nstat_delta"] = d
    if meta["qdisc_before"].get("dropped") is not None and meta["qdisc_after"].get("dropped") is not None:
        meta["qdisc_drops"] = meta["qdisc_after"]["dropped"] - meta["qdisc_before"]["dropped"]
    else:
        meta["qdisc_drops"] = None
    json.dump(meta, open(os.path.join(META, run_id + ".json"), "w"), indent=2)
    with open(os.path.join(BASE, "mechanism_gate", "index.jsonl"), "a") as f:
        f.write(json.dumps({k: meta[k] for k in
                            ("run_id", "state", "n", "rep", "gate_run_rc", "wall_s",
                             "ping_median_ms", "ping_p95_ms", "iperf_mean_bps",
                             "qdisc_drops", "nstat_delta")}) + "\n")
    return meta


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--smoke", action="store_true", help="infrastructure check only (1 rep, N=1,4, S0/S1)")
    ap.add_argument("--states", default="", help="comma-separated state ids to run (default: all)")
    args = ap.parse_args()
    if args.smoke:
        states = STATES[:2]
        grid = [1, 4]
        reps = 1
        tag = "smoke"
    else:
        states = STATES
        grid = N_GRID
        reps = REPS
        tag = "gate"
        if args.states:
            want = set(args.states.split(","))
            states = [st for st in STATES if st["id"] in want]

    # link emulation: realistic MTU (temporary; restored in finally)
    mtu_out = sh(["ip", "link", "show", "lo"]).stdout
    m = re.search(r"mtu (\d+)", mtu_out)
    original_mtu = m.group(1) if m else "65536"
    sh(["sudo", "-n", "ip", "link", "set", "lo", "mtu", "1500"])
    # iperf3 server
    srv = subprocess.Popen(["iperf3", "-s", "-p", str(IPERF_PORT)],
                           stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    time.sleep(1.0)
    try:
        apply_tbf()
        for st in states:
            print("=== state %s (offered %d bps)" % (st["id"], st["offered_bps"]), flush=True)
            bo = background_only(st)
            print("  background-only achieved %d bits/s (offered %d bits/s, rel %+.1f%%)" % (
                bo["achieved_bits_per_second"], bo["offered_bits_per_second"], 100 * bo["rel_err"]), flush=True)
            for rep in range(1, reps + 1):
                for n in grid:
                    run_id = "%s_%s_n%d_r%d" % (tag, st["id"], n, rep)
                    m = None
                    for attempt in range(3):
                        rid = run_id if attempt == 0 else "%s_retry%d" % (run_id, attempt)
                        try:
                            m = run_cell(st, n, rep, rid)
                        except Exception as e:
                            print("  %s FAILED: %s" % (rid, e), flush=True)
                            m = None
                        if m and m.get("background_ok"):
                            break
                        print("  %s background not verified (attempt %d)" % (rid, attempt + 1), flush=True)
                    if m:
                        print("  %s comp=%s ping_med=%s p95=%s drops=%s retrans=%s bg_ok=%s" % (
                            m["run_id"],
                            (m.get("gate_run_stdout") or "").split("completion=")[-1].split()[0]
                            if "completion=" in (m.get("gate_run_stdout") or "") else "?",
                            m["ping_median_ms"], m["ping_p95_ms"], m["qdisc_drops"],
                            m["nstat_delta"].get("TcpRetransSegs"), m.get("background_ok")), flush=True)
    finally:
        clear_qdisc()
        sh(["sudo", "-n", "ip", "link", "set", "lo", "mtu", original_mtu])
        srv.send_signal(signal.SIGINT)
        try:
            srv.wait(timeout=5)
        except Exception:
            srv.kill()
    print("driver done", flush=True)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
