"""Execute the frozen D2 pacing-schedule sweep (runs sequentially, in manifest order).

Refuses to run if the manifest hash differs from manifest/sweep_manifest.sha256.
Skips runs whose result file already exists, so it can be resumed after an
interruption without altering the order or re-running completed cells.
"""
import hashlib, json, os, subprocess, sys

BASE = os.path.join(os.path.dirname(os.path.abspath(__file__)), "..")
REPO = os.path.join(BASE, "..", "..")
man_path = os.path.join(BASE, "manifest", "sweep_manifest.json")
want = open(os.path.join(BASE, "manifest", "sweep_manifest.sha256")).read().split()[0]
got = hashlib.sha256(open(man_path, "rb").read()).hexdigest()
if want != got:
    sys.exit(f"manifest hash mismatch: {got} != {want}")
m = json.load(open(man_path))
binsha = hashlib.sha256(open(os.path.join(BASE, "tools", "sweep_run"), "rb").read()).hexdigest()
if binsha != m["tool"]["binary_sha256"]:
    sys.exit("sweep_run binary hash differs from manifest")
os.makedirs(os.path.join(BASE, "runs"), exist_ok=True)
for o in m["order"]:
    out = os.path.join(BASE, "runs", o["run_id"] + ".json")
    if os.path.exists(out):
        continue
    cmd = [os.path.join(BASE, "tools", "sweep_run"), "-system", o["system"], "-out", out,
           "-run-id", o["run_id"], "-file-size", str(m["workload"]["file_size_bytes"]),
           "-phases", m["schedules_kib_per_s"][o["schedule"]]]
    if o["system"] == "ours":
        s = m["systems"]["ours"]
        cmd += ["-c", str(s["concurrency"]), "-chunk", str(s["chunk_bytes"])]
    else:
        cmd += ["-aria2-args", m["systems"]["aria2"]["args"]]
    r = subprocess.run(cmd, cwd=REPO, capture_output=True, text=True)
    line = {"seq": o["seq"], "run_id": o["run_id"], "rc": r.returncode}
    if os.path.exists(out):
        d = json.load(open(out))
        line.update(completion_ms=d.get("completion_ms"), verified=d.get("content_verified"), err=d.get("err"))
    else:
        line["err"] = (r.stderr or r.stdout)[-300:]
    with open(os.path.join(BASE, "logs", "index.jsonl"), "a") as f:
        f.write(json.dumps(line) + "\n")
    print(line, flush=True)
print("sweep complete")
