// Installation, policy and audit — the three read-mostly screens behind
// install.view, install.policy.manage and install.audit.read.
//
// Each is shown on the verb it needs, never on "is an administrator". There is
// no implication graph between verbs (R-082), so a sidebar that assumed one
// would offer a screen whose every request comes back 403.

import { useLayoutEffect, useRef, useState } from 'react';
import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Banner, Button, EmptyState, Input, Select, StatusIndicator, Switch, Tag } from '@design';

import { api } from '@api/client';
import { Quiet, Screen, messageOf } from './Accounts';
import { NoMatches, SearchField } from '../ui/SearchField';
import { matches } from '../ui/search';
import { Table } from '../ui/Table';

interface AdapterRow {
  ref: string;
  kind: string;
  category: string;
  healthy?: boolean;
  [key: string]: unknown;
}

export function Installation() {
  const adapters = useQuery({
    queryKey: ['adapters'],
    queryFn: () => api.get<{ adapters: AdapterRow[] } | AdapterRow[]>('/adapters'),
  });
  const capacity = useQuery({
    queryKey: ['capacity'],
    queryFn: () => api.get<unknown>('/capacity'),
  });

  const rows = normalize(adapters.data);

  return (
    <Screen heading="Installation">
      {adapters.isError && <Quiet>{messageOf(adapters.error)}</Quiet>}

      <h4 style={{ font: 'var(--type-h4)', margin: '0 0 var(--space-3)' }}>Adapters</h4>
      <Table
        columns={[
          { key: 'ref', header: 'Reference', width: 'minmax(0,28ch)', mono: true },
          { key: 'kind', header: 'Kind', width: 'minmax(0,24ch)' },
          { key: 'category', header: 'Category', width: '16ch', muted: true },
          {
            key: 'healthy',
            header: 'Status',
            width: '16ch',
            render: (row: AdapterRow) => (
              // Live, not stored: an adapter that was reachable at startup and
              // is not now is exactly what this column exists to show.
              <StatusIndicator
                status={row.healthy === false ? 'failed' : 'running'}
                label={row.healthy === false ? 'Unreachable' : 'Reachable'}
              />
            ),
          },
        ]}
        rows={rows}
      />

      <h4 style={{ font: 'var(--type-h4)', margin: 'var(--space-6) 0 var(--space-3)' }}>
        Capacity
      </h4>
      {/* Machine output, shown verbatim in mono. Capacity is per runtime
          adapter and its shape is the adapter's, not Pando's, so prettifying it
          here would be Pando inventing a schema it does not own (R-243). */}
      <pre
        style={{
          font: 'var(--type-code-sm)',
          background: 'var(--paper-sunken)',
          border: 'var(--border-width) solid var(--rule)',
          borderRadius: 'var(--radius-sm)',
          padding: 'var(--space-4)',
          overflowX: 'auto',
          margin: 0,
        }}
      >
        {capacity.isPending ? 'Loading.' : JSON.stringify(capacity.data, null, 2)}
      </pre>
    </Screen>
  );
}

interface PolicyDoc {
  source_allowlist?: string[];
  disabled_verbs?: string[];
  allow_anonymous_grants?: boolean;
  egress_allowlist?: string[];
  require_backup_before_destroy?: boolean;
  max_token_lifetime_days?: number;
  min_build_isolation?: number;
  min_runtime_isolation?: number;

  agent_disabled_verbs?: string[];
  max_log_disk_bytes?: number;

  // The security score (R-313 – R-316).
  min_security_score?: number;
  insecure_action?: string;
  insecure_grace_hours?: number;
  ignore_unfixable_findings?: boolean;
}

interface Violation {
  app_id: string;
  app_name: string;
  code: string;
  message: string;
  remedy?: string;
}

