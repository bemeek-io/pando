# R-099's override clause contradicts R-272, and cites the wrong requirement

Found while implementing the compose importer.

## The conflict

**R-099** ends:

> Compose constructs incompatible with the boundary are rejected or rewritten,
> with the reason shown: `network_mode: host`, `privileged: true`, bind mounts
> to host paths, `deploy.replicas`. **Host policy governs whether an admin may
> override (R-190).**

Two problems with that last sentence.

**The cross-reference is wrong.** R-190 is *"Local secret storage: encrypted at
rest with a key on the same disk."* It has nothing to do with compose, policy,
or overrides. Nothing in the requirements grants an admin the power R-099
describes.

**The substance contradicts the general pattern.** R-272:

> A setting has a permissive default; host policy can **raise the floor**;
> app-level configuration can only move within what policy allows.

And R-270: *"Pando ships permissive defaults. Configuration narrows them."*

Policy can only deny. An admin turning on `privileged: true` for an app is
policy *granting* something the default refuses, which is the one direction the
model does not have. R-099's override clause would be the only place in the
system where policy widens rather than narrows — and it would do it for the
four constructs that specifically remove the isolation every other requirement
assumes is there.

## How it is implemented

The coherent reading, and the one in `internal/detect/compose.go`:

- The **permissive default** is what Pando accepts today: everything except the
  constructs that break the boundary. Those are refused unconditionally, with
  `PLAN_COMPOSE_CONSTRUCT_REJECTED` naming the service, the construct, and why.
- **Policy narrows further.** An admin can add to the refused set — a hardened
  install might refuse `cap_add` entirely, or refuse compose import altogether.
  That direction is R-272-shaped and needs no new concept.
- There is **no policy knob that permits a refused construct.** Nothing in the
  code reads such a setting, so nothing has to be reasoned about later.

Also worth recording: a rejection travels as `Candidate.Blocked` rather than as
an error returned from `Bid`. The auction drops a detector that errors, so one
broken detector cannot take detection down with it — but a *blocked* candidate
dropped the same way would have left the user with a buildpack guess and no
idea why their compose file was ignored. The best reading of the repository is
still the compose file, so the candidate stays in the auction and carries the
reason with it.

## What needs deciding

Someone should amend R-099. Two options:

1. **Drop the override clause.** Rejections stand; policy narrows only. This is
   what is implemented, it is consistent with R-270/R-272, and it needs no new
   mechanism.
2. **Keep it and carve out an exception to R-272**, which then has to say
   explicitly that this is the one setting where policy grants rather than
   denies, and what stops an admin from using it to defeat isolation between
   apps that other users own.

Option 1 unless there is a concrete scenario that needs option 2. Either way
the `(R-190)` citation is wrong and should be removed.
