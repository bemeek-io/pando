import os
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

import redis

r = redis.Redis(host=os.environ.get("REDIS_HOST", "redis"), port=6379)


class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        if self.path != "/":
            self.send_error(404)
            return
        hits = r.incr("hits")
        body = f"<!doctype html><title>counter</title><h1>PANDO-QA compose-web-redis OK</h1><p>hits: {hits}</p>\n".encode()
        self.send_response(200)
        self.send_header("Content-Type", "text/html; charset=utf-8")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)


if __name__ == "__main__":
    print("listening on 8000", flush=True)
    ThreadingHTTPServer(("0.0.0.0", 8000), Handler).serve_forever()
