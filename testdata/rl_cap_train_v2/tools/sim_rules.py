"""Simulator versions of the fixed and rule arms of rl_cap_train_v2 (used for
predictions only).

drop_rule mirrors rl_bench's `-policy drop` (window k = 3, ratio 0.5,
capped target N = 4): the median per-attempt throughput of the last k
attempts, a running maximum of that median, and a one-shot switch when the
median falls below 0.5 x the maximum. Live, the switch sets N = 4 at once;
here the N-only action space lowers N by one per completed attempt, so the
simulated rule reaches N = 4 within about 12 attempts of the trigger.
"""
import math
import os
import sys

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "..", "..", "python"))
from rl.sim_env_cap import run_policy  # noqa: E402


def gm(v):
    return math.exp(sum(math.log(x) for x in v) / len(v))


def hold(state, env):
    return 0


def drop_rule(k=3, ratio=0.5, capped_n=4):
    st = {"samples": [], "base": 0.0, "decided": False}

    def act(state, env):
        b, d, _ = env.recent[-1]
        st["samples"].append(b / d if d > 0 else 0.0)
        if not st["decided"] and len(st["samples"]) >= k:
            s = sorted(st["samples"][-k:])
            med = s[k // 2]
            st["base"] = max(st["base"], med)
            if st["base"] > 0 and med < ratio * st["base"]:
                st["decided"] = True
        if st["decided"] and env.target > capped_n:
            return 2
        return 0
    return act


def oracle(at, m):
    return lambda s, e: 2 if (e.now >= at and e.target > m) else 0


def fixed_s(cond, n, seeds):
    return gm([run_policy(cond, n, hold, seed=k).completion_s() for k in seeds])


def drop_s(cond, seeds):
    return gm([run_policy(cond, 16, drop_rule(), seed=k).completion_s() for k in seeds])


def oracle_best(cond, seeds):
    return min((gm([run_policy(cond, 16, oracle(cond.cap_at, m), seed=k).completion_s() for k in seeds]), m)
               for m in range(2, 16))
