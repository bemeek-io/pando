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

**[O-13]** Behavior when a session is revoked mid-websocket. A long-lived connection authorized once stays open indefinitely. Options: periodic re-authorization on a timer with connection close on failure, or accept it and document the window. Not resolved.

---

## 5. Verb catalog

```go
var Verbs = []Verb{
    "app.view", "app.logs.read", "app.deploy", "app.restart",
    "app.spec.edit", "app.secrets.write", "app.secrets.read",
    "app.exec", "app.grants.manage", "app.routing.override",
    "app.resources.override", "app.delete",
}
```

**[D]** Built-in roles (R-081) are seeded by migration and trigger-protected. When a new verb is introduced in a later Pando version, a migration adds it to the appropriate built-in roles. That is the upgrade mechanism R-081 promises, and it is the only sanctioned way built-in role contents change.

**[D]** Custom roles (R-082) are arbitrary subsets. There is no verb implication graph — holding `app.delete` does not imply `app.view`. Implication graphs are where authorization bugs live; the console can suggest sensible combinations instead.

---

## 6. Audit integration

**[D]** Every authorization **denial** is audited, not only successes. A denial pattern is the signal that matters for detecting misuse, and it is the thing most commonly left out.

**[D]** Audit is written before the privileged action, not after (§04 2.6). An exec session that fails to open is still recorded as attempted.

**[D]** Database-level enforcement: the application role has `INSERT` on `audit_events` and no `UPDATE` or `DELETE`. R-027 says no adapter can rewrite the audit log; this makes it true of core as well, which is stronger and costs nothing.
