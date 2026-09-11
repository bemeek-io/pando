# Sketch — a Cloudflare routing adapter

**Not a commitment to build this.** A paper exercise required by the risk register before the routing
interface is fixed in phase 3, because the named risk is that the interface turns out to be secretly
Docker- and Traefik-shaped. Ten minutes now is cheaper than discovering it in phase 10.

The question is only: **does `RoutingAdapter` survive contact with a provider that works nothing like
Traefik?**

## What Cloudflare actually does

A Cloudflare Tunnel install has no inbound port at all. `cloudflared` runs beside Pando and dials
*out* to Cloudflare's edge; traffic arrives through that outbound connection. Routing is configured by
API calls against a tunnel's ingress rules, plus DNS records Cloudflare owns. TLS terminates at the
edge, not on the host.

So it differs from Traefik on almost every axis that might have leaked into the interface: no local
config file, no listening port, no certificate on the host, no reload, and configuration applied by
remote API rather than by writing to disk.

## Walking the interface

| Method | Holds? | Note |
|---|---|---|
| `Capabilities()` | ✅ | `Modes: [subdomain]`, `SupportsTLS: true`, `SupportsWildcardTLS: true`, `RequiresPublicReachability: false` — the tunnel dials out, so nothing needs to be publicly reachable. This is exactly the case the field was added for. |
| `Ensure(RouteRequest)` | ✅ | Creates or updates an ingress rule and a DNS record. `ProxyUpstream` becomes the rule's service target — `http://pando-proxy:8080`. Idempotent by nature: the API is declarative. |
| `Remove(RouteHandle)` | ✅ | Delete the ingress rule and DNS record. `RouteHandle` carries the tunnel ID and rule ID. |
| `Observe(RouteHandle)` | ✅ | Read the rule back and report whether it matches. Genuinely useful here — Cloudflare state can drift from a dashboard edit, which is exactly the drift `Observe` exists to report. |
| `HealthCheck()` | ✅ | Can we reach the Cloudflare API with the configured token, and is the tunnel connected. |
| `Configure(json.RawMessage)` | ✅ | Account ID, zone ID, tunnel ID, API token. |

**The interface holds.** No method needed changing, and no Traefik-shaped assumption surfaced.

## Two things the sketch did surface

**1. `ProxyUpstream` must be reachable from wherever the adapter's data plane runs, not from Pando.**
With Traefik the proxy address is on the same host. With a tunnel, `cloudflared` connects to
`ProxyUpstream` from its own container — which is fine on the same Compose network, and not fine if
someone runs `cloudflared` elsewhere. The field's *meaning* is right; its documentation should say
"an address the routing adapter's data plane can reach", not "Pando's address". That is a comment
change, not an interface change.

**2. TLS is already per-adapter, and this confirms it should stay that way.** Cloudflare issues and
terminates certificates itself. `TLSRequest` has to be advisory — an intent the adapter may satisfy
however it likes, or ignore because the edge already did it — rather than a set of instructions. This
is O-5 resolving the way the design assumed, and it is the second adapter to want it.

## Conclusion

No interface change. Record both notes above in `03-adapter-interfaces.md` §4 and proceed.

The risk this exercise was meant to catch — a routing abstraction shaped around writing local config
files and reloading a daemon — did not materialize, because `Ensure`/`Remove`/`Observe` describe
*intent* rather than mechanism. Worth remembering when someone proposes adding a `Reload()` or a
`ConfigPath` to this interface: the reason it survived is that it has neither.
