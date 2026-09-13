# Risk register

From design 08 §3, with the mitigation stated as work rather than intention. Each of these is a
failure mode that has already been reasoned about — if you are about to do the thing in the "risk"
column, the mitigation is not optional.

| Risk | Phase | Mitigation | Status |
|---|---|---|---|
| Detection quality below the R-103 bar | 6 | Build a corpus of **30 real repos early**. Track questions-per-deploy as a metric, not a vibe. | **Mitigated** — 10 repos in `test/corpus`, `make detection-corpus`. Its first run against real repositories found three detector defects that unit tests had not (see [detection notes](../design/notes-detection-corpus-findings.md)). Currently 0.90 questions per deploy, worst 1, strategy found in 100% of the cases where one existed. Corpus needs growing to 30 |
| Docker socket leaks into a build | 4 | Integration test asserting the build container's mount list. **Non-negotiable.** | **Closed** — `TestR112_BuildContainerHasNoRuntimeSocket` reads the real mount list, plus privileged/host-network/host-PID, plus a behavioural check from inside the container |
| Header spoofing through the proxy | 5 | Explicit test with forged `X-Pando-*` headers asserting replacement. | **Closed** — `TestR053_ForgedHeadersReachTheAppReplaced` runs against a real deployed app that echoes what it received, covering four spellings, an unknown header in the namespace, and an inbound `X-Forwarded-Prefix` |
| Postgres prerequisite undermines the hobbyist install | 0 | Resolved: Compose supplies Postgres beside Pando, adding no prerequisite a containerized runtime did not already impose. | **Closed** |
| Routing abstraction is Docker/Traefik-shaped | 10 | Sketch the Cloudflare adapter **on paper during phase 3**, before the interface is fixed. | **Closed** — the sketch was done in phase 3 ([notes](../design/notes-cloudflare-routing-sketch.md)) and phase 10 tested it for real: a Traefik adapter routes an app end to end and `RoutingAdapter` and its types were not touched. A paper sketch shows an interface *could* stretch; a second implementation shows it did |
| Required-vs-optional slots (O-4) | 6 | Trial-run promotion fallback. Measure false-block rate against the corpus. | **Mitigated** — promotion requires the app to have named the slot in its own crash log, so one syntax error cannot become forty required slots and forty deploy blockers. Tested both ways. False-block rate still needs measuring against a corpus with crashing apps in it, which the current ten do not have |
| An app's bundle is never destroyed | 7 | Nothing called `RuntimeAdapter.Destroy`. Deleting an app archived the row and left its containers and private network running; Docker's default address pool holds ~30, so an install that adds and deletes apps eventually could not start one. Found because the acceptance suite exhausted the pool | **Closed** — the GC tears down the bundles of deleted apps and reclaims their networks, keeping volumes (R-204). Two bugs were in the way: `Destroy` discarded the `NetworkRemove` error, and the removal could not have succeeded anyway because Pando stays attached to every bundle network. `TestR204_DeletingAnAppTearsDownItsBundleButKeepsVolumes` asserts both halves |
| No install-level authorization exists (O-17) | 8 | Six endpoints were gated by "are you signed in" and nothing else, because there was nothing to gate them with. A user with no grants could suspend the administrator and lock the install out — demonstrated on the shipped stack. The per-app model was sound; there was simply no install scope | **Closed** — install-scoped verbs held as a grant with no app, a fourth built-in role, and `CheckInstall`. The correspondence between "no app" and "carries install verbs" is enforced by a composite foreign key and two CHECKs rather than by the handler. Five acceptance tests against the shipped stack, the first of which is the escalation itself refused (`TestO17_AnOrdinaryUserCannotSuspendTheAdministrator`) |
| The two planes get conflated again | 1 | The comment in `CheckData` explaining that this was reversed once, **plus** a test asserting an operator on someone else's app is denied use. | **Closed** — the comment is there and there are four tests at three levels: `TestR029_ControlPlaneRoleDoesNotGrantDataPlaneUse` and `TestR029_OwnerRoleAloneDoesNotGrantUse` on the authorizer, `TestR029_AnOperatorWithNoDataGrantIsDenied` on the proxy, and `TestR029_AnOperatorIsDeniedUseThroughTheProxy` end to end. This row was stale, not the mitigation |

## Why these

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
