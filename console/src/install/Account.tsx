// One account: who it is, what it can do here, the actions on it, and its
// history — every audit event where it is the actor or the target — with a
// link to the Audit log narrowed to it, for anything deeper.
//
// The actions are the ones Accounts used to put on every row. They are here
// now so that the list reads as a list, and changing someone's role or
// suspending them is something done on purpose, looking at who they are.
// Resetting a local account's password is here for the same reason.

import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Banner, Button, Dialog, EmptyState, Icon, Select, StatusIndicator } from '@design';

import { api } from '@api/client';
import { InstallVerb, useInstallVerb, usePrincipal } from '../app/principal';
import { Sheet } from '../ui/Sheet';
import { AccountApps } from './AccountApps';
import { GeneratedPassword, PasswordToCopy } from './GeneratedPassword';
import type { Account, Role } from './Accounts';
import { Quiet, RoleLabel, RolePicker, StatusToggle, messageOf } from './Accounts';
import { NO_FILTERS, WHEN, linkQuery } from './audit';
import type { AuditFilters } from './audit';
import { AuditTable, LoadOlder, useAuditLog, usePeople } from './Installation';

interface Group {
  id: string;
  name: string;
  members?: string[];
}

export function AccountPage({
  userID,
  onBack,
  onAudit,
}: {
  userID: string;
  onBack: () => void;
  /** Opens the Audit log with these filters. */
  onAudit: (query: string) => void;
}) {
  const manage = useInstallVerb(InstallVerb.UsersManage);
  const canReadAudit = useInstallVerb(InstallVerb.AuditRead);
  const me = usePrincipal();
  const isSelf = userID === me.data?.user_id;

  const account = useQuery({
    queryKey: ['users', userID],
    queryFn: () => api.get<Account>(`/users/${userID}`),
  });
  const roles = useQuery({
    queryKey: ['roles'],
    queryFn: () => api.get<{ roles: Role[] }>('/roles'),
  });
  const groups = useQuery({
    queryKey: ['groups'],
    queryFn: () => api.get<{ groups: Group[] }>('/groups'),
  });

  const back = (
    <Button variant="ghost" icon={<Icon name="arrow-left" />} onClick={onBack} style={{ alignSelf: 'flex-start' }}>
      Accounts
    </Button>
  );

  if (!account.data) {
    return (
      <Sheet heading={back}>
        {account.isError ? <Banner tone="failed">{messageOf(account.error)}</Banner> : <Quiet>Loading</Quiet>}
      </Sheet>
    );
  }

  const a = account.data;
  const active = a.status === 'active';
  const memberOf = (groups.data?.groups ?? []).filter((g) => g.members?.includes(a.id));

  return (
    <Sheet
      heading={
        <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'flex-start', gap: 'var(--space-3)' }}>
          {back}
          <div style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 'var(--space-3) var(--space-4)' }}>
            <h3 style={{ font: 'var(--type-h3)', margin: 0 }}>{a.display_name || a.external_id}</h3>
            <StatusIndicator
              // Suspended is a stopped account, not a failed one (see Accounts).
              status={active ? 'running' : 'stopped'}
              label={active ? 'Active' : 'Suspended'}
            />
            {manage && !isSelf && (
              <div style={{ display: 'flex', gap: 'var(--space-3)' }}>
                <StatusToggle account={a} />
                {/* An external provider holds its accounts' passwords; Pando has
                    nothing to reset. */}
                {a.adapter_id === LOCAL && <ResetPassword account={a} />}
                <DeleteAccount account={a} onDeleted={onBack} />
              </div>
            )}
          </div>
        </div>
      }
      note={a.id}
    >
      <div style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-7)' }}>
        <section>
          <Heading>Details</Heading>
          <dl
            style={{
              display: 'grid',
              gridTemplateColumns: 'minmax(12ch, max-content) 1fr',
              gap: 'var(--space-3) var(--space-6)',
              margin: 0,
              font: 'var(--type-body-ui)',
            }}
          >
            <Detail term="Username" mono>
              {a.external_id}
            </Detail>
            <Detail term="Name">{a.display_name || '—'}</Detail>
            <Detail term="Email">{a.email || '—'}</Detail>
            <Detail term="Identity source" mono={a.adapter_id !== LOCAL}>
              {a.adapter_id === LOCAL ? 'Local account' : a.adapter_id}
            </Detail>
            {a.adapter_id === LOCAL && (
              <Detail term="Password">
                {a.must_change_password ? 'Must be changed at next sign-in' : 'Set by the account holder'}
              </Detail>
            )}
            <Detail term="Created">{a.created_at ? new Date(a.created_at).toLocaleString() : '—'}</Detail>
            <Detail term="Groups">
              {memberOf.length > 0 ? memberOf.map((g) => g.name).join(', ') : 'None'}
            </Detail>
            <Detail term="Installation role">
              {manage ? (
                <div style={{ maxWidth: '32ch' }}>
                  <RolePicker account={a} roles={roles.data?.roles ?? []} isSelf={isSelf} />
                </div>
              ) : (
                <RoleLabel roleID={a.install_role_id} roles={roles.data?.roles ?? []} />
              )}
            </Detail>
          </dl>
        </section>

        <AccountApps account={a} />

        {canReadAudit && <Activity userID={a.id} onAudit={onAudit} />}
      </div>
    </Sheet>
  );
}

