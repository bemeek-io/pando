// Accounts — the screen behind install.users.manage and install.view.
//
// This is where an administrator hands over. Until it existed, the only account
// that could administer a Pando install was the one bootstrap created on first
// run: the verbs existed, the role existed, and nothing could write a second
// grant. An install had exactly one administrator forever.
//
// Two separate actions on every row, because they are two separate powers.
// Suspending an account stops it signing in (R-049); changing its role changes
// what it may do. The API keeps them on different routes and different verbs,
// and this screen does not merge them back together.

import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Button, Dialog, Input, Select, StatusIndicator, Table, Tag } from '@design';

import { api, RequestFailed } from '@api/client';
import { InstallVerb, useInstallVerb, usePrincipal } from '../app/principal';

interface Account {
  id: string;
  external_id: string;
  email?: string;
  display_name?: string;
  status: string;
  must_change_password: boolean;
  install_role_id: string;
}

interface Role {
  id: string;
  name: string;
  builtin: boolean;
  verbs: string[];
}

export function Accounts() {
  const manage = useInstallVerb(InstallVerb.UsersManage);
  const me = usePrincipal();
  const [adding, setAdding] = useState(false);

  const accounts = useQuery({
    queryKey: ['users'],
    queryFn: () => api.get<{ users: Account[] }>('/users'),
  });
  const roles = useQuery({
    queryKey: ['roles'],
    queryFn: () => api.get<{ roles: Role[] }>('/roles'),
  });

  const rows = accounts.data?.users ?? [];

  return (
    <Screen
      heading="Accounts"
      action={
        manage ? (
          <Button variant="primary" onClick={() => setAdding(true)}>
            Add account
          </Button>
        ) : undefined
      }
    >
      {accounts.isError && <Quiet>{messageOf(accounts.error)}</Quiet>}

      <Table
        columns={[
          { key: 'external_id', header: 'Username', width: 'minmax(0,24ch)' },
          {
            key: 'display_name',
            header: 'Name',
            width: 'minmax(0,22ch)',
            muted: true,
            render: (row: Account) => row.display_name || row.email || '—',
          },
          {
            key: 'status',
            header: 'Status',
            width: '14ch',
            render: (row: Account) => (
              <StatusIndicator
                // Suspended is a stopped account, not a failed one. Marker red
                // is for failure and destruction, and an account somebody
                // deliberately suspended is neither.
                status={row.status === 'active' ? 'running' : 'stopped'}
                label={row.status === 'active' ? 'Active' : 'Suspended'}
              />
            ),
          },
          {
            key: 'install_role_id',
            header: 'Installation role',
            width: 'minmax(0,24ch)',
            render: (row: Account) =>
              manage ? (
                <RolePicker
                  account={row}
                  roles={roles.data?.roles ?? []}
                  // You can demote yourself when somebody else can still
                  // administer — the server refuses the last one. What the
                  // console will not do is make that look like a normal edit.
                  isSelf={row.id === me.data?.user_id}
                />
              ) : (
                <RoleLabel roleID={row.install_role_id} roles={roles.data?.roles ?? []} />
              ),
          },
          {
            key: 'actions',
            header: '',
            width: '16ch',
            align: 'right',
            render: (row: Account) =>
              manage && row.id !== me.data?.user_id ? <StatusToggle account={row} /> : null,
          },
        ]}
        rows={rows}
      />

      {adding && <AddAccount onClose={() => setAdding(false)} />}
    </Screen>
  );
}

function RoleLabel({ roleID, roles }: { roleID: string; roles: Role[] }) {
  if (!roleID) return <span style={{ color: 'var(--ink-tertiary)' }}>None</span>;
  const role = roles.find((r) => r.id === roleID);
  return <Tag>{role ? sentence(role.name) : roleID}</Tag>;
}