export function Policy({ canEdit }: { canEdit: boolean }) {
  const queries = useQueryClient();
  const [draft, setDraft] = useState<PolicyDoc | null>(null);

  // Search over the settings themselves. Each section is shown when anything
  // in it matches — its heading, its note, a label, a description, an option —
  // read from what is on the screen rather than from a list of keywords kept
  // beside it, which would be a second description of the page that nobody
  // remembers to update when a setting is added.
  const [query, setQuery] = useState('');
  const sectionsRef = useRef<HTMLDivElement>(null);
  const [nothingMatches, setNothingMatches] = useState(false);

  const policy = useQuery({
    queryKey: ['policy'],
    queryFn: () => api.get<PolicyDoc>('/policy'),
  });

  const save = useMutation({
    mutationFn: (doc: PolicyDoc) => api.put<PolicyDoc>('/policy', doc),
    onSuccess: () => {
      setDraft(null);
      setPreview(null);
      void queries.invalidateQueries({ queryKey: ['policy'] });
    },
  });

  // What this policy would block, before it is saved (design 05 §3).
  //
  // Asked for, not automatic. Running every app's pinned spec through the
  // planner is not free, and doing it on every keystroke would make a form that
  // stutters — which teaches people to ignore the panel it is stuttering to
  // fill.
  const [preview, setPreview] = useState<Violation[] | null>(null);
  const check = useMutation({
    mutationFn: (doc: PolicyDoc) =>
      api.post<{ violations: Violation[] }>('/policy/preview', doc),
    onSuccess: (result) => setPreview(result.violations ?? []),
  });

  const current = draft ?? policy.data ?? {};

  useLayoutEffect(() => {
    const sections = Array.from(sectionsRef.current?.querySelectorAll<HTMLElement>('section') ?? []);
    let shown = 0;
    for (const el of sections) {
      const match = matches(query, el.textContent ?? '');
      // Inline, because PolicySection's own inline display: flex outranks the
      // hidden attribute. Set back to flex, not cleared: React only rewrites a
      // style it sees change, so a cleared one would stay cleared.
      el.style.display = match ? 'flex' : 'none';
      if (match) shown += 1;
    }
    setNothingMatches(sections.length > 0 && shown === 0);
  });
  const edit = (patch: Partial<PolicyDoc>) => {
    // A stale answer is worse than no answer: it says "nothing breaks" about a
    // policy that is no longer the one on screen.
    setPreview(null);
    setDraft({ ...current, ...patch });
  };

  if (policy.isPending) return <Screen heading="Policy"><Quiet>Loading.</Quiet></Screen>;
  if (policy.isError)
    return (
      <Screen heading="Policy">
        <Quiet>{messageOf(policy.error)}</Quiet>
      </Screen>
    );

  return (
    <Screen
      heading="Policy"
      action={
        <div style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 'var(--space-3)' }}>
        {canEdit && draft && (
          <div style={{ display: 'flex', gap: 'var(--space-3)' }}>
            <Button variant="ghost" onClick={() => { setDraft(null); setPreview(null); }}>
              Discard
            </Button>
            <Button
              variant="secondary"
              disabled={check.isPending}
              onClick={() => check.mutate(current)}
            >
              {check.isPending ? 'Checking' : 'Check what this affects'}
            </Button>
            <Button variant="primary" disabled={save.isPending} onClick={() => save.mutate(current)}>
              {save.isPending ? 'Saving' : 'Save policy'}
            </Button>
          </div>
        )}
          <SearchField value={query} onChange={setQuery} placeholder="Search policy" />
        </div>
      }
    >
      {/* O-10, stated where the decision is made rather than in a tooltip.
          Saying it plainly is the difference between an administrator who
          knows why nothing happened and one who thinks the save failed. */}
      <Banner tone="info">
        Saving policy doesn&rsquo;t change apps that are already running. A running app that
        breaks a new rule keeps running, and its next deploy is refused with the reason.
      </Banner>

      {save.isError && (
        <div style={{ marginTop: 'var(--space-4)' }}>
          <Banner tone="failed">{messageOf(save.error)}</Banner>
        </div>
      )}

      {check.isError && (
        <div style={{ marginTop: 'var(--space-4)' }}>
          <Banner tone="failed">{messageOf(check.error)}</Banner>
        </div>
      )}

      {/* O-10 says a violating app keeps running and fails its next deploy.
          This is the half of that which makes it liveable: an administrator
          tightening a rule is entitled to know it blocks four apps before they
          save, because finding out one deploy at a time is how a policy gets
          rolled back in anger. */}
      {preview !== null && (
        <div style={{ marginTop: 'var(--space-4)' }}>
          {preview.length === 0 ? (
            <Banner tone="running">
              Nothing on this installation breaks under these rules.
            </Banner>
          ) : (
            <>
              <Banner tone="building">
                {preview.length === 1
                  ? 'One app keeps running and is refused at its next deploy.'
                  : `${preview.length} apps keep running and are refused at their next deploy.`}
              </Banner>
              <div style={{ marginTop: 'var(--space-4)' }}>
                <Table
                  dense
                  columns={[
                    { key: 'app_name', header: 'App', width: 'minmax(0,24ch)' },
                    { key: 'message', header: 'What its next deploy will say', width: 'minmax(0,32ch)' },
                    { key: 'code', header: 'Code', width: '28ch', mono: true, muted: true },
                  ]}
                  rows={preview}
                />
              </div>
            </>
          )}
        </div>
      )}

      {nothingMatches && (
        <div style={{ marginTop: 'var(--space-6)' }}>
          <NoMatches what="settings" query={query} />
        </div>
      )}

      <div
        ref={sectionsRef}
        style={{
          display: 'flex',
          flexDirection: 'column',
          marginTop: 'var(--space-6)',
          maxWidth: '68ch',
        }}
      >
        {/* Sections separated by rules, which is the system's own answer to a
            page of settings — and the reason is the page rather than the
            style: policy is a list of unrelated decisions that only grows, and
            a flat column of fourteen switches is a column nobody scans. Each
            heading says which question the controls under it answer. */}
        <PolicySection
          heading="Where apps come from"
          note="Evaluated before anything is cloned, so a source nobody allowed never reaches the disk."
        >
          <Input
            label="Where apps may be created from"
            as="textarea"
            rows={3}
            mono
            disabled={!canEdit}
            value={(current.source_allowlist ?? []).join('\n')}
            helper="One host pattern per line, for example github.com/acme/*. Empty means anywhere."
            onChange={(e) =>
              edit({ source_allowlist: e.target.value.split('\n').map((l) => l.trim()).filter(Boolean) })
            }
          />
        </PolicySection>

        <PolicySection
          heading="Who can reach apps"
          note="A floor, never an override: an app owner can be stricter than this and never looser (R-272)."
        >
          <Switch
            checked={current.allow_anonymous_grants !== false}
            disabled={!canEdit}
            label="Allow apps to be shared with anyone on the internet"
            // R-076: when this is off the sharing option stays visible and
            // disabled rather than disappearing. A hidden option produces a
            // support ticket instead of understanding.
            description="When this is off, people can still see the option to share an app without sign-in — it's disabled, with a note saying who to ask."
            onChange={(e) => edit({ allow_anonymous_grants: e.target.checked })}
          />

          <Switch
            checked={current.disabled_verbs?.includes('app.exec') ?? false}
            disabled={!canEdit}
            label="Turn off terminal access for the whole installation"
            // R-085 and R-272: policy is a floor, not an override.
            description="Nobody gets a terminal, including the person who owns the app."
            onChange={(e) => {
              const rest = (current.disabled_verbs ?? []).filter((v) => v !== 'app.exec');
              edit({ disabled_verbs: e.target.checked ? [...rest, 'app.exec'] : rest });
            }}
          />

          {current.disabled_verbs && current.disabled_verbs.length > 0 && (
            <div style={{ display: 'flex', gap: 'var(--space-2)', flexWrap: 'wrap' }}>
              {current.disabled_verbs.map((v) => (
                <Tag key={v} mono>
                  {v}
                </Tag>
              ))}
            </div>
          )}
        </PolicySection>

        <PolicySection
          heading="Isolation and egress"
          note="The floor every app runs at, and where they may connect out to."
        >
          <Select
            label="Minimum isolation for builds"
            disabled={!canEdit}
            value={String(current.min_build_isolation ?? '')}
            options={ISOLATION}
            helper="An adapter that cannot meet this is refused at plan time rather than used anyway (R-024)."
            onChange={(e) => edit({ min_build_isolation: Number(e.target.value) || undefined })}
          />

          <Select
            label="Minimum isolation for running apps"
            disabled={!canEdit}
            value={String(current.min_runtime_isolation ?? '')}
            options={ISOLATION}
            helper="The same floor, for what runs rather than what builds."
            onChange={(e) => edit({ min_runtime_isolation: Number(e.target.value) || undefined })}
          />

          <Input
            label="Where apps may connect out to"
            as="textarea"
            rows={3}
            mono
            disabled={!canEdit}
            value={(current.egress_allowlist ?? []).join('\n')}
            helper="One host per line. Empty means anywhere, which is the shipped default."
            onChange={(e) =>
              edit({ egress_allowlist: e.target.value.split('\n').map((l) => l.trim()).filter(Boolean) })
            }
          />
        </PolicySection>

        <PolicySection
          heading="Tokens and agents"
          note="A token is a second credential for power somebody already holds; an agent is an ordinary principal holding one."
        >
          <Input
            label="Longest a token may live, in days"
            type="number"
            disabled={!canEdit}
            value={String(current.max_token_lifetime_days ?? 0)}
            helper="Zero means no cap, and a token may be created that never expires (R-061). Any other number caps every token, including ones asked to last forever."
            onChange={(e) =>
              edit({ max_token_lifetime_days: Math.max(0, Math.round(Number(e.target.value) || 0)) })
            }
          />

          <Input
            label="Verbs an agent's token may not use"
            as="textarea"
            rows={3}
            mono
            disabled={!canEdit}
            value={(current.agent_disabled_verbs ?? []).join('\n')}
            helper="One verb per line. These are refused for tokens and allowed for people, which is what makes an agent's reach smaller than its owner's."
            onChange={(e) =>
              edit({
                agent_disabled_verbs: e.target.value.split('\n').map((l) => l.trim()).filter(Boolean),
              })
            }
          />
        </PolicySection>

        <PolicySection
          heading="Security scanning"
          note="Every app is scored out of 100 from what it deploys. Nothing here is enforced until a minimum is set."
        >
          {/* The consequence is written at the point of setting it, not in a
              tooltip: this is the setting that can stop somebody's working
              service. */}
          <Input
            label="Minimum security score"
            type="number"
            disabled={!canEdit}
            value={String(current.min_security_score ?? 0)}
            helper="0 to 100. Zero is off. An app below this is refused at deploy, with the findings that cost it the most."
            onChange={(e) => edit({ min_security_score: clamp(Number(e.target.value)) })}
          />

          <Switch
            checked={current.ignore_unfixable_findings ?? false}
            disabled={!canEdit}
            label="Ignore findings with no fix available"
            // R-313a: what is counted is what is shown, both ways round.
            description="A vulnerability nobody has published a fix for is left out of the score and out of the findings list. Off by default, so the score says what is wrong rather than what is fixable today."
            onChange={(e) => edit({ ignore_unfixable_findings: e.target.checked })}
          />

          {(current.min_security_score ?? 0) > 0 && (
            <>
              <Switch
                checked={current.insecure_action === 'stop'}
                disabled={!canEdit}
                label="Stop apps that fall below it while running"
                // R-315: a policy change or a newly published CVE is not a
                // reason to take a working service away without notice, so
                // this is opt-in and grace comes with it.
                description="An app that is already running is never stopped on the spot. Its owner is told, and the grace period below starts."
                onChange={(e) => edit({ insecure_action: e.target.checked ? 'stop' : 'warn' })}
              />

              {current.insecure_action === 'stop' && (
                <Input
                  label="Grace period, in hours"
                  type="number"
                  disabled={!canEdit}
                  value={String(current.insecure_grace_hours ?? 24)}
                  helper="How long an app has after it is first found below the score. The deadline is in the message its owner gets."
                  onChange={(e) =>
                    edit({ insecure_grace_hours: Math.max(1, Number(e.target.value) || 24) })
                  }
                />
              )}
            </>
          )}
        </PolicySection>

        <PolicySection
          heading="Data and destruction"
          note="What happens to an app's storage when somebody, or something, removes it."
        >
          <Switch
            checked={current.require_backup_before_destroy ?? false}
            disabled={!canEdit}
            label="Require a backup before anything is destroyed"
            // R-284: set once, and app owners cannot override downward.
            description="App owners can't turn this off for their own app."
            onChange={(e) => edit({ require_backup_before_destroy: e.target.checked })}
          />

          <Input
            label="Total disk for app logs, in megabytes"
            type="number"
            disabled={!canEdit}
            value={String(Math.round((current.max_log_disk_bytes ?? 0) / 1_000_000))}
            // R-224: checked against the sum of every app's cap at plan time,
            // not against measured usage — bounding what is committed is the
            // stronger guarantee, and the alternative acts after the disk is
            // already filling.
            helper="Across every app, not per app. Zero means no limit. A deploy that would commit more than this is refused."
            onChange={(e) =>
              edit({ max_log_disk_bytes: Math.max(0, Math.round(Number(e.target.value) || 0)) * 1_000_000 })
            }
          />
        </PolicySection>
      </div>
    </Screen>
  );
}