const LOCAL = 'idp_local';

/** The account's audit history: events where it is the actor or the target. */
function Activity({ userID, onAudit }: { userID: string; onAudit: (query: string) => void }) {
  const [when, setWhen] = useState('7d');
  // A custom range is the Audit log's job; the presets are enough to look.
  const filters: AuditFilters = { ...NO_FILTERS, involving: userID, when };
  const { log, events } = useAuditLog(filters);
  const people = usePeople();

  return (
    <section>
      <div
        style={{
          display: 'flex',
          flexWrap: 'wrap',
          alignItems: 'flex-end',
          justifyContent: 'space-between',
          gap: 'var(--space-3) var(--space-4)',
          marginBottom: 'var(--space-4)',
        }}
      >
        <div>
          <Heading>Activity</Heading>
          <p style={{ font: 'var(--type-body-ui)', color: 'var(--ink-secondary)', margin: 0 }}>
            Audit events where this account is the actor or the target.
          </p>
        </div>
        <div style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'flex-end', gap: 'var(--space-3)' }}>
          <div style={{ minWidth: '18ch' }}>
            <Select
              label="Time range"
              value={when}
              options={WHEN.filter((w) => w.value !== 'custom').map((w) => ({ value: w.value, label: w.label }))}
              onChange={(e) => setWhen(e.target.value)}
            />
          </div>
          <Button variant="secondary" onClick={() => onAudit(linkQuery(filters))}>
            Open in audit log
          </Button>
        </div>
      </div>

      {log.isError && <Quiet>{messageOf(log.error)}</Quiet>}

      <AuditTable
        events={events}
        people={people}
        empty={
          <EmptyState heading="No events in this range">
            Choose a longer time range, or open the audit log for more filters.
          </EmptyState>
        }
      />
      <LoadOlder log={log} />
    </section>
  );
}

/**
 * A new password for somebody else's local account, generated rather than typed
 * (see GeneratedPassword). Not offered on your own page: the API refuses it
 * there, because changing your own password takes your current one
 * (POST /me/password).
 */
