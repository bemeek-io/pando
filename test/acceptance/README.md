# Acceptance tests

The four end-to-end sequences from `docs/design/07-sequences.md`. Between them they exercise every
subsystem: **if all four pass against real Postgres and real Docker, v1 works.**

| Sequence | File | Covers | Phase | Status |
|---|---|---|---|---|
| A — Add an app | `sequence_a_detection_test.go` | Detection auction, trial run, questions, proposal acceptance | 7 | **passing** |
| B — Deploy | `sequence_b_deploy_test.go` | Plan boundary, build isolation, slots, secrets, routing | 6 | **passing** |
| C — A request through the proxy | `sequence_c_proxy_test.go` | Both planes, assertion minting, header stripping, streaming | 8 | **passing** |
| D — Disaster recovery | `sequence_d_backup_test.go` | Backup, manifest verification, restore, reconvergence | 9 | **passing** |

These drive a running Compose stack over HTTP, as a client would. Bring it up first:

```
docker compose down -v && PANDO_PORT=8099 docker compose up -d
go test -tags=integration ./test/acceptance/
```

A fresh `down -v` matters: the first-run administrator password is shown once (R-046),
and the tests read it from the server log to sign in.

Two ordinary things make the log a dead end — rebuilding the image, which replaces the
container that printed the password, and changing the password, which R-046 says you must.
Either one used to turn the whole suite into dozens of identical failures. Set
`PANDO_TEST_PASSWORD` to sign in without the log:

```
PANDO_TEST_PASSWORD=... go test -tags=integration ./test/acceptance/
```

### Running it in three minutes instead of forty-three

One test dominates: `TestR151_ACrashLoopingAppReachesFailedAndStaysThere` waits out R-149's
real retry backoff, which at the shipped numbers is about forty minutes of a
forty-three minute run. It is asserting the state machine, not the durations.

Compress the schedule and it finishes in three:

```
PANDO_RECONCILER_BACKOFF=0s,1s,2s,3s,4s \
PANDO_RECONCILER_FAILURE_WINDOW=2m \
PANDO_PORT=8099 docker compose up -d

PANDO_RECONCILER_BACKOFF=0s,1s,2s,3s,4s \
PANDO_RECONCILER_FAILURE_WINDOW=2m \
go test -tags=integration ./test/acceptance/
```

Both sides need it: the server runs the schedule and the test sizes its deadlines from the
same variables, so it cannot pass for the wrong reason. Pando warns at startup when the
backoff is faster than the default, because an install retrying a broken app every second
forever is a real way to melt a host — leave these unset in production, where the shipped
numbers *are* the requirement.

Run everything with `make test-integration`. They use `testcontainers-go` against real Postgres and real Docker,
and are behind an `integration` build tag so `make test` stays fast.

## The assertions that matter most

Each sequence lists its assertions in the design doc. Four of them mitigate named risks in
`docs/plan/risk-register.md` and are **not optional**:

- **B:** the build container has no runtime socket mounted — assert on the container's actual mount
  list, not on configuration intent.
- **B:** a failed build leaves the previous container running and the app's state unchanged.
- **C:** a request arriving with a forged `X-Pando-User` header reaches the app with that header
  **replaced**, never passed through.
- **D:** a truncated or tampered bundle is rejected at verification **with the target install
  untouched.**

## Naming

Individual assertions follow the `TestR###_…` convention from design 00 §4 where they map to a single
requirement, so `make requirements-coverage` sees them. Sequence-level tests are named for the
sequence and call sub-tests that carry the R-IDs.