/**
 * The isolation floors, as the spec names them (design 01 §3).
 *
 * Ordered integers underneath, because policy floors are compared (R-114) — a
 * select of four options is the readable half of that, not a second model.
 */
const ISOLATION = [
  { value: '', label: 'No floor' },
  { value: '10', label: 'Container — a shared kernel' },
  { value: '20', label: 'Sandboxed — a kernel of its own, shared host' },
  { value: '30', label: 'Virtual machine' },
  { value: '40', label: 'Dedicated host' },
];

/**
 * One group of policy settings.
 *
 * A heading, a line saying which question the controls answer, and a rule above
 * it — the system's "sections separated by rules" rather than a card each,
 * because these are settings on one page and not objects in a list.
 */
function PolicySection({
  heading,
  note,
  children,
}: {
  heading: string;
  note: string;
  children: React.ReactNode;
}) {
  return (
    <section
      style={{
        display: 'flex',
        flexDirection: 'column',
        gap: 'var(--space-4)',
        padding: 'var(--space-6) 0',
        borderTop: 'var(--border-width) solid var(--rule)',
      }}
    >
      <div>
        <h4 style={{ font: 'var(--type-h4)', margin: '0 0 var(--space-1)' }}>{heading}</h4>
        <Quiet>{note}</Quiet>
      </div>
      {children}
    </section>
  );
}

