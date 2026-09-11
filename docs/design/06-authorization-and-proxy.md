# 06 — Authorization and the Proxy

Two planes, evaluated separately, never conflated. This is the most security-sensitive code in the system and the part most likely to be quietly broken by a later refactor.

---

## 1. Principals

```go
type Principal struct {
    Kind        PrincipalKind  // user | token | anonymous | system
    ID          string
    UserID      string   // delegated token: the owner (R-059). Same as ID for users.
    TokenID     string
    Groups      []string // resolved live (R-079)
    AdapterID   string
}
```

**[D]** A delegated token resolves to a `Principal` with `Kind: token` and `UserID` set to its owner. Authorization then runs against `UserID` exactly as if the user made the request (R-058, R-059). No grant is ever written for a delegated token.

**[D]** An account token resolves to `Kind: token`, `UserID` empty, and is looked up in grants under its own ID (R-060).

**[D]** `system` exists for the reconciler and background jobs. It bypasses grant checks but **still writes audit events**, attributed to `system`. Anything a background job does must be as visible as anything a person does.

---

## 2. Evaluation order

**[D]** Fixed order. Each step can only deny; none can restore access denied by an earlier step.

```
1. Authenticate            → AUTH_* on failure
2. Principal status check  → suspended/deleted principals denied (R-049)
3. Token validity          → expired, revoked → AUTH_TOKEN_INVALID
4. Token derivation        → owner suspended/deleted → AUTH_TOKEN_ORPHANED (R-059)
5. Host policy             → POLICY_* (e.g. exec disabled install-wide, R-085)
6. Grant lookup            → PERM_* if no matching grant
7. Verb check              → PERM_VERB_REQUIRED (control plane only)
```

**[D]** Policy is evaluated **before** grants (step 5). A policy that disables exec install-wide denies the owner too. Policy is a floor, not an override (R-272).

**[D]** Step 4 is what makes R-059 real. It is a live lookup on every request, not a cascade run at revocation time. Slower, and correct — a cascade means a missed cascade is a permanent security hole.

```go
func (a *Authorizer) CheckControl(ctx context.Context, p Principal, appID string, verb Verb) error {
    if err := a.checkPrincipal(ctx, p); err != nil { return err }
    if err := a.policy.Allows(ctx, verb, appID); err != nil { return err }

    grants := a.state.ControlGrantsFor(ctx, appID, p)  // user, groups, token
    for _, g := range grants {
        if a.roles.Has(g.RoleID, verb) {
            return nil
        }
    }
    return ErrPermission(verb)
}

func (a *Authorizer) CheckData(ctx context.Context, p Principal, appID string) error {
    if err := a.checkPrincipal(ctx, p); err != nil { return err }

    if a.state.IsOwner(ctx, appID, p.UserID) {
        return nil   // R-072: the sole implication between planes
    }
    if a.state.HasDataGrant(ctx, appID, p) {
        return nil
    }
    if a.state.HasAnonymousGrant(ctx, appID) {
        return nil   // R-075
    }
    return ErrPermission("app.use")
}
```

**[D]** `CheckData` contains exactly one cross-plane implication — ownership (R-072). No other control-plane role appears in it. A reviewer seeing another control-plane check added to this function should reject the change; that is R-029 and it was reversed once already during design, so it needs a comment saying so in the code.

**[D]** Being a Pando admin does not appear in `CheckData` at all. R-087: an admin has root and can reach a container outside Pando, but the supported path requires a grant.

---

## 3. Group resolution

**[D]** Groups are resolved live per request (R-079), never denormalized into grants.

**[P]** Cached per session with a short TTL (60s), invalidated immediately on a SCIM push (R-048). The TTL is the effective propagation delay for a group removal on adapters without push, and must be documented as such rather than implied to be instant.

### 3.1 The revocation window

**[D]** Access does not stop the instant it is revoked, and the design contains four separate delays
that were each chosen locally and never added up:

