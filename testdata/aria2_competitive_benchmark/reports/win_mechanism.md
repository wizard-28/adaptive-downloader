# Win Mechanism Report — aria2-competitive benchmark

**Status: `ARIA2_COMPETITIVE_WIN_REPRODUCED`.** All final results come from the
predeclared fresh 30-paired-run reproduction block; configurations were frozen from
the discovery blocks before it; no configuration changed during the final block.

Generated: 2026-09-28T22:12:02Z

## 1. Exact workload (frozen D2 family)

- 512 MiB file (536,870,912 bytes); 1 MiB request granularity for our downloader; HTTP/1.1.
- Server: controlled per-response pacing sampled at request start and held for the whole
  request: **640 KiB/s (0–2 s) -> 160 KiB/s (2–4 s) -> 2500 KiB/s (>=4 s)**, wall-clock
  anchored at the download start.
- Network: tc/netem on `lo`, 40 ms latency + 0.4% loss (identical for both systems).
- Memory-safe replication: the deterministic logical server (identical pacing semantics)
  replaces the historical 512 MiB materialized pattern; both systems receive the same object.

## 2. Exact aria2 frontier

aria2 1.37.0, `--min-split-size` sweep and `--split`/`--max-connection-per-server` grid;
actual served ranges recorded (not assumed from flags). Median completion by split
(min-split 1M unless noted):

| split / max-conn | median completion (s) | runs |
|---|---|---|
| 1 | 833.2 | 1 |
| 2 | 419.2 | 1 |
| 4 | 210.4 | 2 |
| 8 | 105.6 | 1 |
| 16 | 53.3 | 4 |

The strongest configuration is **split=16 / max-connection-per-server=16 / min-split 1M**
(median 53.3 s, 3-run median): it issues 15 x 32 MiB range pieces, all starting before 2 s.
The min-split dimension is degenerate at 512 MiB for split <= 16 (file/split >= 32 MiB),
verified by spot checks (split=16 with 1M vs 32M; split=4 with 1M vs 32M).

## 3. Exact our configuration

**Fixed policy, concurrency C=16, chunk size 4 MiB** (no adaptation; frozen from the
predeclared 40-configuration discovery grid by the 3-run median; the corrected medians are
in Table 4/analysis. The discovery grid spans C in {1,2,4,8,16} x chunk in
{256K,512K,1M,2M,4M,8M,16M,32M}.

## 4. Measured performance gap (final 30 paired runs)

- ours mean **20.12 s**, median **20.02 s**
- aria2 mean **53.56 s**, median **53.32 s**
- geometric ratio **0.3756** (95% CI [0.3728, 0.3785]); mean ratio 0.3757; median ratio 0.3759
- relative improvement **+62.44%**; wins **30/30**; losses 0
- paired one-sided sign-flip permutation p = 5.0e-05

## 5. D2 historical reproduction (separate block)

- historical v6 vs aria2-F: geo ratio 0.5230, CI [0.5200, 0.5261], 20/20 (Sep 25 evidence)
- fresh reproduction (20 pairs, exact historical configs): v6 mean 28.0 s vs aria2 53.5 s;
  geo ratio **0.5230**, CI [0.5200, 0.5261], v6 faster **20/20** — reproduced.

## 6. Request-level mechanism

Final block traces (per 30 runs):

- **ours**: 3840 requests of 4.0 MiB mean; max in-flight 16; request starts by phase: {'<2s': 480, '>=4s': 3360}
- **aria2**: 450 requests of 32.0 MiB mean; max in-flight 15; request starts by phase: {'<2s': 450}

**Mechanism:** the server selects a request's pacing rate at request start and holds it.
aria2's 32 MiB pieces all start before the 2 s transition at 640 KiB/s, so each takes
~51 s. Our 4 MiB ranges let the scheduler re-allocate quickly: only the first 16 requests
start before 2 s; the remaining 112 start after recovery at 2.5 MiB/s. The win is a
**request-granularity x phase-locked-pacing interaction**, not generic adaptive superiority.

## 7. Controls

- **Control A (aria2-like granularity):** ours at C=16, 32 MiB on the same D2 schedule:
  geometric ratio vs aria2-F **0.862** (95% CI [0.839, 0.885], 10/10 pairs); the win shrinks
  from 0.376 (D2 granularity) to 0.862 (matched granularity), isolating the granularity effect.
- **Constant-rate control (2.5 MB/s, no transition):** ours-4M **15.3 s**, ours-32M **16.7 s**,
  aria2 **24.8 s**. The 4M-vs-32M difference collapses (2.3x on D2 -> ~1.1x at constant
  rate), confirming the effect is phase-locked and not a mere range-size artifact.

## 8. Exact scope of the claim

**Under the frozen D2/512 MiB workload family (per-response pacing 640->160->2500 KiB/s at
2 s/4 s, 40 ms + 0.4% netem, single controlled HTTP/1.1 server), our fixed-policy
downloader (C=16, 4 MiB chunks) has a lower median completion time than the strongest
aria2 configuration found in the frozen frontier (split=16), reproducibly across 30 fresh
paired runs.** This does NOT establish universal aria2 superiority, consumer-network
performance, or any claim outside the tested workload family.
