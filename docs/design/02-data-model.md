# 02 — Data Model

PostgreSQL. `sqlc` for typed queries, `golang-migrate` for versioning. No ORM.

R-020 makes this store the sole record of how every app runs, which sets the bar: every mutation is audited, every object is exportable, and nothing important lives only in memory.

---

## 1. Conventions

- IDs are `text`, prefixed ULIDs (§00 3.1). Not `uuid` — the prefix is load-bearing for readability.
- Timestamps are `timestamptz`, always UTC.
- Soft delete only where an object must survive its own deletion for audit purposes. Apps and users soft-delete; grants and sessions hard-delete.
- Every table with a mutable row carries `created_at`, `updated_at`.
- Specs are append-only. There is no `UPDATE` on `spec_revisions`.

---

## 2. Schema

### 2.1 Principals

```sql
CREATE TABLE identity_adapters (
    id            text PRIMARY KEY,          -- idp_...
    kind          text NOT NULL,             -- local | oidc | saml | github
    name          text NOT NULL,
    config        jsonb NOT NULL DEFAULT '{}',
    enabled       boolean NOT NULL DEFAULT true,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE users (
    id            text PRIMARY KEY,          -- usr_...
    adapter_id    text NOT NULL REFERENCES identity_adapters(id),
    external_id   text NOT NULL,             -- subject as the adapter knows them
    email         text,
    display_name  text,
    status        text NOT NULL,             -- active | suspended | deleted  (R-049)
    password_hash text,                      -- local adapter only, argon2id
    must_change_password boolean NOT NULL DEFAULT false,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),
    deleted_at    timestamptz,
    UNIQUE (adapter_id, external_id)
);
```

**[D]** `users.id` is what goes in the assertion `sub` claim (R-054). It is stable across email change and independent of the adapter's own identifiers. This is the single most important stability guarantee in the schema — apps key their data on it.

**[D]** `status` is three-valued because suspended is not deleted (R-049, R-282). Destruction rules (R-280) fire on `deleted`, never on `suspended`.

**[D] Resolved (O-1): linking aliases, it never merges.** No identity linking in v1. When it lands it
is a `user_identities` join table and `users.adapter_id`/`external_id` move there — but the constraint
that matters is what linking may *do*, and it has to be decided now because getting it wrong later is
unrecoverable.

Linking a second identity to a user attaches an alias. It does **not** merge two `users` rows, and
`users.id` never changes and is never retired. Merging is the obvious implementation and it breaks
R-054: `users.id` is the assertion `sub` claim, apps key their data on it, and Pando has no way to
reach into an app and rewrite the rows it stored under the losing ID. A merge would silently orphan a
person's data inside every app they had ever used.

So: an admin linking `alice@corp` (OIDC) to an existing local `alice` picks which `users.id` survives
as primary, every linked identity authenticates *to* that primary, and assertions always carry the
primary. The unlinked-from row is marked as an alias, never deleted — a deletion would free its
`external_id` for reuse by a different human.

```sql
CREATE TABLE groups (
    id            text PRIMARY KEY,          -- grp_...
    adapter_id    text REFERENCES identity_adapters(id),  -- NULL = Pando-native
    external_id   text,
    name          text NOT NULL,
    created_at    timestamptz NOT NULL DEFAULT now(),
    UNIQUE (adapter_id, external_id)
);

CREATE TABLE group_members (
    group_id      text NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    user_id       text NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    PRIMARY KEY (group_id, user_id)
);
```

**[D]** Membership is read live at authorization time (R-079). It is never denormalized into grants.

```sql
CREATE TABLE tokens (
    id            text PRIMARY KEY,          -- tok_...
    kind          text NOT NULL,             -- delegated | account   (R-058, R-060)
    name          text NOT NULL,
    hash          text NOT NULL,             -- argon2id of the secret; secret shown once (R-063)
    owner_user_id text REFERENCES users(id), -- delegated: required. account: NULL (R-060)
    created_by    text NOT NULL,             -- principal that minted it
    expires_at    timestamptz,               -- NULL = never; policy may forbid (R-061)
    last_used_at  timestamptz,               -- R-062
    revoked_at    timestamptz,
    created_at    timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX ON tokens (owner_user_id) WHERE revoked_at IS NULL;
```