| Source | Delay | Effect |
|---|---|---|
| Session validity check | none by default — one indexed lookup per request (§02 2.7) | 0 |
| Group membership cache | 60s, or 0 on a SCIM push (R-048) | up to 60s |
| Assertion lifetime | 120s (R-055) | up to 120s, if the app caches it for its full life |
| Long-lived connections | re-authorized on an interval (§4.2) | up to that interval |

**The effective revocation window is the largest of these, not the smallest.** Revoking a grant while
an app holds a 120-second assertion means the app may honor that assertion for its remaining life.
Nothing in the system was computing this number, and each component's documentation implied its own
delay was the answer.

**[D] One number: 120 seconds.** Everything above is set to that or below it, and the long-lived
connection interval is set to *exactly* the assertion lifetime rather than to an independently chosen
value — two clocks measuring the same thing will drift apart the first time someone tunes one of them.
The 60s group cache stays where it is because a value below the window does not widen it; if it is
ever raised, it must not be raised past 120.

**[D]** The console displays this window wherever access is revoked — removing a grant, suspending a
user, removing someone from a group — as a plain statement that access stops within two minutes.
Implying revocation is instant is the failure mode here, and §03 5's per-adapter `SessionPolicy`
display must show the effective window rather than only the adapter's own lifetime.

**[D]** The window is a floor on Pando's side, not a promise about the app. An app that caches
identity from an assertion for longer than the assertion's life has extended the window itself, which
is one more reason the app-developer documentation states that assertions are per-request and short.

**[P]** If the session check is ever cached for throughput, its TTL joins this table and the window is
recomputed. It does not get to be a hidden fifth delay.

---

## 4. The proxy

The single enforcement point (R-023). One path for every request to every app — authenticated, anonymous, proxy mode, per-domain mode.

```
1.  Resolve app from hostname or path prefix
2.  App exists and is running?             → 404 / 503
3.  Extract session cookie or bearer token
4.  Authenticate → Principal, or anonymous
5.  CheckData(principal, app)
      denied + anonymous  → redirect to login
      denied + authed     → 403 page
6.  Mint assertion (R-051)
7.  Strip inbound X-Pando-* headers        ← critical, see below
8.  Set assertion + convenience headers
9.  Strip path prefix, set X-Forwarded-Prefix (R-167)
10. Forward to the workload
11. Stream response
```

**[D] Step 7 is a security requirement, not hygiene.** Any inbound header in Pando's namespace must be stripped unconditionally before step 8. Without it, a client sets `X-Pando-User: admin@corp.com` and an app trusting the convenience headers (R-053) is trivially spoofed. This is the single most likely serious bug in the proxy, and it needs a test asserting that a request with forged headers arrives with them replaced.

**[D]** The proxy never routes around itself. Routing adapters place traffic in front of it (§00 1.3, §03 4). There is no bypass for public apps (R-075), no bypass for performance, no bypass for websockets.

**[D] How that is made structural, discovered while building it.** Every app sits on its own private
network so no app can reach another (R-025), and no workload publishes a host port (R-026). That
leaves no address at which an app can be reached — except from inside its own network. The runtime
adapter therefore attaches *Pando's own container* to each bundle network as it creates it.

So the set of networks Pando belongs to **is** the set of apps it can reach, and nothing else is
joined to any of them. R-023 stops being a promise the proxy keeps and becomes a property of the
topology: there is no route to an app that does not pass through enforcement, because there is no
route to an app at all.

Attaching is the adapter's job rather than core's — how a workload becomes reachable is exactly the
provider vocabulary core must never learn (R-251).

**[P]** The cost is a private network per app, and a container runtime has a finite supply. Docker's
default pool holds about thirty, so an install past that size needs `default-address-pools` widened
before it can start another app. The adapter turns that refusal into a `CAPACITY_*` error naming the
setting, because the daemon's own message talks about subnets and tells an operator nothing.

