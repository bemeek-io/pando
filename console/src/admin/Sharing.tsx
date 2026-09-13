// Sharing (R-076, R-077).
//
// Two rules, both about wording rather than mechanics:
//
// **The anonymous grant is never labelled "public."** Design 08 §1.3: it reads
// *anyone on the internet, without signing in*, with a confirmation step.
// "Public" is a word people skim past; the sentence is not. R-075 makes it a
// real grant row rather than a flag, and this is the interface to that row.
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
  const [role, setRole] = useState('viewer');

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
            { key: 'plane', header: 'Access', width: '16ch', render: access },
            {
              key: 'actions',
              header: '',
              width: '12ch',
              align: 'right',
              render: (row: GrantRow) => (
                <Button variant="ghost" onClick={() => revoke.mutate(row.id)}>
                  Remove
                </Button>
              ),
            },
          ]}
          rows={rows}
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
        <h4 style={{ font: 'var(--type-h4)', margin: 0 }}>Open it up to everyone</h4>

        {/* Never "public". The sentence says exactly what happens. */}
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
            Stop sharing with everyone
          </Button>
        ) : (
          <Button
            disabled={!allowed}
            onClick={() => setConfirming(true)}
            style={{ alignSelf: 'flex-start' }}
          >
            Open it up to everyone
          </Button>
        )}
      </section>

      <Dialog
        open={confirming}
        title="Open this app to everyone?"
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
              Open it up
            </Button>
          </>
        }
      />
    </div>
  );
}

function who(row: GrantRow): React.ReactNode {
  if (row.principal_kind === 'anonymous') {
    return <span>Anyone on the internet, without signing in</span>;
  }
  // The name the server resolved, falling back to the identifier only when
  // there is genuinely nothing else — a principal deleted since the grant was
  // made. Showing `usr_01M2DW05…` to somebody deciding who may open their app
  // asks them to recognise a ULID, which nobody can do.
  return <span>{row.principal_name || row.principal_id}</span>;
}

function access(row: GrantRow): React.ReactNode {
  // Two planes, and the difference matters to the person reading it: a data
  // grant opens the app, a control grant administers it (R-070, R-071).
  if (row.plane === 'data') return <span>Can open it</span>;

  // The role's name, not its identifier. `role_owner` in a table somebody reads
  // to decide who may administer their app is the same mistake as showing them
  // a ULID for the person.
  return <Tag>{sentence(row.role_name || row.role_id || 'control')}</Tag>;
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