**[D]** A delegated token has no grants of its own. Authorization resolves through `owner_user_id` live (R-059), so an owner's revocation is the token's revocation with no cascade to write.

**[D]** An account token is its own principal and therefore appears directly in `grants.principal_id` (R-060).

### 2.2 Roles and grants

```sql
CREATE TABLE roles (
    id            text PRIMARY KEY,          -- role_...
    name          text NOT NULL UNIQUE,
    builtin       boolean NOT NULL DEFAULT false,   -- R-081, immutable
    scope         text NOT NULL DEFAULT 'app',      -- app | install  (R-080)
    verbs         text[] NOT NULL,
    created_at    timestamptz NOT NULL DEFAULT now(),
    UNIQUE (id, scope)                              -- for the FK below
);
```

**[D]** Built-in rows (`viewer`, `operator`, `owner`, `administrator`) are seeded by migration and protected by a trigger against `UPDATE`/`DELETE` (R-081). New verbs added in a later Pando version are added to built-in roles **by migration**, which is the mechanism R-081 promises.

**[D]** A role is scoped. A role carrying install verbs granted on a single app is nonsense, and a role carrying app verbs granted install-wide is worse. `administrator` is the only install-scoped built-in; custom roles (R-082) are composed within one scope.

**[D]** The `UNIQUE (id, scope)` index is redundant as a uniqueness constraint — `id` is already the primary key — and exists solely so `grants` can reference the pair. It is the cheapest way to make the correspondence a foreign key instead of a convention.

```sql
CREATE TABLE grants (
    id            text PRIMARY KEY,          -- gr_...
    app_id        text REFERENCES apps(id) ON DELETE CASCADE,  -- NULL = install-scoped
    plane         text NOT NULL,             -- control | data   (R-070, R-071)
    role_scope    text NOT NULL DEFAULT 'app',                 -- app | install
    principal_kind text NOT NULL,            -- user | group | token | anonymous
    principal_id  text,                      -- NULL when kind = anonymous (R-074)
    role_id       text REFERENCES roles(id), -- control plane only
    created_by    text NOT NULL,
    created_at    timestamptz NOT NULL DEFAULT now(),
    UNIQUE (app_id, plane, principal_kind, principal_id),      -- NULLS NOT DISTINCT

    FOREIGN KEY (role_id, role_scope) REFERENCES roles (id, scope),
    CHECK ((role_scope = 'install' AND app_id IS NULL) OR
           (role_scope = 'app'     AND app_id IS NOT NULL)),
    CHECK (plane = 'control' OR app_id IS NOT NULL)
);
```

**[D]** A grant with **no app** is install-scoped (R-080, O-17). An administrator is a principal holding a grant, the same as everyone else; the grant simply has no app. There is no admin flag on a user, so the power is revocable and grantable like any other.

**[D]** `app_id NOT NULL` used to be what kept an app-scoped grant from becoming global. Its replacement is the composite foreign key plus the first CHECK: a role carries its scope, a grant carries the scope it was made at, and the two must agree. So "no app" and "carries install verbs" cannot come apart, whatever the application does.

**[D]** The second CHECK says a data grant always names an app. Data-plane use is per-app and binary (R-070) — there is no install-wide "use" — and that was previously implied by the NOT NULL.

**[D]** The unique index is `NULLS NOT DISTINCT`, which already existed so the anonymous grant (whose `principal_id` is NULL) could not be inserted twice. With a NULL `app_id` it does a second job for free: NULL compares equal to itself, so the index reads "one control grant per principal, install-wide". A combination of privileges is a custom role composed from the verb list (R-082), not two grants.

**[D]** Data-plane grants have no role — use is binary (R-070).

**[D]** App creation writes **two rows**, one per plane (R-073). They are independently revocable.

**[D]** `principal_kind = 'anonymous'` is a real row, not a flag on the app (R-075).

