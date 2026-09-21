// Installation, policy and audit — the three read-mostly screens behind
// install.view, install.policy.manage and install.audit.read.
//
// Each is shown on the verb it needs, never on "is an administrator". There is
// no implication graph between verbs (R-082), so a sidebar that assumed one
// would offer a screen whose every request comes back 403.

import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Banner, Button, EmptyState, Input, Select, StatusIndicator, Switch, Table, Tag } from '@design';

import { api } from '@api/client';
import { Quiet, Screen, messageOf } from './Accounts';

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
            header: 'Reachable',
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
        canEdit && draft ? (
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
        ) : undefined
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

      <div
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

export function Audit() {
  const [filter, setFilter] = useState('');

  const log = useQuery({
    queryKey: ['audit', filter],
    queryFn: () =>
      api.get<{ events: AuditRecord[]; next_before: string }>(
        '/audit' + (filter ? `?action=${encodeURIComponent(filter)}` : ''),
      ),
  });

  const events = log.data?.events ?? [];

  return (
    <Screen heading="Audit log">
      <div style={{ maxWidth: '40ch', marginBottom: 'var(--space-5)' }}>
        <Input
          label="Action"
          mono
          value={filter}
          placeholder="app."
          helper="Matches the start of an action, so app. finds every app event."
          onChange={(e) => setFilter(e.target.value)}
        />
      </div>

      {log.isError && <Quiet>{messageOf(log.error)}</Quiet>}

      <Table
        dense
        // Column headers over nothing, until now. A filter that matches
        // nothing and a log that holds nothing look identical in that state,
        // and this is the screen where "nothing happened" and "your filter is
        // wrong" are very different answers.
        empty={
          <EmptyState heading={filter ? 'No events match that action' : 'Nothing recorded yet'}>
            {filter
              ? 'Actions are matched from the start, so app. finds every app event. Clear the filter to see everything.'
              : 'Every action Pando takes is written here, and nothing can rewrite it afterwards.'}
          </EmptyState>
        }
        columns={[
          {
            key: 'occurred_at',
            header: 'When',
            width: '20ch',
            muted: true,
            render: (row: AuditRecord) => new Date(row.occurred_at).toLocaleString(),
          },
          { key: 'action', header: 'Action', width: 'minmax(0,26ch)', mono: true },
          {
            key: 'principal_id',
            header: 'Who',
            width: 'minmax(0,20ch)',
            mono: true,
            // A delegated token records both itself and the person it acted
            // for (R-229). Showing only one of them is how "who did this"
            // stops being answerable.
            render: (row: AuditRecord) =>
              row.on_behalf_of && row.on_behalf_of !== row.principal_id
                ? `${row.principal_id} for ${row.on_behalf_of}`
                : row.principal_id || row.principal_kind,
          },
          {
            key: 'target_id',
            header: 'Target',
            width: 'minmax(0,18ch)',
            mono: true,
            muted: true,
            render: (row: AuditRecord) => row.target_id || row.app_id || '—',
          },
        ]}
        rows={events}
      />
    </Screen>
  );
}

/** GET /adapters has returned both shapes during this phase; accept either
 *  rather than break the screen on the one that turns out to be current. */
function normalize(data: { adapters: AdapterRow[] } | AdapterRow[] | undefined): AdapterRow[] {
  if (!data) return [];
  if (Array.isArray(data)) return data;
  return data.adapters ?? [];
}
