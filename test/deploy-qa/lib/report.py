"""Build out/report.html (and out/summary.md) from the results of the last run.

Works with one pass or both. When both exist it compares them app by app; it
always compares against the committed baseline, so fixes show up as changes.
"""
import glob
import html
import json
import os
from collections import Counter, defaultdict

from . import classify, state

H = html.escape


def load(f):
    d = {}
    for l in open(f):
        r = json.loads(l)
        d[r["id"]] = r
    return d


def _load_or_empty(label):
    f = state.results_file(label)
    return load(f) if os.path.exists(f) else {}


NO = _load_or_empty("noai")
AI = _load_or_empty("ai")
if not NO and AI:  # an AI-only run: report it as the primary pass
    NO, AI = AI, {}
IDS = sorted(NO)
BASE_FILES = sorted(glob.glob(os.path.join(state.HERE, "baseline", "*.json")))
BASE = json.load(open(BASE_FILES[-1])) if BASE_FILES else {"apps": {}}
ok = lambda r: r["outcome"].startswith("PASS")


def rate(rows):
    n = len(rows)
    return (sum(ok(r) for r in rows), n)


def pct(a, n):
    return f"{100 * a / n:.1f}%" if n else "—"


def evidence(r):
    if ok(r):
        a = r.get("answered") or {}
        return "HTTP " + str(r.get("http_status") or "") + ("; answered " + ", ".join(f"{k}={v}" for k, v in a.items()) if a else "")
    t = r.get("deploy_log_tail") or ""
    for key in ("emerg]", "EBADENGINE", "No start command", "Unsupported", "error", "Error"):
        for line in t.splitlines():
            if key in line and "The build failed" not in line and "failed to solve" not in line:
                return line.strip()[:220]
    logs = r.get("app_logs") or ""
    if logs and logs != "{}":
        return ("app log: " + logs[-200:]).replace("\\n", " ")
    return str(r.get("detail") or "")[:220]


# ---------------------------------------------------------------- aggregates
groups = {
    "Source": lambda r: classify.SOURCE.get(r.get("source"), r.get("source")),
    "Language": classify.language,
    "Packaging": classify.packaging,
}


def group_rows(fn):
    g = defaultdict(list)
    for i in IDS:
        g[fn(NO[i])].append(i)
    out = []
    for k, ids in g.items():
        a, n = rate([NO[i] for i in ids])
        b, _ = rate([AI[i] for i in ids if i in AI])
        out.append((k, n, a, b))
    return sorted(out, key=lambda t: (-t[1], t[0]))


def bars(rows):
    out = ['<div class="bars" role="img" aria-label="Success rate by group, without and with AI">']
    for k, n, a, b in rows:
        pa, pb = 100 * a / n, 100 * b / n
        out.append(f'''<div class="bar-row">
  <div class="bar-label">{H(k)} <span class="muted">({n})</span></div>
  <div class="bar-pair">
    <div class="bar-track" title="Without AI: {a}/{n} ({pa:.0f}%)"><div class="bar s1" style="width:{pa:.1f}%"></div><span class="bar-val">{pa:.0f}%</span></div>
    {f'<div class="bar-track" title="With AI: {b}/{n} ({pb:.0f}%)"><div class="bar s2" style="width:{pb:.1f}%"></div><span class="bar-val">{pb:.0f}%</span></div>' if AI else ''}
  </div>
</div>''')
    out.append("</div>")
    return "\n".join(out)


def table(rows, label):
    ai_cols = '<th class="n">With AI</th><th class="n">Change</th>' if AI else ''
    t = [f'<div class="tbl"><table><thead><tr><th>{label}</th><th class="n">Apps</th><th class="n">Without AI</th>{ai_cols}</tr></thead><tbody>']
    for k, n, a, b in rows:
        d = b - a
        ai_cells = f'<td class="n">{b} · {pct(b, n)}</td><td class="n {"up" if d > 0 else "down" if d < 0 else ""}">{d:+d}</td>' if AI else ''
        t.append(f'<tr><td>{H(k)}</td><td class="n">{n}</td><td class="n">{a} · {pct(a, n)}</td>{ai_cells}</tr>')
    t.append("</tbody></table></div>")
    return "\n".join(t)


# causes
cause_no = Counter(classify.cause(NO[i]) for i in IDS if not ok(NO[i]))
cause_ai = Counter(classify.cause(AI[i]) for i in IDS if i in AI and not ok(AI[i]))
apps_by_cause = defaultdict(list)
for i in IDS:
    c = classify.cause(NO[i])
    if c:
        apps_by_cause[c].append(i)

