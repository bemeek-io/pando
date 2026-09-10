#!/usr/bin/env python3
"""One-shot: write a doc.go into each scaffolded package.

Each doc.go states the package's contract and cites the design section that owns it, so the
constraint lives next to the code rather than only in docs/. Run once at scaffold time; after
that, edit the files directly.
"""

from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent

# path -> (package name, doc comment lines)
PACKAGES: dict[str, tuple[str, list[str]]] = {
    "internal/core/authz": ("authz", [
        "Package authz evaluates the two authorization planes.",
        "",
        "Control plane is role-scoped and governs managing an app. Data plane is binary and",
        "governs using one. They are never conflated: CheckData contains exactly one cross-plane",
        "implication, ownership (R-072), and this was reversed once during design.",
        "",
        "Evaluation order is fixed and each step can only deny. See design 06 §2 and the package",
        "CLAUDE.md before changing anything here.",
    ]),
    "internal/core/audit": ("audit", [
        "Package audit writes the append-only event log.",
        "",
        "Append-only is enforced at the database level: the application role holds INSERT on",
        "audit_events and neither UPDATE nor DELETE (R-027). Every authorization denial is audited,",
        "not only successes, and the event is written before the privileged action, not after —",
        "an exec session that fails to open is still recorded as attempted (R-228).",
    ]),
    "internal/core/assertion": ("assertion", [
        "Package assertion mints the identity assertions the proxy passes to apps, and publishes",
        "the JWKS that verifies them.",
        "",
        "Ed25519, aud bound to the app ID to prevent cross-app replay, 120s lifetime, minted per",
        "request (R-051, R-055). The sub claim is users.id, stable across email change and",
        "independent of the identity adapter — apps key their data on it (R-054).",
    ]),
    "internal/core/spec": ("spec", [
        "Package spec defines the AppSpec: the sole record of how an app runs.",
        "",
        "Nothing is read from the repo at deploy time (R-020). Specs are immutable and versioned;",
        "editing produces a new revision and rollback is repointing (R-152). A spec carries no",
        "policy, no grants, and no secret values, which is what makes export safe to hand to",
        "someone. See design 01.",
    ]),
    "internal/core/planner": ("planner", [
        "Package planner turns a spec plus host policy plus adapter capabilities into a bundle",
        "plan, or into a plan-time error.",
        "",
        "Everything the planner does is side-effect-free. That boundary is what makes a plan-time",
        "failure meaningful rather than a label on a mid-deploy crash: steps 1-7 of the deployment",
        "pipeline create nothing. See design 05 §3.",
    ]),
    "internal/core/reconciler": ("reconciler", [
        "Package reconciler runs the loop that converges observed state toward pinned specs.",
        "",
        "The dividing line: the reconciler may create and start things; it may not destroy anything",
        "a human may have wanted (R-148, R-028). A failed app stays failed — R-151 is true because",
        "there is no code path here that touches the failed state, not because a flag is checked.",
        "See design 05.",
    ]),
    "internal/core/policy": ("policy", [
        "Package policy evaluates host policy.",
        "",
        "Policy is a floor, not an override (R-272), and is evaluated before grants — a policy that",
        "disables exec install-wide denies the owner too. It is a single versioned document rather",
        "than scattered columns, so applying policy to a running install is one transaction and one",
        "audit event (R-274).",
    ]),
    "internal/core/state": ("state", [
        "Package state holds sqlc-generated queries and repository types.",
        "",
        "This store is the sole record of how every app runs (R-020), which sets the bar: every",
        "mutation is audited, every object is exportable, nothing important lives only in memory.",
        "Several requirements are enforced by database constraints rather than by code — see the",
        "package CLAUDE.md before changing the schema.",
    ]),
    "internal/adapter/api": ("api", [
        "Package api defines the seven adapter interfaces. Definitions only, no implementations.",
        "",
        "The app declares requirements; adapters translate (R-250). Core never learns a provider's",
        "vocabulary (R-251). If a Docker-shaped concept appears in a signature here, the design has",
        "failed. Everything is compiled in-tree — these are ordinary Go interfaces, freely",
        "refactorable, and there is no wire protocol. See design 03.",
    ]),
    "internal/adapter/identity/local": ("local", [
        "Package local implements the local identity adapter: username and password, argon2id.",
        "",
        "Identity adapters authenticate only (R-044). Subject carries no roles, no verbs, and no",
        "permissions — group names cross the boundary, but what a group can do is Pando's (R-078).",
    ]),
    "internal/adapter/routing/loopback": ("loopback", [
        "Package loopback implements port-mode routing with no TLS: the laptop default.",
        "",
        "Like every routing adapter, it makes traffic arrive at Pando's proxy and never reaches the",
        "workload (R-023).",
    ]),
    "internal/adapter/routing/traefik": ("traefik", [
        "Package traefik implements subdomain and path routing with TLS.",
        "",
        "Built last, deliberately: it is the second implementation of the routing interface, and",
        "building it is the test of whether that abstraction holds. If Traefik requires changing the",
        "interface, the interface was wrong.",
        "",
        "The generated config points at Pando's proxy, never at the workload — the workload address",
        "is right there and would appear to work, which is exactly the trap.",
    ]),
    "internal/adapter/builder/buildkit": ("buildkit", [
        "Package buildkit implements the builder adapter: rootless, containerized, no socket.",
        "",
        "A builder must never receive or request a container runtime socket (R-112). The type system",
        "cannot enforce this; an integration test asserting the build container's mount list does.",
    ]),
    "internal/adapter/runtime/docker": ("docker", [
        "Package docker implements the runtime adapter for local containers.",
        "",
        "Observe reports facts and never remediates — an adapter that silently restarts things makes",
        "drift undetectable and breaks the reconciler's report path. Capacity is adapter-reported;",
        "core does not read /proc and has no concept of a host (R-243).",
    ]),
    "internal/adapter/secrets/local": ("local", [
        "Package local implements the secrets adapter, encrypted at rest with the key on disk (R-190).",
        "",
        "Values cross this boundary wrapped in secret.Value, which refuses to render (R-194). No",
        "plaintext column exists anywhere in the schema.",
    ]),
    "internal/adapter/services/docker": ("docker", [
        "Package docker provisions services that fill declared slots: postgres, mysql, redis.",
        "",
        "A provisioned service joins the app's private bundle. It is not exposed, not addressable",
        "from outside, and not shareable with another app (R-134) — sharing is expressed as two apps",
        "binding to one external target.",
    ]),
    "internal/adapter/notify/console": ("console", [
        "Package console implements the v1 notification adapter (R-231).",
        "",
        "The interface exists so SMTP and hosted senders drop in later without touching core.",
    ]),
    "internal/detect": ("detect", [
        "Package detect runs the detector auction and the trial run.",
        "",
        "Ask, never guess (R-102). Every builder adapter bids; the winner emits a draft spec with",
        "evidence and questions. Questions are held to R-105: answerable by a model that cannot see",
        "the repo, because the intended workflow is pasting them into the assistant that wrote the",
        "app. The trial run turns unanswerable questions into observations — watch what the app",
        "binds rather than asking (R-097). See design 07 sequence A.",
    ]),
    "internal/proxy": ("proxy", [
        "Package proxy is the identity-aware reverse proxy: the single enforcement point for every",
        "request to every app (R-023).",
        "",
        "There is no bypass — not for public apps, not for performance, not for websockets. Inbound",
        "X-Pando-* headers are stripped unconditionally before assertion headers are set; without",
        "that, a client sets X-Pando-User and any app trusting the convenience headers is trivially",
        "spoofed (R-053). Read the package CLAUDE.md before changing anything here.",
    ]),
    "internal/httpapi": ("httpapi", [
        "Package httpapi is the REST surface: chi handlers and nothing else.",
        "",
        "The API is the product (R-261). Handlers contain no business logic — it lives in a service",
        "layer under internal/core that both httpapi and mcp call. A capability that exists in one",
        "surface and not the other means someone put logic in a handler.",
    ]),
    "internal/mcp": ("mcp", [
        "Package mcp exposes Pando's service layer as MCP tools.",
        "",
        "An agent holds a token and is a principal like any other (R-262). No tool bypasses",
        "authorization, and every action lands in the audit log under the token's owner. Exec, secret",
        "value reads, grant mutation, policy mutation, and user deletion are deliberately not exposed.",
    ]),
    "internal/console": ("console", [
        "Package console embeds the built console assets into the binary (R-253).",
        "",
        "The React source lives in the top-level console/ directory and is built into this package's",
        "dist/ by `make console`. The two directories are different things and should not be merged.",
    ]),
    "internal/secret": ("secret", [
        "Package secret provides Value, a wrapper that refuses to render.",
        "",
        "String, MarshalJSON, and MarshalLogObject all return [redacted]. This is how R-194 is",
        "enforced structurally rather than by review: a secret cannot be accidentally logged because",
        "the type will not print. All three must redact — a type that redacts in String but not in",
        "MarshalJSON will leak through an error envelope.",
    ]),
    "internal/id": ("id", [
        "Package id generates prefixed, sortable, opaque identifiers: app_01HQ8..., spec_..., usr_...",
        "",
        "ULID body. The prefix is load-bearing — it makes log lines self-describing and copy-paste",
        "mistakes visible, which is why IDs are text rather than uuid in the schema.",
    ]),
    "internal/errs": ("errs", [
        "Package errs defines the error envelope crossing every API boundary.",
        "",
        "A stable machine code, a human message, and where relevant a remediation hint. Message text",
        "is held to the R-105 standard wherever a user might act on it: self-contained, pasteable",
        "into an assistant, no undefined terms. See design 00 §3.2 for the code taxonomy and the",
        "named codes the requirements promise.",
    ]),
    "internal/config": ("config", [
        "Package config loads configuration from YAML, environment, and flags (R-271).",
    ]),
}


def main() -> None:
    written = 0
    for rel, (pkg, doc) in PACKAGES.items():
        path = ROOT / rel / "doc.go"
        if path.exists():
            continue
        path.parent.mkdir(parents=True, exist_ok=True)
        body = "\n".join(f"// {line}".rstrip() for line in doc)
        path.write_text(f"{body}\npackage {pkg}\n")
        written += 1
    print(f"wrote {written} doc.go files")


if __name__ == "__main__":
    main()