/** 0 to 100, because a threshold outside it is a threshold nothing can meet. */
function clamp(n: number): number {
  if (Number.isNaN(n) || n < 0) return 0;
  if (n > 100) return 100;
  return Math.round(n);
}

interface AuditRecord {
  id: number;
  occurred_at: string;
  principal_kind: string;
  principal_id?: string;
  on_behalf_of?: string;
  action: string;
  app_id?: string;
  target_kind?: string;
  target_id?: string;
}

// What the audit log can be narrowed by. Each maps to a GET /audit parameter,
// and they combine — "Dana's changes to roles in the last day" is one query.
interface AuditFilters {
  action: string;
  actor: string;
  targetKind: string;
  targetID: string;
  when: string;
  since: string;
  until: string;
}

const NO_FILTERS: AuditFilters = { action: '', actor: '', targetKind: '', targetID: '', when: '', since: '', until: '' };

// The kinds of thing the server records events against.
const TARGET_KINDS = [
  'app',
  'user',
  'group',
  'role',
  'grant',
  'token',
  'session',
  'secret',
  'backup',
  'deployment',
  'policy',
  'adapter',
  'volume',
  'slot',
  'spec_revision',
  'workload',
  'launcher_section',
];

const WHEN: { value: string; label: string; hours?: number }[] = [
  { value: '', label: 'All time' },
  { value: '1h', label: 'Last hour', hours: 1 },
  { value: '24h', label: 'Last 24 hours', hours: 24 },
  { value: '7d', label: 'Last 7 days', hours: 24 * 7 },
  { value: '30d', label: 'Last 30 days', hours: 24 * 30 },
  { value: 'custom', label: 'Custom range' },
];

