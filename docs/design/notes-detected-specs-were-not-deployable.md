# A detected spec described the app and not the install

Found by asking a plain question — *could Pando deploy a repository right now?* —
and running it rather than reasoning about it. It could not.

```
POST /apps/{id}/detection  →  status: ready, strategy: dockerfile
POST /apps/{id}/detection:accept  →  revision 1 pinned
POST /apps/{id}/plan
{ "code": "VALID_INVALID",
  "message": "This app does not say how it should be reached." }
```

`detect.Job.assemble` built an `AppSpec` from eight fields — source, build,
workloads, volumes, slots, health, warnings — and left every other section
zero. Routing, runtime, deploy strategy, resources, retention and egress were
all empty, plus `build.adapter_ref` and the isolation floors. The first
plan-time check refused it.

## Why the tests did not catch it

Sequence A ends at step 17, pinning revision 1, and asserts **accepting does not
deploy**. Sequence B deploys a spec the test itself wrote with `putSpec`.

Both passed. *"Accepting does not deploy"* and *"what was pinned can be
deployed"* are different claims, and only the first was ever tested — the seam
between the two sequences was never walked. The fix includes the test that walks
it: `TestSequenceAtoB_ADetectedRepositoryDeploysAndServes` detects a real
repository, accepts it, plans it, deploys it, and fetches its front page through
the proxy.

## The rule the fix implements

R-104, which already answers this completely:

> Questions are blockers; everything else is configuration. Anything with a
> reasonable default gets the default and is changeable later in settings.
> Memory limits, restart policy, log retention, auto-deploy are configuration,
> not questions.

A repository can say it builds from a Dockerfile and listens on 3000. It cannot
say which runtime this install uses or how apps here are addressed — and none of
that is worth asking someone R-005 says may not know what a port is.

`spec.Defaults` fills the gaps, and lives in `spec` rather than in `detect`
because detection is not the only producer of an incomplete spec: an imported
one has exactly the same hole, and so does a hand-written one that omits a
section. It only ever fills empty fields, so it is safe to run over a spec
someone has edited.

Values come from where the requirements say they come from: the routing mode
from the routing adapter's own declared default (R-162, `ModeSource` recorded as
`adapter_default` for R-163), adapter refs from the registry's configured
defaults, isolation floors from host policy rather than from configuration
because policy is a floor and not a preference (R-272), and the rest from the
`[P]` values in R-144, R-152, R-211, R-223 and R-240.

Defaults are applied **at detection**, not at accept. The review is where
someone sees how their app will run, and a draft that says nothing about routing
or limits is not something anyone can review — they would be approving blanks
and finding out at deploy time.

## What it turned up

**Port allocation did not exist.** The `loopback` adapter that ships as the
laptop default is port mode only, so a detected app needs a host port, and no
requirement says where one comes from. Recorded as **O-15** with a `[P]`
implementation: lowest free port in a configured range, counting outstanding
detection proposals as well as pinned specs.

**Nothing ever destroys a bundle.** Deleting an app archives the row and never
tells the runtime; `RuntimeAdapter.Destroy` is implemented and has no caller.
Containers and the private network survive, and Docker's default address pool
holds about thirty. The acceptance suite exhausted it. Added to the risk
register against phase 7, where convergence lives.

**A test that cleaned up too aggressively broke the server.** The first attempt
force-disconnected Pando's container from each bundle network to reclaim it.
Pando is joined to every one of them — that is how the proxy reaches an app at
all (R-023) — and disconnecting a running container from a bridge network
disturbed its routing enough that the next outbound clone hung for minutes. It
presented as a slow network, not as the suite sabotaging the server, and it only
appeared on cold runs. Networks are now reclaimed at suite start, when the Pando
attached to them is already gone.