### 4.1 Assertion minting

```go
type Assertion struct {
    Sub    string   `json:"sub"`     // users.id, stable (R-054)
    Email  string   `json:"email,omitempty"`
    Name   string   `json:"name,omitempty"`
    Groups []string `json:"groups,omitempty"`
    Aud    string   `json:"aud"`     // app ID — prevents cross-app replay
    Iat    int64    `json:"iat"`
    Exp    int64    `json:"exp"`
    Iss    string   `json:"iss"`
}
```

**[P]** Header: `X-Pando-Assertion`. Lifetime 120 seconds (R-055), minted per request.

**[D]** Anonymous requests get an assertion with `sub: "anonymous"`, a constant (R-056). Consequence, which the app-developer documentation must state explicitly: **absence of the assertion header means the request did not come through Pando**, and an app may reject on that basis.

**[D]** Convenience headers `X-Pando-User`, `X-Pando-Email`, `X-Pando-Groups` are sent alongside (R-053) and documented as **unverified**. An app trusting them is trusting the network boundary, which is legitimate but must be a choice the developer knows they are making.

**[P]** Keys: Ed25519, published at `/.well-known/jwks.json` with key IDs, rotated on overlap (R-057). Rotation generates a new key, publishes both, signs with the new one after a propagation window, retires the old.

### 4.2 Streaming

**[D]** R-170: websockets, SSE, and large uploads must work. Concretely — no response buffering, `Flush()` on every write for SSE, hijack for websocket upgrade, and no default body size limit.

**[D] Resolved (O-13).** A long-lived connection is re-authorized on a timer and closed when
authorization fails. The interval is the assertion lifetime — 120s, the same number as §3.1 — because
a long-lived connection is the one case where the per-request check that normally enforces `CheckData`
never fires again. Accepting the alternative (leave it open, document the gap) would have made a
websocket the one way to hold access indefinitely after revocation, which is precisely the property an
attacker would look for.

Re-authorization runs the same `CheckData` as a fresh request. On failure the connection closes with a
normal WebSocket close frame carrying a policy-violation status, not an abrupt reset, so a client can
tell revocation from a network fault.

---

## 5. Verb catalog

```go
var Verbs = []Verb{
    "app.view", "app.logs.read", "app.deploy", "app.restart",
    "app.spec.edit", "app.secrets.write", "app.secrets.read",
    "app.exec", "app.grants.manage", "app.routing.override",
    "app.resources.override", "app.egress.override", "app.delete",
}
```

**[D]** The three `*.override` verbs form a set: routing, resources, and egress. Each one permits
deviating from a default the host operator chose, which is why none of them is in Operator and all
three sit with Owner. `app.egress.override` is what R-184 requires — an app-level allowlist
**replaces** the install-wide list rather than narrowing it (R-182, R-183), so defining one is an
escalation and has to be gated. Adding it to the catalog without gating it would make the install-wide
list advisory.

**[D]** Built-in roles (R-081) are seeded by migration and trigger-protected. When a new verb is introduced in a later Pando version, a migration adds it to the appropriate built-in roles. That is the upgrade mechanism R-081 promises, and it is the only sanctioned way built-in role contents change.

**[D]** Custom roles (R-082) are arbitrary subsets. There is no verb implication graph — holding `app.delete` does not imply `app.view`. Implication graphs are where authorization bugs live; the console can suggest sensible combinations instead.

---

## 6. Audit integration

**[D]** Every authorization **denial** is audited, not only successes. A denial pattern is the signal that matters for detecting misuse, and it is the thing most commonly left out.

**[D]** Audit is written before the privileged action, not after (§04 2.6). An exec session that fails to open is still recorded as attempted.

**[D]** Database-level enforcement: the application role has `INSERT` on `audit_events` and no `UPDATE` or `DELETE`. R-027 says no adapter can rewrite the audit log; this makes it true of core as well, which is stronger and costs nothing.
