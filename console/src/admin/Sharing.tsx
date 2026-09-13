// Sharing (R-076, R-077).
//
// Two rules, both about wording rather than mechanics:
//
// **The anonymous grant never stands on the bare word "public."** R-077 [P],
// and the override is in the heading only: the action is called "Make it
// public", because that is what it is called everywhere else and a heading
// nobody recognises is its own kind of unclear. What the requirement is
// actually protecting is kept — the consequence, *anyone on the internet can
// open this, without signing in*, sits directly under the heading and in the
// confirmation, so the word is never doing the work alone. "Public" is a word
// people skim past; the sentence is not. R-075 makes it a real grant row
// rather than a flag, and this is the interface to that row.
//
// **When host policy forbids it, the option is visible but disabled, with an
// explanation of who to ask** — not hidden. A hidden option produces a support
// ticket instead of understanding, and a person who cannot find the setting
// cannot tell whether it exists.

import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Button, Dialog, Input, Select, Table, Tag } from '@design';

import { api, RequestFailed } from '@api/client';
import type { GrantRow } from '@api/types.gen';

interface GrantsResponse {
  grants: GrantRow[] | null;
  /**
   * Whether host policy allows sharing with everyone (R-076).
   *
   * From the API, not inferred: the console must not decide what policy allows.
   * Absent means the endpoint does not report it yet, and the option stays
   * enabled — the server refuses it either way, and the refusal carries the
   * explanation.
   */
  anonymous_allowed?: boolean;
  anonymous_policy_note?: string;
}

export function Sharing({ appID, appName }: { appID: string; appName: string }) {
  const queries = useQueryClient();
  const [confirming, setConfirming] = useState(false);
  const [email, setEmail] = useState('');
  // The least access, not the second-least. Sharing an app most often means
  // "you can use this"; anything more is a deliberate choice, and a default
  // that quietly hands over the app's settings is the wrong way round.
  const [role, setRole] = useState('use');

  const grants = useQuery({
    queryKey: ['apps', appID, 'grants'],
    queryFn: () => api.get<GrantsResponse>(`/apps/${appID}/grants`),
  });

  const share = useMutation({
    mutationFn: (body: unknown) => api.post(`/apps/${appID}/grants`, body),
    onSuccess: () => queries.invalidateQueries({ queryKey: ['apps', appID, 'grants'] }),
  });

  const revoke = useMutation({
    mutationFn: (grantID: string) => api.del(`/apps/${appID}/grants/${grantID}`),
    onSuccess: () => queries.invalidateQueries({ queryKey: ['apps', appID, 'grants'] }),
  });

  const rows = grants.data?.grants ?? [];
  const anonymous = rows.find((g) => g.principal_kind === 'anonymous');
  const allowed = grants.data?.anonymous_allowed ?? true;

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-6)', maxWidth: 'var(--console-max)' }}>
      <section style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-3)' }}>
        {/* Both planes are in this table, so the heading cannot claim only one
            of them. "Who can open this app" over a row that says `owner` reads
            as a mistake — and the R-070/071 distinction is the thing this
            screen exists to make visible, not to paper over. */}
        <h4 style={{ font: 'var(--type-h4)', margin: 0 }}>Who has access</h4>
        <Table
          columns={[
            { key: 'who', header: 'Who', width: 'minmax(0,2fr)', render: who },
            { key: 'access', header: 'Access', width: 'minmax(0,1fr)', render: access },
            {
              key: 'actions',
              header: '',
              width: '12ch',
              align: 'right',
              render: (row: Access) => (
                <Button variant="ghost" onClick={() => row.grantIDs.forEach((id) => revoke.mutate(id))}>
                  Remove
                </Button>
              ),
            },
          ]}
          rows={byPrincipal(rows)}
        />
      </section>

      <section
        style={{
          display: 'flex',
          flexDirection: 'column',
          gap: 'var(--space-3)',
          paddingTop: 'var(--space-5)',
          borderTop: 'var(--border-width) solid var(--rule)',
        }}
      >
        <h4 style={{ font: 'var(--type-h4)', margin: 0 }}>Share with someone</h4>
        <p style={{ font: 'var(--type-body-ui)', color: 'var(--ink-secondary)', margin: 0 }}>
          They&rsquo;ll see {appName} the next time they sign in. Pando doesn&rsquo;t send them a
          message.
        </p>
        {/* The select used to be sized with `width: var(--space-9)` — a spacing
            token used as a width, and far narrower than its longest option. The
            label overflowed and the button sat on top of it. It is sized from
            its content now, and the row wraps instead of overlapping when there
            is not enough of it. */}
        <div
          style={{
            display: 'flex',
            flexWrap: 'wrap',
            gap: 'var(--space-3)',
            alignItems: 'flex-end',
          }}
        >
          <Input
            label="Email address"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            style={{ flex: '2 1 24ch' }}
          />
          <Select
            label="What they can do"
            options={[
              { value: 'use', label: 'Open the app' },
              { value: 'viewer', label: 'Open it and see its settings' },
              { value: 'operator', label: 'Open it and deploy it' },
              { value: 'owner', label: 'Everything, including sharing' },
            ]}
            value={role}
            onChange={(e) => setRole(e.target.value)}
            style={{ flex: '1 1 28ch' }}
          />
          <Button
            onClick={() => share.mutate({ email, role })}
            disabled={!email || share.isPending}
          >
            Share app
          </Button>
        </div>
        {share.isError && <Failure error={share.error} />}
      </section>

      <section
        style={{
          display: 'flex',
          flexDirection: 'column',
          gap: 'var(--space-3)',
          paddingTop: 'var(--space-5)',
          borderTop: 'var(--border-width) solid var(--rule)',
        }}
      >
        <h4 style={{ font: 'var(--type-h4)', margin: 0 }}>Make it public</h4>

        {/* R-077 [P], overridden in the heading and kept here. The requirement
            forbids presenting this as the *bare* word "public" — a toggle
            labelled "Public" and nothing else, which people skim past without
            registering what it does. "Make it public" is what the action is
            called everywhere else in the world, and the consequence directly
            beneath it is what the requirement is actually protecting. The
            confirmation step stays too. */}
        <p style={{ font: 'var(--type-body-ui)', color: 'var(--ink)', margin: 0 }}>
          Anyone on the internet can open {appName}, without signing in.
        </p>

        {!allowed && (
          // Visible and disabled, with who to ask. Hiding it would produce a
          // support ticket instead of understanding.
          <p style={{ font: 'var(--type-caption)', color: 'var(--ink-secondary)', margin: 0 }}>
            {grants.data?.anonymous_policy_note ??
              'This installation doesn’t allow apps to be opened up to everyone. An administrator can change that in host policy.'}
          </p>
        )}

        {anonymous ? (
          <Button
            variant="destructive"
            onClick={() => revoke.mutate(anonymous.id)}
            style={{ alignSelf: 'flex-start' }}
          >
            Make it private again
          </Button>
        ) : (
          <Button
            disabled={!allowed}
            onClick={() => setConfirming(true)}
            style={{ alignSelf: 'flex-start' }}
          >
            Make it public
          </Button>
        )}
      </section>

      <Dialog
        open={confirming}
        title="Make this app public?"
        description={`Anyone who has the link will be able to open ${appName} without signing in. You can undo this at any time.`}
        onClose={() => setConfirming(false)}
        footer={
          <>
            <Button variant="ghost" onClick={() => setConfirming(false)}>
              Cancel
            </Button>
            <Button
              variant="primary"
              onClick={() => {
                share.mutate({ principal_kind: 'anonymous' });
                setConfirming(false);
              }}
            >
              Make it public
            </Button>
          </>
        }
      />
    </div>
  );
}

