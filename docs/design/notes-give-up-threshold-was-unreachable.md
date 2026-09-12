# R-150's give-up threshold was unreachable

Design 05 §2.2 sets two numbers, each reasonable on its own:

```go
var backoff = []time.Duration{0, 5*time.Second, 15*time.Second, 60*time.Second, 5*time.Minute}

const (
    failureThreshold = 10               // R-150
    failureWindow    = 30 * time.Minute
)
```

Ten failures within thirty minutes, with a backoff capping at five. Multiply it
out:

```
backoff after attempts 1..9:  5s, 15s, 60s, 300s, 300s, 300s, 300s, 300s, 300s
time to reach the 10th attempt: 1880s = 31.3 minutes
```

**31.3 minutes, against a window that resets at 30.** The tenth failure never
arrives inside the window, the count returns to one, and the app is retried
forever. That is the precise outcome R-150 exists to prevent — and the app it
happens to is a broken one, being restarted every five minutes indefinitely.

## How it surfaced

Not by reading the numbers. The phase's *Done when* — "killing it repeatedly
reaches `failed` and stays there" — ran against the real stack for twelve
minutes and reported:

```
app app_01M29PKV... stayed in "running", never reached [failed]
```

The first reading was that the test was wrong. It was, in a different way, and
fixing it exposed a second bug in the reconciler before this one became visible.

## The two bugs behind it

**The counter was measuring Apply errors.** A crash-looping app — the ordinary
failure mode — has a workload that exists, has exited, is recreated, and exits
again. Apply *succeeds* every time, because the container really is created. So
nothing was ever counted and the threshold was unreachable for a second,
independent reason.

What is counted now is *attempts*. An app that needed correcting was not
working; whether the correction returned an error is a detail of how it was not
working. Reaching `running` with health passing is the only thing that clears
the count, which is what design 05 §2.2 already said.

**The window was measured from the first failure.** Fixed at thirty minutes from
the first failure, the threshold is arithmetically unreachable as shown above.

Measured from the *last* failure — an idle timeout — it means what the
requirement means: consecutive failures always reach the threshold however long
backoff stretches them out, and unrelated failures a day apart never accumulate.
The column is `apps.last_failure_at`, and both published `[P]` numbers are
unchanged.

## Why not just change a number

Widening the window to 60 minutes or capping backoff at 60 seconds would both
work, and both leave the same trap for the next person: two independently tuned
constants whose product has to stay inside a third. An idle timeout has no such
coupling. Backoff can be retuned to anything, and the threshold stays reachable.

## A third one, from watching it run

The crash-looping app in the acceptance test showed as `Restarting (1)` in
`docker ps`, because Pando sets a restart policy on its containers. That is
wanted — the runtime recovers a crashed container faster than a fifteen-second
tick, and keeps doing it while Pando is away (design 05 §2.1.1).

The cost is that a crash-looping app is briefly `Running` between crashes. A
tick landing in that window reads it as recovered and clears the failure count,
so the app never reaches `failed`: every glimpse of it up undoes the progress
toward giving up on it. Design 05 §1.1 already says degraded means "health
failing **or restarting**"; nothing was reading the restart signal.

The first fix was a heuristic: a workload that has restarted before and started
again moments ago is looping, not recovered. `ObservedWorkload` already carried
`RestartCount` and `StartedAt`, and both were unused.

It was not enough, and watching a real crash-looping app showed why. Docker
backs off between restarts, so at twenty restarts the container had last started
**forty seconds** ago — past the thirty-second settle window — and the heuristic
declared it settled. The failure count reset instead of climbing, again.

The container inspects as:

```
Running=true  Restarting=true  RestartCount=20  StartedAt=40s ago
```

`Running` and `Restarting` are **both true at once**, and the runtime was saying
so the whole time. `ObservedWorkload` had no field for it. It does now, the
Docker adapter fills it from `State.Restarting`, and the heuristic stays behind
it as the fallback for a runtime that cannot tell — which is exactly the
position a heuristic should occupy.

The lesson is narrower than "use the real signal". It is that a heuristic
written to stand in for information you assume you lack should be checked
against what the adapter actually reports, because the assumption is the part
most likely to be wrong.

## What to take from it

The two constants were correct in isolation and wrong together, sitting four
lines apart in a design document that had been read several times. Nothing
caught it until an app was actually left to fail on a real machine for twelve
minutes.