### 2.3 Apps and specs

```sql
CREATE TABLE apps (
    id             text PRIMARY KEY,          -- app_...
    name           text NOT NULL,
    slug           text NOT NULL UNIQUE,      -- used in routing
    owner_user_id  text REFERENCES users(id), -- R-031
    state          text NOT NULL,             -- see 05-reconciler
    pinned_spec_id text REFERENCES spec_revisions(id),
    desired_state  text NOT NULL,             -- running | stopped
    unobservable_since timestamptz,           -- adapter unreachable; NOT an app state
    applied_env_fingerprint text,             -- see 2.4, secret rotation
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now(),
    deleted_at     timestamptz
);

CREATE TABLE spec_revisions (
    id           text PRIMARY KEY,            -- spec_...
    app_id       text NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    revision     integer NOT NULL,
    origin       text NOT NULL,               -- detected | edited | redetected | imported | manual
    body         jsonb NOT NULL,              -- the AppSpec (01-spec-schema)
    created_by   text NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    UNIQUE (app_id, revision)
);
CREATE INDEX ON spec_revisions (app_id, revision DESC);
```

**[D]** Append-only. Enforced by a trigger rejecting `UPDATE` and `DELETE`, so R-152's rollback is always to something that provably existed.

**[D]** `unobservable_since` is a third field alongside `state` and `desired_state`, for the same
reason those two are separate: it answers a different question. `state` is what is true of the app;
`desired_state` is what a human asked for; `unobservable_since` is whether Pando currently knows
either. An adapter being unreachable is a platform problem, not an app state (§05 2), and folding it
into `state` would mean either lying — reporting `running` for an app nobody can see — or inventing an
`unknown` state that every consumer of the state machine then has to handle. The console renders it as
a banner over the app's last known state, not as a replacement for it.

**[P]** Pruning past `Retention.SpecRevisions` is a background job that deletes only revisions never pinned. A revision that was ever live is kept.

```sql
CREATE TABLE deployments (
    id           text PRIMARY KEY,            -- dep_...
    app_id       text NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    spec_id      text NOT NULL REFERENCES spec_revisions(id),
    trigger      text NOT NULL,               -- manual | branch_updated | release_tagged | rollback
    status       text NOT NULL,               -- pending|building|applying|succeeded|failed|superseded
    error_code   text,
    error_detail jsonb,
    started_at   timestamptz NOT NULL DEFAULT now(),
    finished_at  timestamptz,
    created_by   text NOT NULL
);
```

### 2.4 Volumes, secrets, services

```sql
CREATE TABLE volumes (
    id          text PRIMARY KEY,             -- vol_...
    app_id      text NOT NULL REFERENCES apps(id) ON DELETE RESTRICT,
    name        text NOT NULL,
    adapter_ref text NOT NULL,
    handle      text,                         -- adapter's own identifier
    created_at  timestamptz NOT NULL DEFAULT now()
);
```

**[D]** `ON DELETE RESTRICT`, deliberately. An app cannot be deleted out from under its volumes; the delete flow must resolve them explicitly through the keep-or-discard prompt (R-204).

```sql
CREATE TABLE secrets (
    id          text PRIMARY KEY,             -- sec_...
    app_id      text NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    key         text NOT NULL,
    adapter_ref text NOT NULL,                -- which secrets adapter holds it
    ciphertext  bytea,                        -- local adapter only
    external_ref text,                        -- external adapter: a pointer, not a value
    version     integer NOT NULL DEFAULT 1,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    UNIQUE (app_id, key)
);
```

**[D]** No plaintext column exists anywhere. The local adapter stores ciphertext; external adapters store only a reference (R-190, R-191).

**[D]** `version` increments on rotation so the reconciler can detect that a restart is required
(R-193). **The detection is state-side, not observed.** `Observe` returns no environment — see
§03 2.2 — so there is no way to see that a running workload holds a stale secret by looking at it. The
reconciler instead compares `apps.applied_env_fingerprint`, written at apply time, against the
fingerprint of the currently-resolved environment. A mismatch is reconcilable drift and the workload is
recreated.

