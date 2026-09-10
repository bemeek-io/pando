# Open decisions

Fourteen questions. O-1 through O-10 come from requirements §23; O-11 through O-14 were added during
design. **O-11 is resolved and nothing is blocking.**

**These are not TODOs to resolve at your discretion.** An agent hitting one of these should raise it,
state which options the docs already identify, and stop — not pick quietly and move on. Record any
resolution both here and in the requirements or design doc that owns it.

## Resolved

| ID | Question | Resolution |
|---|---|---|
| **O-11** | How Postgres is supplied | Supplied by the install topology: a Compose file with a `pando` service and a `postgres` service that start together, plus an external-database override in config. Design 00 §1.1. |

The three options originally on the table were bundled-container, bring-your-own, and embedded
binary. The resolution is a fourth that takes bundled-container's experience without its cost, and
the distinction is worth keeping in mind because it is easy to collapse: **Pando does not start
Postgres — the install topology does.** Pando connects to a database already coming up beside it,
exactly as it would to an external one, so it needs no runtime adapter to reach its own state store.

Two consequences:

- **[O-14] largely dissolves.** It existed only because a Pando-managed Postgres container would have
  had to be started during DR restore before there was a state store to say how. With Compose, the
  database comes up with the topology and restore writes into it. What remains is ordinary sequencing
  in the restore path, not a bootstrap paradox.
- **The external-database path needs a privilege contract.** The audit guarantee (R-027) is a database
  grant, and creating a restricted application role requires administrative rights that Pando has on a
  fresh cluster and may not have on someone else's. That path must document the privileges it needs
  and verify them at startup, failing loudly. See design 00 §1.1 and phase 0.

## Phase-scoped

Each of these can be resolved in the phase that needs it, but must be resolved *before* that phase
ships — not after.

| ID | Question | Phase | Owner doc |
|---|---|---|---|
| **O-4** | Required vs optional slot detection — the forty-key `.env.example` problem | 6 | design 01 §2.5 |
| **O-12** | Whether the MCP exclusion list is hard or policy-controlled | 10 | design 04 §3 |
| **O-13** | Session revocation mid-websocket | 5 | design 06 §4.2 |
| **O-14** | DR restore bootstrap ordering — largely dissolved by O-11; confirm in phase 9 | 9 | design 07 D |

**O-4** has a [P] fallback that preserves R-103: default `Required: false` for anything not typed to a
known service, and let the trial run settle it — a slot whose absence crashes the trial run is
promoted to required with the crash log as evidence. This turns an unanswerable question into an
observation. Measure its false-block rate against the detection corpus rather than assuming it works.

**O-14 is largely dissolved by O-11's resolution.** It existed only under the bundled-container
option. With Compose supplying the database, restore writes into a Postgres that the topology has
already brought up. Confirm during phase 9 that nothing else in the restore path depends on reading
adapter configuration before the database is available.

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