function RolePicker({
  account,
  roles,
  isSelf,
}: {
  account: Account;
  roles: Role[];
  isSelf: boolean;
}) {
  const queries = useQueryClient();
  const [error, setError] = useState<string>();

  const change = useMutation({
    mutationFn: (roleID: string) =>
      roleID === ''
        ? api.del<void>(`/users/${account.id}/role`)
        : api.put<unknown>(`/users/${account.id}/role`, { role_id: roleID }),
    onSuccess: () => {
      setError(undefined);
      void queries.invalidateQueries({ queryKey: ['users'] });
      // Your own verbs may have just changed, and the sidebar reads them.
      void queries.invalidateQueries({ queryKey: ['me'] });
    },
    // The server's refusal, shown as written — "this is the only account that
    // can manage accounts" is exactly what the person needs to read.
    onError: (e) => setError(messageOf(e)),
  });

  return (
    <Select
      value={account.install_role_id}
      disabled={change.isPending}
      onChange={(e) => change.mutate(e.target.value)}
      options={[
        { value: '', label: isSelf ? 'None — give up admin' : 'None' },
        ...roles.map((r) => ({ value: r.id, label: sentence(r.name) })),
      ]}
      helper={error}
    />
  );
}

function StatusToggle({ account }: { account: Account }) {
  const queries = useQueryClient();
  const suspended = account.status !== 'active';

  const set = useMutation({
    mutationFn: (status: string) => api.patch<void>(`/users/${account.id}`, { status }),
    onSuccess: () => void queries.invalidateQueries({ queryKey: ['users'] }),
  });

  // Suspending ends every session the account holds, immediately (R-048). That
  // is worth saying before it happens rather than after.
  return (
    <Button
      variant={suspended ? 'secondary' : 'ghost'}
      disabled={set.isPending}
      onClick={() => set.mutate(suspended ? 'active' : 'suspended')}
    >
      {suspended ? 'Reinstate' : 'Suspend'}
    </Button>
  );
}

function AddAccount({ onClose }: { onClose: () => void }) {
  const [username, setUsername] = useState('');
  const [displayName, setDisplayName] = useState('');
  const [password, setPassword] = useState('');
  const queries = useQueryClient();

  const create = useMutation({
    mutationFn: () =>
      api.post<unknown>('/users', {
        username,
        password,
        display_name: displayName,
      }),
    onSuccess: () => {
      void queries.invalidateQueries({ queryKey: ['users'] });
      onClose();
    },
  });

  return (
    <Dialog
      open
      title="Add account"
      description="A local account. Give the person the password you set here; they choose their own after signing in."
      onClose={onClose}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button
            variant="primary"
            disabled={create.isPending || username === '' || password === ''}
            onClick={() => create.mutate()}
          >
            {create.isPending ? 'Adding' : 'Add account'}
          </Button>
        </>
      }
    >
      <div style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-4)' }}>
        <Input label="Username" value={username} onChange={(e) => setUsername(e.target.value)} />
        <Input
          label="Name"
          value={displayName}
          helper="Shown in the console and in the audit log."
          onChange={(e) => setDisplayName(e.target.value)}
        />
        <Input
          label="Password"
          type="password"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          error={create.isError ? messageOf(create.error) : undefined}
        />
      </div>
    </Dialog>
  );
}

export function Screen({
  heading,
  action,
  children,
}: {
  heading: string;
  action?: React.ReactNode;
  children: React.ReactNode;
}) {
  // Left-aligned and uncapped: the rules under a table run to the window's
  // edge, and the screen's action sits beside its heading rather than at the
  // far end of a measure.
  return (
    <div>
      <header
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: 'var(--space-4)',
          padding: 'var(--space-6) var(--console-padding) var(--space-4)',
        }}
      >
        <h3 style={{ font: 'var(--type-h3)', margin: 0 }}>{heading}</h3>
        {action}
      </header>
      <div style={{ padding: '0 var(--console-padding) var(--space-7)' }}>{children}</div>
    </div>
  );
}

export function Quiet({ children }: { children: React.ReactNode }) {
  return <p style={{ font: 'var(--type-body-ui)', color: 'var(--ink-secondary)' }}>{children}</p>;
}

export function messageOf(error: unknown): string {
  if (error instanceof RequestFailed) return error.message;
  return 'Pando could not reach the server. Reload the page to try again.';
}

/** Role names are stored lowercase; the design system sets everything in
 *  sentence case. */
function sentence(s: string): string {
  return s.charAt(0).toUpperCase() + s.slice(1);
}
