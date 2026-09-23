"""Live view of a deploy-QA run: `python3 test/deploy-qa/qa.py dashboard [--pass ai]`.

Reads out/run-<pass>.log and out/results-<pass>.jsonl, so it never touches the
run itself. Ctrl-C to quit.
"""
import json
import os
import re
import shutil
import subprocess
import sys
import time
from collections import defaultdict
from datetime import datetime

from . import state

LABEL = "noai"
LOG = state.log_file(LABEL)
RES = state.results_file(LABEL)
TOTAL_FILES = state.CASE_FILES

G, R, Y, D, B, X = "\033[32m", "\033[31m", "\033[33m", "\033[2m", "\033[1m", "\033[0m"


def total_cases():
    # The run logs "N cases to run (M already done)"; its last such line is the real target.
    try:
        hits = [re.search(r"(\d+) cases to run \((\d+) already done\)", l) for l in open(LOG)]
        hits = [h for h in hits if h]
        if hits:
            return int(hits[-1].group(1)) + int(hits[-1].group(2))
    except FileNotFoundError:
        pass
    n = 0
    for f in TOTAL_FILES:
        try:
            n += len(json.load(open(f)))
        except Exception:
            pass
    return n


def results():
    rows = {}
    try:
        for line in open(RES):
            try:
                r = json.loads(line)
                rows[r["id"]] = r
            except Exception:
                pass
    except FileNotFoundError:
        pass
    return rows


def log_state():
    started, done, events = {}, set(), []
    today = datetime.now().strftime("%Y-%m-%d ")
    try:
        for line in open(LOG):
            m = re.match(r"(\d\d:\d\d:\d\d) (start|done)\s+(\S+)", line)
            if m:
                t, kind, cid = m.groups()
                if kind == "start":
                    started[cid] = datetime.strptime(today + t, "%Y-%m-%d %H:%M:%S")
                    done.discard(cid)
                else:
                    done.add(cid)
            elif " requeue " in line:
                rq = line.split(" requeue ")[1].split()[0]
                done.add(rq)
            elif "watchdog" in line or "disk gate" in line or "RESIDUE" in line or line.startswith("=== ") or "all done" in line or "stopped:" in line:
                events.append(line.rstrip())
    except FileNotFoundError:
        pass
    inflight = {c: t for c, t in started.items() if c not in done}
    return inflight, events[-4:]


def cpu_pct():
    try:
        out = subprocess.run(["top", "-l", "2", "-n", "0", "-s", "1"], capture_output=True, text=True, timeout=5).stdout
        m = re.findall(r"CPU usage: ([\d.]+)% user, ([\d.]+)% sys", out)
        u, s = m[-1]
        return float(u) + float(s)
    except Exception:
        return None


def mem():
    try:
        total = int(subprocess.run(["sysctl", "-n", "hw.memsize"], capture_output=True, text=True).stdout)
        vm = subprocess.run(["vm_stat"], capture_output=True, text=True).stdout
        page = int(re.search(r"page size of (\d+)", vm).group(1))
        get = lambda k: int(re.search(rf"{k}:\s+(\d+)", vm).group(1)) * page
        used = get("Pages active") + get("Pages wired down") + get("Pages occupied by compressor")
        return used / 1e9, total / 1e9
    except Exception:
        return None, None


def docker_counts():
    try:
        ps = subprocess.run(["docker", "ps", "-q"], capture_output=True, text=True, timeout=5).stdout.split()
        nets = subprocess.run(["docker", "network", "ls", "-q"], capture_output=True, text=True, timeout=5).stdout.split()
        return len(ps), len(nets)
    except Exception:
        return None, None


def bar(frac, width=30):
    n = int(round(frac * width))
    return "█" * n + "░" * (width - n)


def ok(r):
    return r.get("outcome", "").startswith("PASS")


