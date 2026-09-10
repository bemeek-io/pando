# Acceptance tests

The four end-to-end sequences from `docs/design/07-sequences.md`. Between them they exercise every
subsystem: **if all four pass against real Postgres and real Docker, v1 works.**

| Sequence | File | Covers | Phase |
|---|---|---|---|
| A — Add an app | `sequence_a_onboard_test.go` | Detection auction, trial run, questions, proposal acceptance | 6 |
| B — Deploy | `sequence_b_deploy_test.go` | Plan boundary, build isolation, slots, secrets, routing | 4 |
| C — A request through the proxy | `sequence_c_proxy_test.go` | Both planes, assertion minting, header stripping, streaming | 5 |
| D — Disaster recovery | `sequence_d_dr_test.go` | Backup, manifest verification, restore, reconvergence | 9 |

Run with `make test-integration`. They use `testcontainers-go` against real Postgres and real Docker,
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
