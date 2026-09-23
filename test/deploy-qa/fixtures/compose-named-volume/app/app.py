import os
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

DATA_DIR = os.environ.get("DATA_DIR", "/data")
COUNTER = os.path.join(DATA_DIR, "count.txt")
lock = threading.Lock()


def bump():
    with lock:
        try:
            with open(COUNTER) as f:
                n = int(f.read().strip() or "0")
        except FileNotFoundError:
            n = 0
        n += 1
        tmp = COUNTER + ".tmp"
        with open(tmp, "w") as f:
            f.write(str(n))
        os.replace(tmp, COUNTER)
        return n


class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        if self.path != "/":
            self.send_error(404)
            return
        n = bump()
        body = f"<!doctype html><title>visits</title><h1>PANDO-QA compose-named-volume OK</h1><p>visits: {n}</p>\n".encode()
        self.send_response(200)
        self.send_header("Content-Type", "text/html; charset=utf-8")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)


if __name__ == "__main__":
    os.makedirs(DATA_DIR, exist_ok=True)
    print(f"listening on 8000, counter at {COUNTER}", flush=True)
    ThreadingHTTPServer(("0.0.0.0", 8000), Handler).serve_forever()