function ResetPassword({ account }: { account: Account }) {
  const [open, setOpen] = useState(false);
  const [password, setPassword] = useState('');
  const [mustChange, setMustChange] = useState(true);
  const queries = useQueryClient();

  const reset = useMutation({
    mutationFn: () =>
      api.post<void>(`/users/${account.id}/password`, { password, must_change_password: mustChange }),
    onSuccess: () => {
      void queries.invalidateQueries({ queryKey: ['users', account.id] });
      void queries.invalidateQueries({ queryKey: ['users'] });
      void queries.invalidateQueries({ queryKey: ['audit'] });
    },
  });

  const close = () => {
    setOpen(false);
    // The next reset starts from a fresh password, not this one.
    setPassword('');
    setMustChange(true);
    reset.reset();
  };

  return (
    <>
      <Button variant="ghost" onClick={() => setOpen(true)}>
        Reset password
      </Button>
      {open &&
        (reset.isSuccess ? (
          <Dialog
            open
            title={`Password reset for ${account.external_id}`}
            // Last sight of it: Pando cannot show it again once this closes.
            description="Copy the password and give it to them separately. It is not shown again after you close this."
            onClose={close}
            footer={
              <Button variant="primary" onClick={close}>
                Done
              </Button>
            }
          >
            <div style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-4)' }}>
              <PasswordToCopy value={password} />
              <Quiet>
                Every session {account.external_id} had has ended.
                {mustChange ? ' They choose a new password the next time they sign in.' : ''}
              </Quiet>
            </div>
          </Dialog>
        ) : (
          <Dialog
            open
            title={`Reset password for ${account.external_id}`}
            // What happens to the sessions is said before, not after (R-282).
            description="Pando replaces the password with the one below and signs the account out everywhere. Copy it and give it to them separately."
            onClose={close}
            footer={
              <>
                <Button variant="ghost" onClick={close}>
                  Cancel
                </Button>
                <Button
                  variant="primary"
                  disabled={reset.isPending || password === ''}
                  onClick={() => reset.mutate()}
                >
                  {reset.isPending ? 'Resetting' : 'Reset password'}
                </Button>
              </>
            }
          >
            <div style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-4)' }}>
              <GeneratedPassword
                value={password}
                onChange={(p) => {
                  if (reset.isError) reset.reset();
                  setPassword(p);
                }}
                mustChange={mustChange}
                onMustChange={setMustChange}
              />
              {reset.isError && <Banner tone="failed">{messageOf(reset.error)}</Banner>}
            </div>
          </Dialog>
        ))}
    </>
  );
}

function DeleteAccount({ account, onDeleted }: { account: Account; onDeleted: () => void }) {
  const [open, setOpen] = useState(false);
  const queries = useQueryClient();
  const remove = useMutation({
    mutationFn: () => api.del<void>(`/users/${account.id}`),
    onSuccess: () => {
      void queries.invalidateQueries({ queryKey: ['users'] });
      void queries.invalidateQueries({ queryKey: ['groups'] });
      onDeleted();
    },
  });

  return (
    <>
      <Button variant="ghost" onClick={() => setOpen(true)}>
        Delete
      </Button>
      {open && (
        <Dialog
          open
          title={`Delete ${account.external_id}`}
          // What goes with it, before it goes (R-282). Suspending is the
          // reversible one, and the dialog says so.
          description="The account signs out everywhere, loses its installation role, group memberships and access to every app, and its tokens stop working. This cannot be undone. To stop someone signing in but keep the account, suspend it instead."
          onClose={() => setOpen(false)}
          footer={
            <>
              <Button variant="ghost" onClick={() => setOpen(false)}>
                Cancel
              </Button>
              <Button variant="destructive" disabled={remove.isPending} onClick={() => remove.mutate()}>
                {remove.isPending ? 'Deleting' : 'Delete account'}
              </Button>
            </>
          }
        >
          {remove.isError && <Banner tone="failed">{messageOf(remove.error)}</Banner>}
        </Dialog>
      )}
    </>
  );
}

function Heading({ children }: { children: React.ReactNode }) {
  return <h4 style={{ font: 'var(--type-h4)', margin: '0 0 var(--space-3)' }}>{children}</h4>;
}

function Detail({ term, mono, children }: { term: string; mono?: boolean; children: React.ReactNode }) {
  return (
    <>
      <dt style={{ color: 'var(--ink-secondary)' }}>{term}</dt>
      <dd style={{ margin: 0, minWidth: 0, overflowWrap: 'anywhere', font: mono ? 'var(--type-code)' : undefined }}>
        {children}
      </dd>
    </>
  );
}
