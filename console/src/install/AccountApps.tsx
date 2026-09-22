// The apps an account has something on, on its page: its role for managing
// each, whether it can use each, and — where the viewer may change that — the
// controls to change it.
//
// Editable only where the server says so. `can_manage` on each row is the
// authorizer's own answer for app.grants.manage on that app (GET
// /users/{id}/apps), so a row the viewer cannot change is shown as text rather
// than as a control that would be refused. Access through a group is always
// text here: it belongs to the group, and changing it would change it for
// everyone in it.

import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Banner, Button, Checkbox, Dialog, EmptyState, Select, Tag } from '@design';

import { api } from '@api/client';
import { InstallVerb, useInstallVerb } from '../app/principal';
import { Table } from '../ui/Table';
import type { Account, Role } from './Accounts';
import { Quiet, messageOf, sentence } from './Accounts';

interface Grant {
  grant_id: string;
  plane: 'control' | 'data';
  role_id?: string;
  role_name?: string;
  via: 'user' | 'group';
  group_id?: string;
  group_name?: string;
}

interface AppAccess {
  app_id: string;
  app_name: string;
  owner: boolean;
  can_manage: boolean;
  control: Grant[];
  data: Grant[];
}

export function AccountApps({ account }: { account: Account }) {
  const key = ['users', account.id, 'apps'];
  const access = useQuery({
    queryKey: key,
    queryFn: () => api.get<{ apps: AppAccess[] }>(`/users/${account.id}/apps`),
  });
  // The roles an app can be granted with: the three built-ins and any custom
  // app role.
  const roles = useQuery({
    queryKey: ['roles', 'app'],
    queryFn: () => api.get<{ roles: Role[] }>('/roles?scope=app'),
  });
  // Every app is manageable by someone holding install.apps.manage, so for
  // them — and only them — the page offers access to an app the account does
  // not have yet. Anyone else manages the apps they were granted, from each
  // app's Sharing tab.
  const everyApp = useInstallVerb(InstallVerb.AppsManage);
  const [adding, setAdding] = useState(false);

  const rows = access.data?.apps ?? [];

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
          <h4 style={{ font: 'var(--type-h4)', margin: '0 0 var(--space-3)' }}>Apps</h4>
          <p style={{ font: 'var(--type-body-ui)', color: 'var(--ink-secondary)', margin: 0 }}>
            The role this account manages each app with, and whether it can open it.
          </p>
        </div>
        {everyApp && (
          <Button variant="secondary" onClick={() => setAdding(true)}>
            Give access to an app
          </Button>
        )}
      </div>

      {access.isError && <Quiet>{messageOf(access.error)}</Quiet>}

      <Table
        columns={[
          {
            key: 'app_name',
            header: 'App',
            width: 'minmax(0,1fr)',
            filter: 'text',
            render: (row: AppAccess) => (
              <span style={{ display: 'inline-flex', flexWrap: 'wrap', alignItems: 'center', gap: 'var(--space-2)' }}>
                {row.app_name}
                {row.owner && <Tag>Owner of record</Tag>}
              </span>
            ),
          },
          {
            key: 'role',
            header: 'Role',
            width: 'minmax(0,26ch)',
            render: (row: AppAccess) => (
              <div style={{ padding: 'var(--space-2) 0' }}>
                <RoleCell account={account} row={row} roles={roles.data?.roles ?? []} />
              </div>
            ),
          },
          {
            key: 'use',
            header: 'Can open',
            width: '16ch',
            render: (row: AppAccess) => <UseCell account={account} row={row} />,
          },
        ]}
        rows={rows.map((r) => ({ ...r, id: r.app_id }))}
        empty={
          <EmptyState heading="No apps">
            {everyApp
              ? 'This account has no access to any app. Give it access to one above.'
              : 'This account has no access to any app you can see.'}
          </EmptyState>
        }
      />

      {adding && (
        <GiveAccess
          account={account}
          has={new Set(rows.map((r) => r.app_id))}
          roles={roles.data?.roles ?? []}
          onClose={() => setAdding(false)}
        />
      )}
    </section>
  );
}

/** The account's own grant for managing the app, editable if the viewer may;
 *  and any role it holds through a group, as text. */
function RoleCell({ account, row, roles }: { account: Account; row: AppAccess; roles: Role[] }) {
  const own = row.control.find((g) => g.via === 'user');
  const viaGroups = row.control.filter((g) => g.via === 'group');
  const change = useGrantChange(account, row.app_id);
  const nameOf = (g: Grant) => sentence(g.role_name || roles.find((r) => r.id === g.role_id)?.name || g.role_id || '');

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-1)' }}>
      {row.can_manage ? (
        <Select
          aria-label={`Role on ${row.app_name}`}
          value={own?.role_id ?? ''}
          disabled={change.isPending}
          onChange={(e) => {
            const role = e.target.value;
            if (!role && own) change.mutate({ method: 'delete', grant: own.grant_id });
            else if (role && own) change.mutate({ method: 'patch', grant: own.grant_id, role });
            else if (role) change.mutate({ method: 'post', plane: 'control', role });
          }}
          options={[{ value: '', label: 'None' }, ...roles.map((r) => ({ value: r.id, label: sentence(r.name) }))]}
          helper={change.isError ? messageOf(change.error) : undefined}
        />
      ) : (
        <span>{own ? nameOf(own) : viaGroups.length > 0 ? '' : 'None'}</span>
      )}
      {viaGroups.map((g) => (
        <span key={g.grant_id} style={{ font: 'var(--type-caption)', color: 'var(--ink-secondary)' }}>
          {nameOf(g)} through {g.group_name || g.group_id}
        </span>
      ))}
    </div>
  );
}

