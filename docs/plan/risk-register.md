# Risk register

From design 08 §3, with the mitigation stated as work rather than intention. Each of these is a
failure mode that has already been reasoned about — if you are about to do the thing in the "risk"
column, the mitigation is not optional.

| Risk | Phase | Mitigation | Status |
|---|---|---|---|
| Detection quality below the R-103 bar | 6 | Build a corpus of **30 real repos early**. Track questions-per-deploy as a metric, not a vibe. | Not started |
| Docker socket leaks into a build | 4 | Integration test asserting the build container's mount list. **Non-negotiable.** | **Closed** — `TestR112_BuildContainerHasNoRuntimeSocket` reads the real mount list, plus privileged/host-network/host-PID, plus a behavioural check from inside the container |
| Header spoofing through the proxy | 5 | Explicit test with forged `X-Pando-*` headers asserting replacement. | **Closed** — `TestR053_ForgedHeadersReachTheAppReplaced` runs against a real deployed app that echoes what it received, covering four spellings, an unknown header in the namespace, and an inbound `X-Forwarded-Prefix` |
| Postgres prerequisite undermines the hobbyist install | 0 | Resolved: Compose supplies Postgres beside Pando, adding no prerequisite a containerized runtime did not already impose. | **Closed** |
| Routing abstraction is Docker/Traefik-shaped | 10 | Sketch the Cloudflare adapter **on paper during phase 3**, before the interface is fixed. | **Done** — sketched ([notes](../design/notes-cloudflare-routing-sketch.md)); interface unchanged, two documentation clarifications recorded in design 03 §4 |
| Required-vs-optional slots (O-4) | 6 | Trial-run promotion fallback. Measure false-block rate against the corpus. | Open |
| The two planes get conflated again | 1 | The comment in `CheckData` explaining that this was reversed once, **plus** a test asserting an operator on someone else's app is denied use. | Not started |

## Why these seven

They share a property: each is a failure that a code review would plausibly wave through, and each
becomes dramatically more expensive to fix later than to prevent now. Four of the seven are mitigated
by a **single specific test**, which is why those tests are named in the phase files rather than left
to judgment.

The two that are not test-shaped — the Postgres prerequisite and the routing abstraction — are
mitigated by doing a cheap thing early: resolve one decision, and spend ten minutes with a pencil on
an adapter nobody has asked for yet. The first of those is now done.

One risk the resolution introduces, small but real: the external-database path can be pointed at a
cluster where Pando cannot constrain the application role, which would leave the audit log rewritable.
The mitigation is a startup preflight that fails loudly rather than degrading — phase 0.