/** One person, and everything they can do with this app. */
interface Access {
  key: string;
  principalKind: string;
  name: string;
  canOpen: boolean;
  roles: string[];
  grantIDs: string[];
}

/**
 * One row per person, not one per grant.
 *
 * Owning an app is two grants — control to administer it, data to open it —
 * and the table listed both, so the owner appeared twice with no indication
 * that the two lines were the same person. R-070/071 keeps the planes separate
 * in the model, which is right; it does not follow that somebody reading a list
 * of who has access should be shown the model.
 */
function byPrincipal(rows: GrantRow[]): Access[] {
  const out = new Map<string, Access>();

  for (const row of rows) {
    // Anonymous is not a person and has its own section below, where the
    // consequence is spelled out rather than listed in a table.
    if (row.principal_kind === 'anonymous') continue;

    const key = `${row.principal_kind}:${row.principal_id ?? ''}`;
    const entry = out.get(key) ?? {
      key,
      principalKind: row.principal_kind,
      // The name the server resolved, falling back to the identifier only when
      // there is nothing else — a principal deleted since the grant was made.
      name: row.principal_name || row.principal_id || '',
      canOpen: false,
      roles: [],
      grantIDs: [],
    };

    entry.grantIDs.push(row.id);
    if (row.plane === 'data') {
      entry.canOpen = true;
    } else {
      entry.roles.push(sentence(row.role_name || row.role_id || 'control'));
    }
    out.set(key, entry);
  }

  return [...out.values()];
}

function who(row: Access): React.ReactNode {
  return <span>{row.name}</span>;
}

function access(row: Access): React.ReactNode {
  // Everything this person can do, in one cell. "Can open it" is the data
  // plane; the roles are control (R-070, R-071). Someone who owns the app has
  // both, and that now reads as one person with two capabilities rather than
  // as two people who happen to share a name.
  const parts = [...row.roles, ...(row.canOpen ? ['Can open it'] : [])];
  return (
    <div style={{ display: 'flex', gap: 'var(--space-2)', flexWrap: 'wrap', alignItems: 'center' }}>
      {row.roles.map((r) => (
        <Tag key={r}>{r}</Tag>
      ))}
      {row.canOpen && <span>Can open it</span>}
      {parts.length === 0 && <span>No access</span>}
    </div>
  );
}

/** Role names are stored lowercase; the design system sets everything in
 *  sentence case. */
function sentence(s: string): string {
  return s.charAt(0).toUpperCase() + s.slice(1);
}

function Failure({ error }: { error: unknown }) {
  const failed = error instanceof RequestFailed ? error : null;
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-1)' }}>
      <p style={{ font: 'var(--type-body-ui)', color: 'var(--marker-deep)', margin: 0 }}>
        {failed?.message ?? 'Something went wrong. Try again.'}
      </p>
      {failed?.remedy && (
        <p style={{ font: 'var(--type-caption)', color: 'var(--ink-secondary)', margin: 0 }}>
          {failed.remedy}
        </p>
      )}
    </div>
  );
}