def render():
    rows = results()
    inflight, events = log_state()
    total = total_cases()
    n = len(rows)
    passed = sum(ok(r) for r in rows.values())
    residue = [r for r in rows.values() if r.get("residue")]
    cpu = cpu_pct()
    mu, mt = mem()
    du = shutil.disk_usage("/")
    running, nets = docker_counts()

    out = []
    out.append(f"{B}Pando QA — pass: {LABEL}{X}   {datetime.now():%H:%M:%S}")
    out.append("")
    frac = n / total if total else 0
    out.append(f"progress   {bar(frac)} {n}/{total} done  {B}{len(inflight)}{X} in flight  {max(total - n - len(inflight), 0)} queued")
    rate = f"{100 * passed / n:.1f}%" if n else "—"
    try:
        stamps = [re.match(r"(\d\d:\d\d:\d\d) done", l) for l in open(LOG)]
        stamps = [datetime.strptime(m.group(1), "%H:%M:%S") for m in stamps if m][-15:]
        per_min = (len(stamps) - 1) / max((stamps[-1] - stamps[0]).total_seconds() / 60, 0.1) if len(stamps) > 2 else 0
    except Exception:
        per_min = 0
    left = max(total - n, 0)
    eta = f"{left / per_min:.0f} min at {per_min:.1f}/min" if per_min and left else ("done" if not left else "estimating…")
    out.append(f"eta        {eta}")
    out.append(f"success    {G}{passed} passed{X}  {R}{n - passed} failed{X}  → {B}{rate}{X}")
    by = defaultdict(lambda: [0, 0])
    for r in rows.values():
        by[r.get("source") or "?"][0] += ok(r)
        by[r.get("source") or "?"][1] += 1
    out.append("by source  " + "   ".join(f"{k} {v[0]}/{v[1]}" for k, v in sorted(by.items())))
    outc = defaultdict(int)
    for r in rows.values():
        outc[r.get("outcome")] += 1
    out.append(D + "outcomes   " + "  ".join(f"{k}:{v}" for k, v in sorted(outc.items(), key=lambda kv: -kv[1])) + X)
    out.append("")
    cl = f"{G}clean — 0 residue{X}" if not residue else f"{R}RESIDUE on {len(residue)} app(s): {', '.join(r['id'] for r in residue[:5])}{X}"
    out.append(f"cleanup    {cl}")
    free = du.free / 1e9
    col = G if free > 60 else (Y if free > 40 else R)
    out.append(f"disk       {col}{free:.0f} GB free{X} of {du.total / 1e9:.0f} GB   (new cases hold below {state.load().get('min_free_gb', 40)} GB)")
    out.append(f"cpu        {cpu:.0f}%" if cpu is not None else "cpu        ?")
    out.append(f"memory     {mu:.1f} / {mt:.0f} GB used" if mu else "memory     ?")
    out.append(f"docker     {running} containers running, {nets} networks" if running is not None else "docker     not responding")
    out.append("")
    out.append(f"{B}in flight{X}")
    now = datetime.now()
    for cid, t in sorted(inflight.items(), key=lambda kv: kv[1]):
        secs = int((now - t).total_seconds())
        out.append(f"  {cid:36} {secs // 60:2d}m{secs % 60:02d}s")
    if not inflight:
        out.append(D + "  (none)" + X)
    out.append("")
    out.append(f"{B}recent{X}")
    recent = sorted(rows.values(), key=lambda r: r.get("_t", 0))
    try:
        order = [re.match(r"\S+ done\s+(\S+)", l).group(1) for l in open(LOG) if " done " in l]
        recent = [rows[i] for i in order[-8:] if i in rows]
    except Exception:
        recent = recent[-8:]
    for r in recent:
        c = G if ok(r) else R
        out.append(f"  {c}{r.get('outcome', '?'):18}{X} {r['id']:34} {D}{str(r.get('detail') or '')[:70]}{X}")
    if events:
        out.append("")
        out.append(f"{B}events{X}")
        out += ["  " + e for e in events]
    return "\n".join(out)


def main(label="noai"):
    global LABEL, LOG, RES
    LABEL, LOG, RES = label, state.log_file(label), state.results_file(label)
    try:
        while True:
            screen = render()
            sys.stdout.write("\033[H\033[2J" + screen + "\n")
            sys.stdout.flush()
            time.sleep(2)
    except KeyboardInterrupt:
        pass
