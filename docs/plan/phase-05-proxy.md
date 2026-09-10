# Phase 5 — Proxy

**Goal:** the single enforcement point. This phase ends in Sequence C, the hottest path in the system
and the one where a mistake is worst.

**Prerequisites:** phase 4.

**Design:** `../design/06-authorization-and-proxy.md` §4; `../design/07-sequences.md` C. Also read
`../../internal/proxy/CLAUDE.md` before writing a line.

## Tasks

- [ ] App resolution from Host header, and from path prefix in proxy mode
- [ ] Session and bearer authentication → `Principal`, or anonymous
- [ ] `CheckData` at the proxy; denied+anonymous → 302 login, denied+authed → 403
- [ ] Assertion minting: Ed25519, `aud` = app ID, 120s lifetime, per request (R-051, R-054, R-055)
- [ ] **Unconditional inbound `X-Pando-*` strip**, before setting anything (R-053)
- [ ] Convenience headers, documented as unverified
- [ ] JWKS at `/.well-known/jwks.json`, with key IDs and overlap rotation (R-057)
- [ ] Path mode: strip prefix, set `X-Forwarded-Prefix` (R-167)
- [ ] Streaming: no buffering, `Flush()` per SSE write, websocket hijack, no body size limit (R-170)
- [ ] Anonymous assertion with the constant `sub: "anonymous"` (R-056)
- [ ] Long-lived connection re-authorization (O-13, resolved): re-run `CheckData` on the assertion
      lifetime — the same 120s, not a second number — and close with a policy-violation close frame
      so a client can tell revocation from a network fault

## Requirements in scope

R-023, R-026, R-051–R-057, R-072, R-075, R-079, R-087, R-167, R-170.

## Done when

**Sequence C passes**, including the forged-header test and the cross-app `aud` rejection.

## Traps

- **The header strip is the single most likely serious bug in this system.** A client sets
  `X-Pando-User: admin@corp.com`; without an unconditional strip, any app trusting the convenience
  headers is trivially spoofed. Test that a forged header arrives *replaced*.
- **There is no bypass** — not for public apps, not for performance, not for websockets. Prove it by
  asserting the anonymous path still increments the audit/metrics counter.
- Absence of the assertion header means the request did not come through Pando. Say so explicitly in
  the app-developer documentation; apps may reject on that basis.
- **[O-13] is resolved:** re-authorize long-lived connections on the assertion lifetime and close on
  failure. Leaving them open would have made a websocket the one way to hold access indefinitely
  after revocation — precisely the property an attacker looks for. Use the *same* constant as the
  assertion lifetime, not a copy of its value.