/** The query string for a set of filters, times resolved now. */
function auditQuery(f: AuditFilters, before?: string): string {
  const q = new URLSearchParams();
  if (f.action) q.set('action', f.action);
  if (f.actor) q.set('principal_id', f.actor);
  if (f.targetKind) q.set('target_kind', f.targetKind);
  if (f.targetID) q.set('target_id', f.targetID.trim());
  const preset = WHEN.find((w) => w.value === f.when);
  if (preset?.hours) q.set('since', new Date(Date.now() - preset.hours * 3_600_000).toISOString());
  if (f.when === 'custom') {
    // datetime-local is the viewer's own clock; the wire is UTC (RFC 3339).
    if (f.since) q.set('since', new Date(f.since).toISOString());
    if (f.until) q.set('until', new Date(f.until).toISOString());
  }
  if (before) q.set('before', before);
  const s = q.toString();
  return s ? `?${s}` : '';
}

export function Audit() {
  const [filters, setFilters] = useState<AuditFilters>(NO_FILTERS);
  const set = (patch: Partial<AuditFilters>) => setFilters((f) => ({ ...f, ...patch }));
  const filtered = JSON.stringify(filters) !== JSON.stringify(NO_FILTERS);

  // Pages on the server's cursor (design 04 §2.8): "Show older" asks for the
  // page before the last one shown, so events arriving meanwhile never shift
  // what has already been read.
  const log = useInfiniteQuery({
    queryKey: ['audit', filters],
    initialPageParam: '',
    queryFn: ({ pageParam }) =>
      api.get<{ events: AuditRecord[] | null; next_before: string }>('/audit' + auditQuery(filters, pageParam || undefined)),
    getNextPageParam: (last) => last.next_before || undefined,
  });

  // Names for the "who" column and the actor picker. install.audit.read does
  // not imply install.view — an account can hold only the first — so when the
  // list is refused, the picker becomes a field for an ID and the column shows
  // IDs, which is what it did before.
  const users = useQuery({
    queryKey: ['users'],
    queryFn: () => api.get<{ users: Array<{ id: string; external_id: string; display_name?: string }> }>('/users'),
    retry: false,
  });
  const people = users.data?.users ?? [];
  const nameOf = (id?: string) => (id ? (people.find((u) => u.id === id)?.external_id ?? id) : '');

  const events = log.data?.pages.flatMap((p) => p.events ?? []) ?? [];

  const custom = filters.when === 'custom';
  const range = (
    <Field>
      <Select
        label="Time range"
        value={filters.when}
        options={WHEN.map((w) => ({ value: w.value, label: w.label }))}
        onChange={(e) => set({ when: e.target.value })}
      />
    </Field>
  );

  return (
    <Screen heading="Audit log">
      <div style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-3)', marginBottom: 'var(--space-5)' }}>
        <FilterRow>
          <Field>
            <Input
              label="Action"
              mono
              value={filters.action}
              placeholder="Prefix, e.g. app."
              onChange={(e) => set({ action: e.target.value })}
            />
          </Field>

          <Field>
            {users.isSuccess ? (
              <Select
                label="Actor"
                value={filters.actor}
                options={[
                  { value: '', label: 'Any' },
                  ...people.map((u) => ({ value: u.id, label: u.display_name ? `${u.external_id} (${u.display_name})` : u.external_id })),
                ]}
                onChange={(e) => set({ actor: e.target.value })}
              />
            ) : (
              <Input
                label="Actor"
                mono
                placeholder="usr_… or tok_…"
                value={filters.actor}
                onChange={(e) => set({ actor: e.target.value.trim() })}
              />
            )}
          </Field>

          <Field>
            <Select
              label="Target type"
              value={filters.targetKind}
              options={[{ value: '', label: 'Any' }, ...TARGET_KINDS.map((k) => ({ value: k, label: k }))]}
              onChange={(e) => set({ targetKind: e.target.value })}
            />
          </Field>

          <Field>
            <Input
              label="Target ID"
              mono
              placeholder="app_…"
              value={filters.targetID}
              onChange={(e) => set({ targetID: e.target.value })}
            />
          </Field>

          {!custom && range}

          {filtered && !custom && <ClearFilters onClear={() => setFilters(NO_FILTERS)} />}
        </FilterRow>

        {/* A custom range is three fields that belong together, so they take
            a row of their own rather than wrapping one at a time onto it. */}
        {custom && (
          <FilterRow>
            {range}
            <Field>
              <Input
                label="From"
                type="datetime-local"
                value={filters.since}
                onChange={(e) => set({ since: e.target.value })}
              />
            </Field>
            <Field>
              <Input
                label="To"
                type="datetime-local"
                value={filters.until}
                onChange={(e) => set({ until: e.target.value })}
              />
            </Field>
            <ClearFilters onClear={() => setFilters(NO_FILTERS)} />
          </FilterRow>
        )}
      </div>

      {log.isError && <Quiet>{messageOf(log.error)}</Quiet>}

      <Table
        dense
        // A filter that matches nothing and a log that holds nothing look
        // identical as an empty table, and on this screen "nothing happened"
        // and "your filter is wrong" are very different answers.
        empty={
          <EmptyState heading={filtered ? 'No matching events' : 'No events recorded'}>
            {filtered
              ? 'Action is a prefix match: app. matches every app event.'
              : 'The audit log is append-only; recorded events cannot be modified or deleted.'}
          </EmptyState>
        }
        columns={[
          {
            key: 'occurred_at',
            header: 'Time',
            width: '20ch',
            muted: true,
            render: (row: AuditRecord) => new Date(row.occurred_at).toLocaleString(),
          },
          { key: 'action', header: 'Action', width: 'minmax(0,26ch)', mono: true },
          {
            key: 'principal_id',
            header: 'Actor',
            width: 'minmax(0,20ch)',
            mono: true,
            // A delegated token records both itself and the person it acted
            // for (R-229). Showing only one of them is how "who did this"
            // stops being answerable.
            render: (row: AuditRecord) =>
              row.on_behalf_of && row.on_behalf_of !== row.principal_id
                ? `${nameOf(row.principal_id)} for ${nameOf(row.on_behalf_of)}`
                : nameOf(row.principal_id) || row.principal_kind,
          },
          {
            key: 'target_id',
            header: 'Target',
            width: 'minmax(0,24ch)',
            mono: true,
            muted: true,
            render: (row: AuditRecord) =>
              row.target_id ? `${row.target_kind ? row.target_kind + ' ' : ''}${row.target_kind === 'user' ? nameOf(row.target_id) : row.target_id}` : row.app_id || '—',
          },
        ]}
        rows={events}
      />

      {log.hasNextPage && (
        <div style={{ marginTop: 'var(--space-4)' }}>
          <Button variant="secondary" disabled={log.isFetchingNextPage} onClick={() => void log.fetchNextPage()}>
            {log.isFetchingNextPage ? 'Loading' : 'Load older events'}
          </Button>
        </div>
      )}
    </Screen>
  );
}

function FilterRow({ children }: { children: React.ReactNode }) {
  return (
    <div style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'flex-end', gap: 'var(--space-3) var(--space-4)' }}>
      {children}
    </div>
  );
}

function ClearFilters({ onClear }: { onClear: () => void }) {
  return (
    <Button variant="ghost" onClick={onClear}>
      Clear filters
    </Button>
  );
}

function Field({ children }: { children: React.ReactNode }) {
  return <div style={{ flex: '1 1 18ch', minWidth: '18ch', maxWidth: '28ch' }}>{children}</div>;
}

/** GET /adapters has returned both shapes during this phase; accept either
 *  rather than break the screen on the one that turns out to be current. */
function normalize(data: { adapters: AdapterRow[] } | AdapterRow[] | undefined): AdapterRow[] {
  if (!data) return [];
  if (Array.isArray(data)) return data;
  return data.adapters ?? [];
}
