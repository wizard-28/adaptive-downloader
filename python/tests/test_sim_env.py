import os
import sys

sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))

from rl.sim_env import SimConfig, SimEnv


def cfg():
    return SimConfig(file_size=512 * 1024, max_episode_steps=200)


def test_reset_step():
    e = SimEnv(cfg(), seed=1)
    s = e.reset()
    assert len(s) == 13
    r = e.step(1)
    assert r["reward"] < 0
    assert not r["done"]


def test_deterministic():
    def run():
        e = SimEnv(cfg(), seed=42)
        e.reset()
        for i in range(10):
            e.step(i % 5)
        return e.state()

    assert run() == run()


def test_terminates_and_return_negative():
    e = SimEnv(cfg(), seed=7)
    e.reset()
    total, n = 0.0, 0
    while not e.done() and n < 1000:
        total += e.step(0)["reward"]
        n += 1
    assert e.done()
    assert total < 0


def test_truncation():
    c = SimConfig(file_size=100 * 1024 * 1024, max_episode_steps=5)
    e = SimEnv(c, seed=3)
    e.reset()
    for _ in range(5):
        e.step(0)
    assert e.done()


def test_randomized_conditions():
    seen = set()
    for seed in range(20):
        e = SimEnv(cfg(), seed=seed)
        e.reset()
        seen.add(e.concurrency)
    assert len(seen) >= 2