**[D]** The fingerprint is a hash over `(key, version)` pairs and literal env values — **never over
secret values.** It has to be comparable without decrypting anything and must not become a place a
secret can leak into (R-194). Hashing the resolved values would put a verifier for every secret in the
state store, which is a worse position than not having the feature.

```sql
CREATE TABLE provisioned_services (
    id          text PRIMARY KEY,             -- svc_...
    app_id      text NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    slot_key    text NOT NULL,
    type        text NOT NULL,                -- postgres | redis | ...
    adapter_ref text NOT NULL,
    handle      text,
    created_at  timestamptz NOT NULL DEFAULT now()
);
```

**[D]** Scoped to one app (R-134). No sharing — sharing is expressed as two apps binding to one external target.

### 2.5 Policy and adapters

```sql
CREATE TABLE adapter_configs (
    id          text PRIMARY KEY,             -- rt_..., rte_..., bld_..., sec_..., ntf_...
    category    text NOT NULL,                -- runtime|routing|builder|secrets|services|identity|notify
    kind        text NOT NULL,                -- docker | traefik | buildkit | local | ...
    name        text NOT NULL,
    config      jsonb NOT NULL DEFAULT '{}',
    is_default  boolean NOT NULL DEFAULT false,
    enabled     boolean NOT NULL DEFAULT true,
    created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX ON adapter_configs (category) WHERE is_default;

CREATE TABLE host_policy (
    id          integer PRIMARY KEY DEFAULT 1 CHECK (id = 1),  -- singleton, R-015
    body        jsonb NOT NULL,
    updated_by  text NOT NULL,
    updated_at  timestamptz NOT NULL DEFAULT now()
);
```

**[D]** Singleton by constraint. One install, one org (R-015) — encode it so nobody accidentally builds multi-tenancy in.

**[D]** Policy is a single versioned document, not scattered columns, so R-274's "apply policy to a running install" is one transaction and one audit event.

### 2.6 Audit

```sql
CREATE TABLE audit_events (
    id            bigserial PRIMARY KEY,
    occurred_at   timestamptz NOT NULL DEFAULT now(),
    principal_kind text NOT NULL,             -- user | token | system | anonymous
    principal_id  text,
    on_behalf_of  text,                       -- delegated token: the owning user (R-229)
    action        text NOT NULL,              -- e.g. app.deploy, grant.create, exec.session
    app_id        text,
    target_kind   text,
    target_id     text,
    request_id    text,
    detail        jsonb NOT NULL DEFAULT '{}'
);
CREATE INDEX ON audit_events (app_id, occurred_at DESC);
CREATE INDEX ON audit_events (principal_id, occurred_at DESC);
```

**[D]** Append-only. `REVOKE UPDATE, DELETE` from the application role at the database level. R-027 says no adapter can rewrite the audit log; the enforcement should be a database grant, not a code review.

**[D] The revoke is only meaningful if the application role owns nothing.** This was established
empirically in phase 0 and is easy to get backwards. `REVOKE` *does* take effect against a table's
owner — after revoking, `has_table_privilege` reports false even for the owner. What an owner retains
is **grant option**, implicitly, so it can restore the privilege to itself in a single statement:

```sql
GRANT UPDATE ON audit_events TO pando_app;   -- succeeds when run as the owner
UPDATE audit_events SET action = 'something else';
```

Against an owning role the revoke is a speed bump, not a boundary, and the statement that undoes it is
available to exactly the process an attacker would be running inside. **Ownership is the property that
must be denied, not the privilege.** Pando therefore runs migrations as a schema-owning role and
serves traffic as a separate `pando_app` that owns nothing, and it verifies both at startup —
refusing to run if the audit table is owned by the role serving traffic.

**[D]** `detail` never contains a secret value. The `secret.Value` type from §00 3.3 makes this structural.

### 2.7 Sessions

