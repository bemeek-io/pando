import time

print("worker started", flush=True)
n = 0
while True:
    n += 1
    print(f"worker heartbeat {n}", flush=True)
    time.sleep(30)