function UseCell({ account, row }: { account: Account; row: AppAccess }) {
  const own = row.data.find((g) => g.via === 'user');
  const viaGroup = row.data.find((g) => g.via === 'group');
  const change = useGrantChange(account, row.app_id);
  // An owner opens the app by owning it (R-072), grant or no grant.
  const opens = Boolean(own || viaGroup || row.owner);

  if (!row.can_manage || row.owner) {
    return (
      <span style={{ color: opens ? undefined : 'var(--ink-tertiary)' }}>
        {opens ? (row.owner && !own ? 'Yes, as owner' : 'Yes') : 'No'}
      </span>
    );
  }
  return (
    <Checkbox
      aria-label={`Can open ${row.app_name}`}
      label={viaGroup && !own ? 'Through a group' : undefined}
      checked={Boolean(own || viaGroup)}
      disabled={change.isPending || Boolean(viaGroup && !own)}
      onChange={(e) => {
        if (e.target.checked) change.mutate({ method: 'post', plane: 'data' });
        else if (own) change.mutate({ method: 'delete', grant: own.grant_id });
      }}
    />
  );
}

type Change =
  | { method: 'post'; plane: 'control' | 'data'; role?: string }
  | { method: 'patch'; grant: string; role: string }
  | { method: 'delete'; grant: string };

function useGrantChange(account: Account, appID: string) {
  const queries = useQueryClient();
  return useMutation({
    mutationFn: (c: Change) => {
      const base = `/apps/${appID}/grants`;
      if (c.method === 'post') {
        return api.post<unknown>(base, {
          plane: c.plane,
          principal_kind: 'user',
          principal_id: account.id,
          ...(c.role ? { role_id: c.role } : {}),
        });
      }
      if (c.method === 'patch') return api.patch<unknown>(`${base}/${c.grant}`, { role_id: c.role });
      return api.del<unknown>(`${base}/${c.grant}`);
    },
    onSettled: () => {
      void queries.invalidateQueries({ queryKey: ['users', account.id, 'apps'] });
      void queries.invalidateQueries({ queryKey: ['apps', appID] });
    },
  });
}

/** Access to an app the account has nothing on yet — for someone who manages
 *  every app, so every app offered is one they can grant. */
function GiveAccess({
  account,
  has,
  roles,
  onClose,
}: {
  account: Account;
  has: Set<string>;
  roles: Role[];
  onClose: () => void;
}) {
  const apps = useQuery({
    queryKey: ['apps'],
    queryFn: () => api.get<{ apps: { id: string; name: string }[] }>('/apps'),
  });
  const offered = (apps.data?.apps ?? []).filter((a) => !has.has(a.id));
  const [appID, setAppID] = useState('');
  const [role, setRole] = useState('');
  const [opens, setOpens] = useState(true);
  const queries = useQueryClient();

  const give = useMutation({
    mutationFn: async () => {
      const base = `/apps/${appID}/grants`;
      const who = { principal_kind: 'user', principal_id: account.id };
      if (role) await api.post<unknown>(base, { plane: 'control', role_id: role, ...who });
      if (opens) await api.post<unknown>(base, { plane: 'data', ...who });
    },
    onSettled: () => void queries.invalidateQueries({ queryKey: ['users', account.id, 'apps'] }),
    onSuccess: onClose,
  });

  return (
    <Dialog
      open
      title="Give access to an app"
      description={`What ${account.display_name || account.external_id} can do on one more app.`}
      onClose={onClose}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button
            variant="primary"
            disabled={give.isPending || !appID || (!role && !opens)}
            onClick={() => give.mutate()}
          >
            {give.isPending ? 'Giving access' : 'Give access'}
          </Button>
        </>
      }
    >
      <div style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-4)' }}>
        {offered.length === 0 && !apps.isPending ? (
          <Quiet>This account already has access to every app.</Quiet>
        ) : (
          <Select
            label="App"
            value={appID}
            onChange={(e) => setAppID(e.target.value)}
            options={[{ value: '', label: 'Choose an app' }, ...offered.map((a) => ({ value: a.id, label: a.name }))]}
          />
        )}
        <Select
          label="Role"
          value={role}
          onChange={(e) => setRole(e.target.value)}
          helper="What the account can change on the app. None to only let it open the app."
          options={[{ value: '', label: 'None' }, ...roles.map((r) => ({ value: r.id, label: sentence(r.name) }))]}
        />
        <Checkbox label="Can open the app" checked={opens} onChange={(e) => setOpens(e.target.checked)} />
        {give.isError && <Banner tone="failed">{messageOf(give.error)}</Banner>}
      </div>
    </Dialog>
  );
}
