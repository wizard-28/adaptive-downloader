"""Execute the frozen NGINX check. Starts a private nginx instance from
conf/nginx.conf.tmpl, runs the manifest order, stops nginx. Hash-checked and
resumable. Usage: run_nginx.py WORKDIR   (WORKDIR holds www/big.bin; see manifest)."""
import hashlib, json, os, subprocess, sys, time
BASE = os.path.join(os.path.dirname(os.path.abspath(__file__)), "..")
REPO = os.path.join(BASE, "..", "..")
W = os.path.abspath(sys.argv[1])
m_path = os.path.join(BASE, "manifest", "nginx_manifest.json")
want = open(os.path.join(BASE, "manifest", "nginx_manifest.sha256")).read().split()[0]
if hashlib.sha256(open(m_path, "rb").read()).hexdigest() != want:
    sys.exit("manifest hash mismatch")
m = json.load(open(m_path))
tool = os.path.join(BASE, "tools", "nginx_run")
if hashlib.sha256(open(tool, "rb").read()).hexdigest() != m["tool"]["binary_sha256"]:
    sys.exit("nginx_run binary differs from manifest")
big = os.path.join(W, "www", "big.bin")
h = hashlib.sha256()
with open(big, "rb") as f:
    for b in iter(lambda: f.read(1 << 24), b""):
        h.update(b)
if h.hexdigest() != m["object"]["sha256"]:
    sys.exit("object hash mismatch")
for d in ("logs", "tmp"):
    os.makedirs(os.path.join(W, d), exist_ok=True)
tmpl = open(os.path.join(BASE, "conf", "nginx.conf.tmpl")).read().replace("@@DIR@@", W)
open(os.path.join(W, "nginx.conf"), "w").write(tmpl)
subprocess.run(["nginx", "-c", os.path.join(W, "nginx.conf"), "-p", W], check=True)
time.sleep(1)
try:
    os.makedirs(os.path.join(BASE, "runs"), exist_ok=True)
    for o in m["order"]:
        out = os.path.join(BASE, "runs", o["run_id"] + ".json")
        if os.path.exists(out):
            continue
        url = "http://127.0.0.1:18080" + m["configs"][o["config"]]
        cmd = [tool, "-system", o["system"], "-url", url, "-file-size", str(m["object"]["size_bytes"]),
               "-expect-sha", m["object"]["sha256"], "-out", out, "-run-id", o["run_id"]]
        if o["system"] == "ours":
            s = m["systems"]["ours"]
            cmd += ["-c", str(s["concurrency"]), "-chunk", str(s["chunk_bytes"])]
        else:
            cmd += ["-aria2-args", m["systems"]["aria2"]["args"]]
        r = subprocess.run(cmd, cwd=REPO, capture_output=True, text=True)
        line = {"seq": o["seq"], "run_id": o["run_id"], "rc": r.returncode}
        if os.path.exists(out):
            d = json.load(open(out)); line.update(completion_ms=d.get("completion_ms"), verified=d.get("content_verified"), err=d.get("err"))
        with open(os.path.join(BASE, "logs", "index.jsonl"), "a") as f:
            f.write(json.dumps(line) + "\n")
        print(line, flush=True)
finally:
    subprocess.run(["nginx", "-c", os.path.join(W, "nginx.conf"), "-p", W, "-s", "stop"])
print("nginx check complete")
