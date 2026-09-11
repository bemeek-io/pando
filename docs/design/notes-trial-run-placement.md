# The trial run belongs to the runtime, not the builder

Design 03 §3 puts `ObservedPorts` and `ObservedWrites` on `BuildResult`, and
says they "are the trial run's output — the mechanism behind R-097's *watch what
it binds, don't ask*".

That cannot be built. Producing them means starting a container. Starting a
container needs a container runtime socket. **R-112:** *"The build path never
exposes a container runtime socket to build code. Mounting the Docker socket
into a build is a host compromise and is categorically forbidden."* Phase 4
already shipped `TestR112_BuildContainerHasNoRuntimeSocket`, which reads the
build container's real mount list and asserts the socket is not there.

So a builder that filled in those fields would either violate R-112 or lie.

Sequence A had it right all along — step 10 is *"trial run in throwaway
isolation"*, its own step after the auction, not part of step 8's bids or of the
build at all. Only the field placement in design 03 §3 was wrong.

## What was built

`RuntimeAdapter.Trial(ctx, TrialRequest) (TrialResult, error)`. On the runtime
because a runtime that can `Apply` a bundle can start one and throw it away —
it needs no capability the interface did not already require.

It is a required method rather than an optional one discovered by type
assertion. R-254 says capabilities are a returned struct, and the reason bites
here: `SupportsTrialRun`, `SupportsPortObservation` and `SupportsWriteObservation`
are declared, so "this runtime cannot observe ports" and "this app opened no
port" stay distinguishable. They are opposite facts that both arrive as an
empty slice.

`BuildResult` lost the two fields.

## Three things the real implementation turned up

**Port observation cannot exec into the app's container.** A `FROM scratch` Go
binary is a normal image, not an exotic one, and it contains no shell and no
tools. A sidecar joined to the trial container's *network* namespace reads
`/proc/net/tcp` instead — it shares nothing else, and the app's image needs to
contain nothing at all.

**Docker runs a DNS resolver inside every container.** On a user-defined
network there is always a listening socket on `127.0.0.11`, on a port that
changes every run. The first implementation reported it as the app's port, so a
`sleep 3600` container "listened on" 35211, then 37525, then 39895. Addresses
are now classified, not just ports.

Which turned out to be worth more than the bug fix. Separating loopback binds
from routable ones means Pando can say *"your app is listening on 3000, but only
on 127.0.0.1, which nothing outside the app can reach"* — a dev-server default,
one of the most common real deployment failures, and one that otherwise presents
as a healthy container whose every request times out with nothing in any log.

**A cancelled observation must not overwrite a successful one.** Ports are
polled while the container runs, so a healthy app costs ~2s instead of the whole
timeout. But the poll used the trial's own deadline context, so the last check
before expiry returned empty and erased what an earlier check had seen. The test
passed alone and failed in a full run. `observePorts` now reports whether it
managed to look, separately from what it found, and reads the sidecar's output
under a context of its own.

## R-097's real payoff

Questions a person has to answer went from 1.6 per repository to 0.9 across the
corpus, and the port question is most of the difference. R-005 says the person
deploying may not know what a port is, so a question about one is a question
they cannot answer — the trial run is what makes it unnecessary to ask.