# AI effect
reg = [i for i in IDS if i in AI and ok(NO[i]) and not ok(AI[i])]
fix = [i for i in IDS if i in AI and not ok(NO[i]) and ok(AI[i])]
screen = [AI[i].get("detection", {}).get("screening") or {} for i in IDS if i in AI]
ran = [s for s in screen if s.get("ran")]
applied = sum(len(s.get("applied") or []) for s in ran)
skipped = Counter(s.get("skip_code") or s.get("skipped") for s in screen if not s.get("ran"))
durs = sorted(s.get("duration_ms", 0) for s in ran)
amend_kinds = Counter(a.get("amendment", {}).get("kind") for s in ran for a in (s.get("applied") or []))

a_no, n = rate([NO[i] for i in IDS])
a_ai, _ = rate([AI[i] for i in IDS if i in AI])
zt_no = sum(NO[i]["outcome"] == "PASS_ZERO_TOUCH" for i in IDS)
zt_ai = sum(AI[i]["outcome"] == "PASS_ZERO_TOUCH" for i in IDS if i in AI)
pando_fail_no = sum(v for k, v in cause_no.items() if classify.C[k][0] == "PANDO")
other_fail_no = sum(v for k, v in cause_no.items() if classify.C[k][0] == "OTHER")
excl = sum(v for k, v in cause_no.items() if k in ("harness",))


def app_list(ids):
    return ", ".join(f"<code>{H(i)}</code>" for i in ids)


def ai_change_rows(ids):
    out = []
    for i in ids:
        s = AI[i].get("detection", {}).get("screening") or {}
        am = "; ".join(a.get("summary", "") for a in (s.get("applied") or [])) or ("screening skipped: " + str(s.get("skipped")) if not s.get("ran") else "no amendments")
        out.append(f'<tr><td><code>{H(i)}</code></td><td>{H(NO[i]["outcome"])}</td><td>{H(AI[i]["outcome"])}</td><td>{H(am[:260])}</td><td>{H(evidence(AI[i])[:200])}</td></tr>')
    return "\n".join(out)


cause_rows = []
for k, (kind, label) in classify.C.items():
    if cause_no.get(k) or cause_ai.get(k):
        cause_rows.append((kind, label, cause_no.get(k, 0), cause_ai.get(k, 0), apps_by_cause.get(k, [])))
cause_rows.sort(key=lambda t: (t[0] != "PANDO", -t[2]))

all_rows = []
for i in IDS:
    r, s = NO[i], AI.get(i, {})
    c = classify.cause(r) or classify.cause(s) if s else classify.cause(r)
    all_rows.append(f'''<tr data-q="{H((i + ' ' + classify.language(r) + ' ' + classify.packaging(r) + ' ' + str(r['outcome']) + ' ' + str(s.get('outcome'))).lower())}">
<td><code>{H(i)}</code></td><td>{H(classify.SOURCE.get(r.get('source'), ''))}</td><td>{H(classify.language(r))}</td><td>{H(classify.packaging(r))}</td>
<td class="{'pass' if ok(r) else 'fail'}">{H(r['outcome'].replace('_', ' ').title())}</td>
<td class="{'pass' if s and ok(s) else 'fail'}">{H(str(s.get('outcome', '—')).replace('_', ' ').title())}</td>
<td>{H(classify.C[classify.cause(r)][1] if classify.cause(r) else '')}</td>
<td class="ev">{H(evidence(r))}</td></tr>''')

# change against the committed baseline (the no-AI pass is the number tracked)
b_apps = BASE.get("apps", {})
newly_pass = [i for i in IDS if i in b_apps and ok(NO[i]) and not b_apps[i]["noai"].startswith("PASS")]
newly_fail = [i for i in IDS if i in b_apps and not ok(NO[i]) and b_apps[i]["noai"].startswith("PASS")]
b_pass = sum(v["noai"].startswith("PASS") for v in b_apps.values())
st = state.load()

