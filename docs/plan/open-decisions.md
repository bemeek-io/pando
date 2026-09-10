# Open decisions

Fourteen unresolved questions. O-1 through O-10 come from requirements §23; O-11 through O-14 were
added during design.

**These are not TODOs to resolve at your discretion.** An agent hitting one of these should raise it,
state which options the docs already identify, and stop — not pick quietly and move on. Record any
resolution both here and in the requirements or design doc that owns it.

## Blocking

| ID | Question | Blocks | Owner doc |
|---|---|---|---|
| **O-11** | **How Postgres is supplied** — bundled container, bring-your-own, or embedded binary | **Phase 0** | design 00 §1.1 |

O-11 is the one that must be decided before any other work. R-253 says Pando ships as a single binary
and R-002 says setup cost is paid once; requiring an operator to stand up Postgres first adds a
prerequisite to the hobbyist path that Pando exists to eliminate. Three ways out, each with a real
cost:

- **Bundled Postgres** — `pando install` starts a container Pando manages, on the same runtime adapter
  it uses for apps. One command, no prerequisite. Cost: Pando's own state depends on the runtime
  adapter being healthy, which complicates bootstrap ordering and DR (**and creates O-14**).
- **Bring your own** — connection string at install. Clean separation, worse first-run experience.
- **Embedded Postgres binary** — unpacked and supervised by Pando itself. No container dependency,
  adds ~100 MB and platform-specific binaries.

Everything downstream assumes Postgres regardless; **only the install path changes.**

## Phase-scoped

Each of these can be resolved in the phase that needs it, but must be resolved *before* that phase
ships — not after.

| ID | Question | Phase | Owner doc |
|---|---|---|---|
| **O-4** | Required vs optional slot detection — the forty-key `.env.example` problem | 6 | design 01 §2.5 |
| **O-12** | Whether the MCP exclusion list is hard or policy-controlled | 10 | design 04 §3 |
| **O-13** | Session revocation mid-websocket | 5 | design 06 §4.2 |
| **O-14** | DR restore bootstrap ordering when Pando's own Postgres is a managed container | 9 | design 07 D |

**O-4** has a [P] fallback that preserves R-103: default `Required: false` for anything not typed to a
known service, and let the trial run settle it — a slot whose absence crashes the trial run is
promoted to required with the crash log as evidence. This turns an unanswerable question into an
observation. Measure its false-block rate against the detection corpus rather than assuming it works.

**O-14 is coupled to O-11.** It only exists if O-11 resolves to the bundled option. Resolve them
together.

## Deferred by design

These are open because deferring them is the right answer for now, not because nobody has looked.

| ID | Question | Note |
|---|---|---|
| **O-1** | Identity linking across adapters | If it lands, it is a `user_identities` join table and `users.adapter_id`/`external_id` move there. Nothing else restructures. |
| **O-2** | Per-adapter session lifetime and revocation | Deliberately deferred to each adapter's `SessionPolicy` (R-047). This is the answer, not an absence of one. |
| **O-3** | Private repo credential ownership | App-owned or user-owned. User-owned dies at offboarding — that is the tension. |
| **O-5** | TLS issuance | Per-adapter: ACME, wildcards, local self-signed. |
| **O-6** | Backup destination | Local-only is useless for disk failure. Needed by phase 9. |
| **O-7** | Exec command recording | Session-only audit, or full command capture. |
| **O-8** | Runtime adapter swap under a running app | Migration or redeploy. |
| **O-9** | Share notifications | Does being granted access notify you. |
| **O-10** | Retroactive policy application | Newly-violating running apps: block, force, or report. Touches R-274 and the spec's `ModeSource` field, which exists to make this answerable. |

## Adding one

If you hit a question the docs do not answer, add a row here rather than deciding in code. Include:
the question, what depends on it, the options you can see, and which requirement or design section
would need to change for each. A question recorded with its options is most of the work of answering
it.
