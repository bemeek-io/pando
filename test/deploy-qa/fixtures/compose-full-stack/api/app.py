import json
import os
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

import psycopg

DSN = os.environ["DATABASE_URL"]


class Handler(BaseHTTPRequestHandler):
    def _json(self, code, payload):
        body = json.dumps(payload, separators=(",", ":")).encode()
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        if self.path.rstrip("/") == "/api/health":
            try:
                with psycopg.connect(DSN, connect_timeout=3) as conn:
                    one = conn.execute("SELECT 1").fetchone()[0]
                self._json(200, {"status": "ok", "db": "ok" if one == 1 else "unexpected"})
            except Exception as exc:  # report, don't crash
                self._json(503, {"status": "degraded", "db": "error", "detail": str(exc)})
            return
        if self.path.rstrip("/") == "/api/time":
            with psycopg.connect(DSN, connect_timeout=3) as conn:
                now = conn.execute("SELECT now()::text").fetchone()[0]
            self._json(200, {"now": now})
            return
        self._json(404, {"error": "not found"})


if __name__ == "__main__":
    print("api listening on 8000", flush=True)
    ThreadingHTTPServer(("0.0.0.0", 8000), Handler).serve_forever()
