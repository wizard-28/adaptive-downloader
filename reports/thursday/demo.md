# Viva demo script

Run everything from the repository root. Each step lists the exact command,
what to point out, and a fallback. Build once beforehand (step 0), so no
compile time is spent in the viva.

## 0. Before the viva (not shown)

```bash
go build -o /tmp/dl ./cmd/downloader
go build -o /tmp/rl_bench ./testdata/rl_capped/tools/
sudo -n tc qdisc show dev lo          # must show "noqueue": no shaping left over
```

## 1. Live download from a public mirror (about 30 s)

```bash
/tmp/dl -url https://cdn.kernel.org/pub/linux/kernel/v3.x/linux-3.0.tar.xz \
  -out /tmp/linux-3.0.tar.xz -concurrency 8 -chunk 1048576 \
  -sha256 ef5e8c2dedb20a6eab01bb379e9982cd7d47cf95bd87c9ee3e872272e85c24b6
```

Point out:
- `-size` is omitted, so the CLI probes the size with `Range: bytes=0-0` and
  requires a 206 response.
- 8 × 1 MiB is the setting that was within 3.7 % of the best per-condition
  choice in the live pilot.
- The SHA-256 is checked against kernel.org's published checksum; a mismatch
  fails the download.

Fallback (no internet): skip to step 2, which brings its own server.

## 2. Benchmark run on the capped server, with the retrained agent (about 70 s)

```bash
/tmp/rl_bench -system ours -n 16 -chunk 1048576 -no-request-timeout -split \
  -policy dqn6 -dqn-model testdata/rl_cap_train/model/dqn_cap_candidate.json -window 16 \
  -schedule t1 -cap-c 8388608 -cap-gamma 0.05 -cap-at 8 \
  -netem-latency-ms 40 -netem-loss 0.4 -run-id demo -out /tmp/demo.json
python3 -c "import json;r=json.load(open('/tmp/demo.json'));d=r['decisions'];print('time',r['completion_ms']/1000,'verified',r['content_verified'],'final N',r['final_n']);print([ (round(e['time_ms']/1000,1),e['concurrency']) for e in d[::40]])"
```

Point out:
- The server switches its concurrency cap on at 8 s. The agent sees request
  times jump and lowers N, settling around 7.
- In Stage C, over 10 blocks, this was 0.907 of the time of fixed
  16 × 1 MiB, but slower than 16 × 32 MiB and aria2. Explain why: large ranges
  started before the cap keep the faster rate.
- Netem is applied and removed by the harness. Show
  `sudo -n tc qdisc show dev lo` afterwards.

Shorter fallback (about 18 s): drop `-policy`, `-dqn-model`, `-window` and
the `-cap-*` flags to run fixed 16 × 1 MiB on the uncapped server.

## 3. Tests and the number checker (about 2 min)

```bash
go test -count=1 ./internal/... 2>&1 | tail -15   # -count=1: run for real, not cached
python3 tools/claims_check.py | tail -2   # 183 numbers recomputed from raw runs
```

Point out:
- No test needs internet; each starts its own local server.
- The checker recomputes every number in the paper from the raw run files
  and fails on any it cannot reproduce. Its self-test
  (`python3 tools/test_claims_check.py`) shows it rejects deliberately wrong
  numbers.

## Timing

Measured on 2026-10-25 (devcontainer, home broadband):

- **Step 1:** 29.7 s (63.8 MB at 2.15 MB/s; size probe and SHA-256 OK).
- **Step 2:** 70 s wall time; the download took 69.2 s, content was
  verified, and the final N was 6. N held at 14–16 before the cap and fell
  to 6 by 15.8 s, after the cap at 8 s.
- **Step 3:** about 30 s for the uncached internal tests (18 packages OK),
  plus 8 s for the checker (183/183, 0 unverified).
- **Total:** about 2.5 min plus talking. Afterwards `sudo -n tc qdisc show
  dev lo` showed `noqueue` (clean).
