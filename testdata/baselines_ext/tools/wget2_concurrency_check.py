#!/usr/bin/env python3
"""Throttled range server (~2 MiB/s per response, 16 MiB) that logs requests in
flight; used to check wget2 chunk concurrency outside the harness (protocol A1).
Usage: python3 wget2_concurrency_check.py   then point wget2 at http://127.0.0.1:18099/f.bin"""
import http.server, socketserver, threading, time, sys
SIZE = 16 << 20
active = 0; peak = 0; lock = threading.Lock()
class H(http.server.BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"
    def log_message(self, *a): pass
    def head(self, code, start, end):
        self.send_response(code)
        self.send_header("Accept-Ranges", "bytes")
        self.send_header("Content-Length", str(end - start + 1))
        if code == 206:
            self.send_header("Content-Range", f"bytes {start}-{end}/{SIZE}")
        self.end_headers()
    def do_HEAD(self):
        self.head(200, 0, SIZE - 1)
        print("HEAD", flush=True)
    def do_GET(self):
        global active, peak
        rng = self.headers.get("Range")
        start, end = 0, SIZE - 1
        if rng:
            a, b = rng.split("=")[1].split("-")
            start, end = int(a), int(b) if b else SIZE - 1
        with lock:
            active += 1; peak = max(peak, active); now = active
        print(f"GET {rng} active={now} conn={self.client_address[1]}", flush=True)
        self.head(206 if rng else 200, start, end)
        n = end - start + 1; sent = 0
        while sent < n:                      # ~2 MiB/s per response
            k = min(65536, n - sent)
            self.wfile.write(b"\0" * k); sent += k; time.sleep(k / (2 << 20))
        with lock:
            active -= 1
class S(socketserver.ThreadingMixIn, http.server.HTTPServer):
    daemon_threads = True
S(("127.0.0.1", 18099), H).serve_forever()
