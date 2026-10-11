# Python RL training

Double DQN training for the adaptive downloader. **Training-only:** the
production inference path is Go (`internal/rl/dqn`) and loads
`exported_model.json`, so Python is not required at inference time.

## Setup (CPU-only PyTorch, no CUDA/nvidia packages)

```bash
python3 -m venv /tmp/dqnvenv
/tmp/dqnvenv/bin/pip install --only-binary=:all: numpy==2.5.3
/tmp/dqnvenv/bin/pip install --index-url https://download.pytorch.org/whl/cpu torch==2.9.0+cpu
```

`numpy` must be installed as a **binary wheel** (`--only-binary=:all:`) because
older numpy releases have no wheel for Python 3.14 and pip would otherwise build
it from source. The CPU-only PyTorch index avoids the ~2 GB CUDA/nvidia bundle.

## Train (sanity)

```bash
/tmp/dqnvenv/bin/python python/train_dqn.py --seed 0 --episodes 300
/tmp/dqnvenv/bin/python -m pytest python/tests -q
```

Artifacts are written to `testdata/diagnostics/dqn/` and are labeled
`DEVELOPMENT / SANITY MODEL`; they are not the final evaluation policy.
