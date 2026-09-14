# A question deferred to a trial run that cannot happen

Adding a plain Go module — `go.mod`, no Dockerfile — asked two questions:

> This app appears to be a Go project, but it does not include a Dockerfile or
> any other instructions for running it. Pando needs the command that starts it.

> Pando could not determine which port this Go app serves HTTP on.

Neither needed to be asked, and the corpus reported the repository as costing
one question.

## The port

`Question.Deferred` was introduced by the corpus work
([notes](notes-detection-corpus-findings.md)) to stop charging the user for a
question R-097 answers by watching the app bind. That reasoning holds exactly
as far as the trial run reaches, and it does not reach a source build.

`Job.trial` returns early when the winning draft's primary workload has no
image:

```go
primary, ok := primaryWorkload(draft)
if !ok || primary.Image == "" {
    // Nothing to run. A source build would have to happen first, and
    // building before a proposal is reviewed is work nobody asked for.
    return Trial{}
}
```

That early return is right. Building an image to answer a question about a
proposal nobody has accepted is work nobody asked for, and R-024 keeps builds
off the host besides. But it means the trial run only ever happens for the two
tiers that arrive with an image — a published image (R-094 tier 1) and a compose
file naming images. Every strategy that builds from source — `dockerfile`,
`buildpack`, `static` — reaches `ApplyTrial` with `Trial{Ran: false}`, and
`undefer` turns every deferred question back into one a person answers.

So the port question was deferred to something that, for these repositories,
never runs. It arrived in front of a person anyway — a question about a port,
put to someone R-005 says may not know what a port is, on every repository
with no Dockerfile.

R-104 settles what should happen instead: *anything with a reasonable default
gets the default and is changeable later in settings*. The default already
existed. `BuildpackDetector` puts the language's usual port in the draft as
`spec.PortFramework`, which is the value the review screen shows, labeled as
the guess it is and editable before anything is pinned. The question was asking
for something Pando had already written down.

The language detectors no longer raise it. The Dockerfile-without-`EXPOSE` case
still does, and should: no default is known there, so Pando genuinely cannot
proceed.

## The start command

`BuildpackDetector.Bid` attached the generated build plan to the draft and then
appended the start-command question two lines later, unconditionally.

nixpacks had already answered it. On the repository above it decides
`go build -o out ./cmd/server` and writes a Dockerfile ending `CMD ["./out"]`.
The prompt's first clause — *"it does not include a Dockerfile or any other
instructions for running it"* — was false at the moment it was shown, about a
Dockerfile Pando wrote itself and put in the proposal for review.

The question is now asked only when nothing worked the answer out: no planner is
configured, or the plan carries no start instruction. With a planner attached, a
plain Go module asks nothing at all.

Reading the plan needed one fix. `readDockerfile` took the *first* `CMD` or
`ENTRYPOINT`, and nixpacks writes a multi-stage file whose build stage opens
with `ENTRYPOINT ["/bin/bash", "-l", "-c"]` — so the first instruction is the
shell wrapper and the real `CMD` is the last line. Only the final stage's
survives into the image, so the last one is the correct answer generally, not
only here.

## Why the metric did not show it

The corpus counted `detect.Asked(result.Questions)` straight off the auction,
which is before `ApplyTrial`. A deferred question was therefore free in the
measurement whether or not anything could answer it, and the mean sat at 0.90
against a budget of 1.00 while four of the ten cases put two questions in front
of a person.

`runCase` now applies `Trial{Ran: false}` before counting, for any winner whose
draft has no image to start — the same condition `Job.trial` applies. Restoring
the old detector behavior against the new counting fails four cases at
`questions 2/1` and the aggregate at `mean 1.30`, which is what it should have
done in the first place.

The deferred count is still printed beside each case, so moving a question out
of the budget stays visible rather than becoming a way to hide it. What changed
is that it can no longer be moved somewhere nothing collects it.
