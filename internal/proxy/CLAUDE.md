# The proxy — read this before changing anything here

Design: `docs/design/06-authorization-and-proxy.md` §4, and Sequence C in `docs/design/07-sequences.md`.

The proxy is **the single enforcement point for every request to every app** (R-023). One path for
every request: authenticated, anonymous, proxy mode, per-domain mode. This is the hottest path in the
system and the one where a mistake is worst.

## The proxy is ours, and nothing routes around it

Pando implements the identity-aware proxy itself on `httputil.ReverseProxy`. Delegating the
authorization decision to an external proxy would put it outside core, violating R-027.

Routing adapters place traffic **in front of** this proxy. They never reach the workload. There is
**no bypass** — not for public apps (R-075), not for performance, not for websockets. If you are
adding a fast path, you are adding a security hole.

## Request path

```
 1. Resolve app from Host header, or path prefix in proxy mode
 2. App exists and running?            → 404 / 503
 3. Session cookie or bearer token
 4. Authenticate → Principal, or anonymous
 5. CheckData(principal, app)
      denied + anonymous → 302 login
      denied + authed    → 403
 6. Mint assertion (R-051)
 7. STRIP all inbound X-Pando-* headers        ← see below
 8. Set assertion + convenience headers
 9. Path mode: strip prefix, set X-Forwarded-Prefix (R-167)
10. Forward, stream unbuffered
```

## Step 7 is a security requirement, not hygiene

Any inbound header in Pando's namespace **must be stripped unconditionally** before step 8. Without
it, a client sets `X-Pando-User: admin@corp.com` and any app trusting the convenience headers is
trivially spoofed.

**This is the single most likely serious bug in the proxy.** It needs a test asserting that a request
arriving with forged `X-Pando-*` headers reaches the app with them *replaced*, never passed through.

## Assertions

`X-Pando-Assertion`, Ed25519-signed, 120s lifetime [P], minted per request (R-055).

Claims: `sub` (= `users.id`, stable across email change and independent of the adapter — apps key
their data on it, R-054), `email`, `name`, `groups`, `aud` (= app ID, prevents cross-app replay),
`iat`, `exp`, `iss`.

Anonymous requests still get an assertion, with `sub: "anonymous"`, a constant (R-056). The
consequence must be stated explicitly in app-developer documentation: **absence of the assertion
header means the request did not come through Pando**, and an app may reject on that basis.

Convenience headers `X-Pando-User`, `X-Pando-Email`, `X-Pando-Groups` ship alongside (R-053) and are
documented as **unverified**. An app trusting them is trusting the network boundary — legitimate, but
it must be a choice the developer knows they are making.

Keys are published at `/.well-known/jwks.json` with key IDs and rotated on overlap (R-057): generate
new, publish both, sign with the new one after a propagation window, retire the old.

## Streaming

R-170. Websockets, SSE, and large uploads must work. Concretely: no response buffering, `Flush()` on
every write for SSE, hijack for the websocket upgrade, and **no default body size limit**.

**[O-13] unresolved:** what happens when a session is revoked mid-websocket. A long-lived connection
authorized once currently stays open indefinitely. Either re-authorize on a timer and close on
failure, or accept it and document the window. Do not silently pick one — see
`docs/plan/open-decisions.md`.

## Tests this package owes

- A forged `X-Pando-User` header arrives at the app replaced.
- An anonymous request to a public app traverses every step and gets `sub: "anonymous"` — assert the
  audit/metrics counter increments, proving no bypass path exists.
- An assertion minted for app A fails verification at app B (`aud` mismatch).
- Removing a user from a group revokes access within the documented cache TTL, without a redeploy.
- A websocket upgrade streams bidirectionally.
- SSE responses are not buffered.
