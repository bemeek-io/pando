import os
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

import redis

r = redis.Redis(host=os.environ.get("REDIS_HOST", "redis"), port=6379, decode_responses=True)


class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        if self.path != "/":
            self.send_error(404)
            return
        job_id = r.incr("jobs:next")
        r.lpush("jobs", f"{job_id}:{time.time()}")
        queued = r.llen("jobs")
        done = r.get("jobs:done") or "0"
        body = (
            "<!doctype html><title>queue</title>"
            "<h1>PANDO-QA compose-worker-queue OK</h1>"
            f"<p>enqueued job {job_id}; queued: {queued}; processed by worker: {done}</p>\n"
        ).encode()
        self.send_response(200)
        self.send_header("Content-Type", "text/html; charset=utf-8")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)


if __name__ == "__main__":
    print("web listening on 8000", flush=True)
    ThreadingHTTPServer(("0.0.0.0", 8000), Handler).serve_forever()