page = f"""<title>Pando Deploy QA</title>
<link rel="preconnect" href="https://fonts.googleapis.com">
<link rel="stylesheet" href="https://fonts.googleapis.com/css2?family=IBM+Plex+Sans:wght@400;500;600&family=IBM+Plex+Mono:wght@400;500&family=Fraunces:opsz,wght@9..144,500;9..144,600&display=swap">
<style>
:root {{
  --bg: #f6f7f5; --panel: #ffffff; --ink: #14181a; --ink-2: #4a5155; --ink-3: #7b8286;
  --rule: #dfe3e1; --accent: #1f5c4d; --s1: #2a78d6; --s2: #eb6834;
  --pass: #1d7a45; --fail: #b23a2c; --track: #eceeed;
}}
@media (prefers-color-scheme: dark) {{ :root:not([data-theme="light"]) {{
  color-scheme: dark; --bg: #121516; --panel: #1a1e1f; --ink: #eef1f0; --ink-2: #b9c0c2; --ink-3: #8a9295;
  --rule: #2c3234; --accent: #6fc2a8; --s1: #3987e5; --s2: #d95926; --pass: #5cc48a; --fail: #ec7a6b; --track: #252a2c; }} }}
:root[data-theme="dark"] {{ color-scheme: dark; --bg: #121516; --panel: #1a1e1f; --ink: #eef1f0; --ink-2: #b9c0c2; --ink-3: #8a9295;
  --rule: #2c3234; --accent: #6fc2a8; --s1: #3987e5; --s2: #d95926; --pass: #5cc48a; --fail: #ec7a6b; --track: #252a2c; }}
body {{ background: var(--bg); color: var(--ink); font: 15px/1.55 "IBM Plex Sans", system-ui, sans-serif; }}
.wrap {{ max-width: 1080px; margin: 0 auto; padding-inline: 20px; padding-block: 40px 80px; display: grid; gap: 44px; }}
h1, h2, h3 {{ font-family: "Fraunces", Georgia, serif; font-weight: 600; text-wrap: balance; margin: 0; }}
h1 {{ font-size: 2.4rem; letter-spacing: -0.01em; }}
h2 {{ font-size: 1.5rem; margin-bottom: 14px; }}
h3 {{ font-size: 1.1rem; margin: 18px 0 8px; }}
h4 {{ margin: 0 0 2px; font-size: 1rem; }}
p {{ margin: 0 0 10px; max-width: 72ch; color: var(--ink-2); }}
code {{ font: 0.85em "IBM Plex Mono", ui-monospace, monospace; }}
.eyebrow {{ font: 500 0.78rem "IBM Plex Mono", monospace; letter-spacing: 0.08em; text-transform: uppercase; color: var(--accent); }}
.meta {{ color: var(--ink-3); font-size: 0.9rem; }}
.head {{ display: grid; gap: 10px; }}
.kpis {{ display: grid; grid-template-columns: repeat(auto-fit, minmax(190px, 1fr)); gap: 12px; }}
.kpi {{ background: var(--panel); border: 1px solid var(--rule); border-radius: 6px; padding: 16px 18px; }}
.kpi .v {{ font: 600 2rem "Fraunces", Georgia, serif; font-variant-numeric: tabular-nums; }}
.kpi .l {{ color: var(--ink-2); font-size: 0.88rem; }}
.kpi .s {{ color: var(--ink-3); font-size: 0.8rem; margin-top: 4px; }}
.legend {{ display: flex; gap: 18px; font-size: 0.85rem; color: var(--ink-2); margin-bottom: 10px; flex-wrap: wrap; }}
.sw {{ display: inline-block; width: 12px; height: 12px; border-radius: 2px; vertical-align: -1px; margin-right: 6px; }}
.bars {{ display: grid; gap: 10px; }}
.bar-row {{ display: grid; grid-template-columns: minmax(140px, 240px) 1fr; gap: 12px; align-items: center; }}
.bar-label {{ font-size: 0.9rem; }}
.bar-pair {{ display: grid; gap: 2px; }}
.bar-track {{ position: relative; height: 12px; background: var(--track); border-radius: 0 4px 4px 0; }}
.bar {{ height: 100%; border-radius: 0 4px 4px 0; }}
.s1 {{ background: var(--s1); }} .s2 {{ background: var(--s2); }}
.bar-val {{ position: absolute; left: calc(100% + 6px); top: -3px; font: 0.75rem "IBM Plex Mono", monospace; color: var(--ink-2); }}
.bar-track {{ margin-right: 44px; }}
.muted {{ color: var(--ink-3); }}
.tbl {{ overflow-x: auto; border: 1px solid var(--rule); border-radius: 6px; background: var(--panel); }}
table {{ border-collapse: collapse; width: 100%; font-size: 0.86rem; }}
th, td {{ text-align: left; padding: 7px 10px; border-bottom: 1px solid var(--rule); vertical-align: top; }}
th {{ font-weight: 600; color: var(--ink-2); background: var(--bg); position: sticky; top: 0; }}
td.n, th.n {{ text-align: right; font-variant-numeric: tabular-nums; white-space: nowrap; }}
td.up {{ color: var(--pass); }} td.down {{ color: var(--fail); }}
td.pass {{ color: var(--pass); font-weight: 500; }} td.fail {{ color: var(--fail); }}
td.ev {{ color: var(--ink-3); font: 0.78rem "IBM Plex Mono", monospace; max-width: 380px; word-break: break-word; }}
.kind {{ font: 500 0.72rem "IBM Plex Mono", monospace; padding: 1px 6px; border-radius: 3px; white-space: nowrap; }}
.kind.p {{ background: color-mix(in srgb, var(--fail) 14%, transparent); color: var(--fail); }}
.kind.o {{ background: var(--track); color: var(--ink-2); }}
.findings {{ list-style: none; padding: 0; margin: 0; display: grid; gap: 14px; }}
.findings li {{ border-left: 3px solid var(--fail); padding: 2px 0 2px 14px; }}
.findings .where {{ margin: 0 0 4px; color: var(--ink-3); }}
.grid2 {{ display: grid; grid-template-columns: repeat(auto-fit, minmax(320px, 1fr)); gap: 24px; }}
input[type=search] {{ width: 100%; max-width: 360px; padding: 8px 10px; border: 1px solid var(--rule); border-radius: 4px; background: var(--panel); color: var(--ink); font: inherit; margin-bottom: 10px; }}
input:focus-visible {{ outline: 2px solid var(--accent); outline-offset: 1px; }}
.all {{ max-height: 640px; overflow: auto; }}
@media (max-width: 560px) {{ .bar-row {{ grid-template-columns: 1fr; gap: 4px; }} h1 {{ font-size: 1.9rem; }} }}
</style>
<div class="wrap">
<header class="head">
  <div class="eyebrow">Deployment QA · {H(st.get("git_ref", ""))} · {H(st.get("run_started", ""))}</div>
  <h1>Pando Deploy QA</h1>
  <p>{n} apps were created, detected, deployed and requested through Pando's proxy{", once with no AI adapter and once with the Anthropic adapter screening every plan" if AI else ""}. An app passes when it answers HTTP through the proxy (with the expected page, where known), or, for things that are not web apps, when Pando declines to deploy it. Questions were answered the way the app's author would answer them.</p>
</header>

<section class="kpis">
  <div class="kpi"><div class="l">Success without AI</div><div class="v">{pct(a_no, n)}</div><div class="s">{a_no} of {n} · {zt_no} with no input at all</div></div>
  <div class="kpi"><div class="l">Since baseline ({H(BASE.get("run", "none"))})</div><div class="v">{a_no - b_pass:+d}</div><div class="s">{len(newly_pass)} newly passing · {len(newly_fail)} newly failing</div></div>
  {"" if not AI else ""}<div class="kpi" {"hidden" if not AI else ""}><div class="l">Success with AI</div><div class="v">{pct(a_ai, n)}</div><div class="s">{a_ai} of {n} · {zt_ai} with no input at all</div></div>
  <div class="kpi" {"hidden" if not AI else ""}><div class="l">AI regressions</div><div class="v">{len(reg)}</div><div class="s">passed without AI, failed with it</div></div>
  <div class="kpi" {"hidden" if not AI else ""}><div class="l">AI fixes</div><div class="v">{len(fix)}</div><div class="s">failed without AI, passed with it</div></div>
</section>

<section>
  <h2>Where it works and where it doesn't</h2>
  <div class="legend"><span><span class="sw s1"></span>Without AI</span>{'<span><span class="sw s2"></span>With AI</span>' if AI else ''}</div>
  <div class="grid2">
    <div><h3>By source</h3>{bars(group_rows(groups["Source"]))}</div>
    <div><h3>By packaging</h3>{bars(group_rows(groups["Packaging"]))}</div>
  </div>
  <h3>By language</h3>
  {bars(group_rows(groups["Language"]))}
  <h3>Same numbers as tables</h3>
  <div class="grid2">{table(group_rows(groups["Source"]), "Source")}{table(group_rows(groups["Packaging"]), "Packaging")}</div>
  <div style="margin-top:12px">{table(group_rows(groups["Language"]), "Language")}</div>
</section>

<section>
  <h2>Changes since the baseline</h2>
  <p>Baseline: {H(BASE.get("run", "none"))} at {H(BASE.get("commit", ""))}, {b_pass} of {len(b_apps)} passing without AI (issue #{BASE.get("issue", "")}).</p>
  <h3>Newly passing ({len(newly_pass)})</h3><p>{app_list(newly_pass) or "None."}</p>
  <h3>Newly failing ({len(newly_fail)})</h3><p>{app_list(newly_fail) or "None."}</p>
</section>

<section {"hidden" if not AI else ""}>
  <h2>What the AI adapter did</h2>
  <p>Screening ran on {len(ran)} of {len(screen)} apps and applied {applied} amendments ({", ".join(f"{k} ×{v}" for k, v in amend_kinds.most_common()) or "none"}). Median screening time {durs[len(durs)//2]/1000 if durs else 0:.1f} s. {("Skipped: " + ", ".join(f"{k} ×{v}" for k, v in skipped.items())) if skipped else ""}</p>
  <h3>Regressions ({len(reg)})</h3>
  {'<p>None: no app that passed without AI failed with it.</p>' if not reg else f'<div class="tbl"><table><thead><tr><th>App</th><th>Without AI</th><th>With AI</th><th>What screening changed</th><th>Evidence</th></tr></thead><tbody>{ai_change_rows(reg)}</tbody></table></div>'}
  <h3>Fixes ({len(fix)})</h3>
  {'<p>None.</p>' if not fix else f'<div class="tbl"><table><thead><tr><th>App</th><th>Without AI</th><th>With AI</th><th>What screening changed</th><th>Evidence</th></tr></thead><tbody>{ai_change_rows(fix)}</tbody></table></div>'}
</section>

<section>
  <h2>Why apps failed</h2>
  <p>Every failure is assigned exactly one cause. Without AI: {sum(cause_no.values())} failures, {pando_fail_no} attributable to Pando and {other_fail_no} not ({excl} of those from the test environment). With AI: {sum(cause_ai.values())} failures.</p>
  <div class="tbl"><table><thead><tr><th>Cause</th><th></th><th class="n">Without AI</th><th class="n">With AI</th><th>Apps (without AI)</th></tr></thead><tbody>
  {"".join(f'<tr><td>{H(l)}</td><td><span class="kind {"p" if k == "PANDO" else "o"}">{"Pando" if k == "PANDO" else "Not Pando"}</span></td><td class="n">{a}</td><td class="n">{b}</td><td>{app_list(ids)}</td></tr>' for k, l, a, b, ids in cause_rows)}
  <tr><td><strong>Total</strong></td><td></td><td class="n"><strong>{sum(cause_no.values())}</strong></td><td class="n"><strong>{sum(cause_ai.values())}</strong></td><td></td></tr>
  </tbody></table></div>
</section>


<section>
  <h2>Every app</h2>
  <input type="search" id="q" placeholder="Filter by app, language, outcome…" aria-label="Filter apps">
  <div class="tbl all"><table><thead><tr><th>App</th><th>Source</th><th>Language</th><th>Packaging</th><th>Without AI</th><th>With AI</th><th>Cause (without AI)</th><th>Evidence</th></tr></thead>
  <tbody id="rows">{"".join(all_rows)}</tbody></table></div>
</section>

<section>
  <h2>How this was run</h2>
  <p>Generated by <code>test/deploy-qa/qa.py</code>: a separate Pando instance (compose project <code>pando-qa</code>) built from the checkout under test, driven through its API. Each app is created (git URL, published image, or uploaded directory), detected, answered, accepted, deployed with 0.25 CPU, and requested through the proxy; then everything it created is removed and checked. See <code>test/deploy-qa/README.md</code>.</p>
</section>
</div>
<script>
const q = document.getElementById('q'), rows = [...document.querySelectorAll('#rows tr')];
q.addEventListener('input', () => {{ const v = q.value.trim().toLowerCase(); rows.forEach(r => r.hidden = v && !r.dataset.q.includes(v)); }});
</script>
"""
open(state.path("report.html"), "w").write(page)

md = [f"## Pando deploy QA — {st.get('run_started', '')} — {st.get('git_ref', '')}", "",
      f"- Without AI: **{a_no}/{n} ({pct(a_no, n)})** · change since baseline {H(BASE.get('run', ''))}: {a_no - b_pass:+d}"]
if AI:
    md.append(f"- With AI: **{a_ai}/{n} ({pct(a_ai, n)})** · regressions {len(reg)} · fixes {len(fix)}")
md += [f"- Newly passing: {', '.join(newly_pass) or 'none'}", f"- Newly failing: {', '.join(newly_fail) or 'none'}", "",
       "| Cause | Without AI" + (" | With AI |" if AI else " |"), "|---|---" + ("|---|" if AI else "|")]
for k, l, a, b, ids in cause_rows:
    md.append(f"| {l} | {a}" + (f" | {b} |" if AI else " |"))
open(state.path("summary.md"), "w").write("\n".join(md) + "\n")


def main():
    print(f"wrote {state.path('report.html')} and summary.md; noai {a_no}/{n}" + (f", ai {a_ai}/{n}, regressions {len(reg)}, fixes {len(fix)}" if AI else ""))