```sql
CREATE TABLE sessions (
    id           text PRIMARY KEY,            -- ses_...
    user_id      text NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    adapter_id   text NOT NULL REFERENCES identity_adapters(id),
    issued_at    timestamptz NOT NULL DEFAULT now(),
    expires_at   timestamptz NOT NULL,
    revoked_at   timestamptz,
    user_agent   text,
    ip           inet
);
CREATE INDEX ON sessions (user_id) WHERE revoked_at IS NULL;
```

**[D]** Sessions are server-side rows, not stateless cookies. R-047 defers session lifetime to each adapter, but revocation has to be immediate when an adapter *can* push (R-048), and that is impossible with a stateless cookie. The cookie carries only `ses_...`.

**[P]** The proxy checks session validity on every request. At single-host scale this is one indexed lookup; cache with a short TTL if it ever matters, accepting that the TTL becomes the revocation window.

### 2.8 Backups

```sql
CREATE TABLE backups (
    id           text PRIMARY KEY,            -- bkp_...
    app_id       text REFERENCES apps(id) ON DELETE SET NULL,  -- NULL for DR bundles
    kind         text NOT NULL CHECK (kind IN ('rolling','on_delete','dr_bundle')),
    adapter_ref  text NOT NULL,               -- which backup adapter holds it (R-217)
    object_name  text NOT NULL,
    size_bytes   bigint,
    manifest     jsonb NOT NULL,              -- what's inside; drives restore verification (R-215)
    retain_until timestamptz,                 -- NULL for on_delete: kept until discarded (R-204)
    created_by   text NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT backups_on_delete_is_never_aged_out CHECK (
        kind <> 'on_delete' OR retain_until IS NULL),
    CONSTRAINT backups_scope_matches_kind CHECK (
        (kind =  'dr_bundle' AND app_id IS     NULL) OR
        (kind <> 'dr_bundle' AND app_id IS NOT NULL)),
    CONSTRAINT backups_object_is_unique UNIQUE (adapter_ref, object_name)
);
```

**[D]** `kind = 'on_delete'` rows have `retain_until IS NULL` — R-204 says these are kept until explicitly discarded, not aged out. The CHECK makes that structural rather than a convention the pruning query has to remember.

**[D]** `app_id` is `ON DELETE SET NULL`, **not** `CASCADE`. R-204 keeps a final backup after the app is gone; cascading would delete the record of that backup at exactly the moment it starts mattering. The row outlives its app on purpose, and `backups_scope_matches_kind` is therefore written against `kind`, which does not change, rather than against the app still existing.

**[D]** The destination is stored as an adapter reference plus an object name (R-217, R-252). Resolved at write time and recorded, never re-resolved at restore: a bundle written to one destination is not findable in another, and quietly looking elsewhere is how a restore reports "not found" for a bundle that exists.

**[D]** The `manifest` is what restore verifies against before applying anything (R-215). Pando keeps this copy; the copy inside the bundle is the one being checked against it.

**[D]** A per-app backup is encrypted under the **install's own secrets key**, not a supplied passphrase. R-213 governs the DR bundle and its reasoning does not carry over: it exists because a restore onto a fresh machine cannot unwrap keys held by the machine that died, and because shipping the key inside the bundle makes it plaintext for anyone holding the file. An app backup is restored in place, onto this install (R-206), so the machine that can read it is the machine that wrote it. The alternative — prompting for a passphrase on every app deletion — is a prompt people learn to type "password" into, which is weaker than the key already protecting every secret in the install. Pando keeps this copy; the copy inside the bundle is the one being checked against it.

---

## 3. Things deliberately not in the schema

- **Hosts.** R-256: multi-machine capability lives entirely in adapters. Adding a `hosts` table would be the first step toward the scheduler R-010 forbids.
- **Tenants/orgs.** R-015.
- **Per-user instances.** R-290 is `LATER`. When it lands it is an `app_instances` table keyed on `(app_id, user_id)` with its own volume rows; nothing above needs restructuring to accommodate it.
- **Log storage.** Logs are streamed from the runtime adapter and retained on disk under a size cap (R-222), not in Postgres. Putting them in Postgres makes R-224's disk accounting harder, not easier.
