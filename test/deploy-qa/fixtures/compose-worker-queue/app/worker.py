import os

import redis

r = redis.Redis(host=os.environ.get("REDIS_HOST", "redis"), port=6379, decode_responses=True)

print("worker waiting for jobs", flush=True)
while True:
    item = r.brpop("jobs", timeout=5)
    if item is None:
        continue
    _, job = item
    done = r.incr("jobs:done")
    print(f"processed job {job} (total {done})", flush=True)
