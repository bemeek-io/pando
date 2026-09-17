// Groups and roles — R-078 and R-082, which had a full API and no screen.
//
// The two are deliberately not one screen with two tabs pretending to be the
// same idea. A group is *who*: a set of accounts, named once and granted access
// as a unit. A role is *what*: a named set of verbs. Conflating them is how an
// authorization model becomes a list of people with special powers, which is
// the thing R-078 exists to avoid.

import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Badge, Banner, Button, Checkbox, Dialog, Input, Select, Table, Tag } from '@design';

import { api } from '@api/client';
import { Quiet, Screen, messageOf } from './Accounts';

interface Group {
  id: string;
  name: string;
  source?: string;
  members?: string[];
}

interface Role {
  id: string;
  name: string;
  scope?: string;
  builtin: boolean;
  verbs: string[];
}

interface VerbRow {
  verb: string;
  scope: string;
}

interface Account {
  id: string;
  display_name?: string;
  email?: string;
  external_id: string;
}

export function Identity({ canEdit }: { canEdit: boolean }) {
  return (
    <Screen heading="Groups and roles">
      <div style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-7)' }}>
        <Groups canEdit={canEdit} />
        <Roles canEdit={canEdit} />
      </div>
    </Screen>
  );
}

// --- groups ----------------------------------------------------------------

function Groups({ canEdit }: { canEdit: boolean }) {
  const [editing, setEditing] = useState<Group | 'new' | null>(null);

  const groups = useQuery({
    queryKey: ['groups'],
    queryFn: () => api.get<{ groups: Group[] | null }>('/groups'),
  });

  const rows = groups.data?.groups ?? [];

  return (
    <section>
      <div style={{ display: 'flex', alignItems: 'baseline', justifyContent: 'space-between' }}>
        <h4 style={{ font: 'var(--type-h4)', margin: '0 0 var(--space-2)' }}>Groups</h4>
        {canEdit && (
          <Button variant="ghost" onClick={() => setEditing('new')}>
            Add group
          </Button>
        )}
      </div>

      {/* R-078: an identity provider says who is in a group; Pando decides what
          that group can do. Saying so here is what stops someone looking for
          permissions on this screen. */}
      <Quiet>
        Named sets of people, so an app is shared with a team once instead of with each person.
        What a group can do is set where the app is shared, not here.
      </Quiet>

      {groups.isError && <Banner tone="failed">{messageOf(groups.error)}</Banner>}

      <div style={{ marginTop: 'var(--space-4)' }}>
        <Table
          columns={[
            { key: 'name', header: 'Name', width: 'minmax(0,36ch)' },
            {
              key: 'members',
              header: 'People',
              width: '14ch',
              render: (row: Group) => <Badge count={row.members?.length ?? 0} />,
            },
            {
              key: 'source',
              header: 'Comes from',
              width: '20ch',
              muted: true,
              // A group synced from an identity provider is not Pando's to
              // edit: the provider is the source of truth for membership
              // (R-078), and an edit here would be overwritten at the next
              // sign-in without saying so.
              render: (row: Group) => (row.source ? row.source : 'Pando'),
            },
            {
              key: 'edit',
              header: '',
              width: '14ch',
              align: 'right',
              render: (row: Group) =>
                canEdit && !row.source ? (
                  <Button variant="ghost" onClick={() => setEditing(row)}>
                    Change people
                  </Button>
                ) : null,
            },
          ]}
          rows={rows}
          empty={<Quiet>No groups yet. Apps can still be shared with one person at a time.</Quiet>}
        />
      </div>

      {editing && <EditGroup group={editing} onClose={() => setEditing(null)} />}
    </section>
  );
}

function EditGroup({ group, onClose }: { group: Group | 'new'; onClose: () => void }) {
  const queries = useQueryClient();
  const creating = group === 'new';
  const [name, setName] = useState(creating ? '' : group.name);
  const [members, setMembers] = useState<string[]>(creating ? [] : (group.members ?? []));

  const accounts = useQuery({
    queryKey: ['users'],
    queryFn: () => api.get<{ users: Account[] }>('/users'),
  });

  const save = useMutation({
    mutationFn: () =>
      creating
        ? api.post('/groups', { name, members })
        : api.put(`/groups/${group.id}/members`, { members }),
    onSuccess: () => {
      void queries.invalidateQueries({ queryKey: ['groups'] });
      onClose();
    },
  });

  const toggle = (id: string) =>
    setMembers((current) =>
      current.includes(id) ? current.filter((m) => m !== id) : [...current, id],
    );

  return (
    <Dialog
      open
      onClose={onClose}
      title={creating ? 'Add group' : `People in ${name}`}
      description="Changing who is in a group changes what they can reach, immediately."
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button
            variant="primary"
            disabled={save.isPending || (creating && !name)}
            onClick={() => save.mutate()}
          >
            {save.isPending ? 'Saving' : 'Save'}
          </Button>
        </>
      }
    >
      <div style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-5)' }}>
        {creating && (
          <Input
            label="Name"
            value={name}
            helper="What this set of people is called, for example platform."
            onChange={(e) => setName(e.target.value)}
          />
        )}

        <div style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-3)' }}>
          {(accounts.data?.users ?? []).map((a) => (
            <Checkbox
              key={a.id}
              checked={members.includes(a.id)}
              label={a.display_name || a.email || a.external_id}
              onChange={() => toggle(a.id)}
            />
          ))}
        </div>

        {save.isError && <Banner tone="failed">{messageOf(save.error)}</Banner>}
      </div>
    </Dialog>
  );
}

// --- roles -----------------------------------------------------------------

function Roles({ canEdit }: { canEdit: boolean }) {
  const [adding, setAdding] = useState(false);

  const roles = useQuery({
    queryKey: ['roles'],
    queryFn: () => api.get<{ roles: Role[] | null }>('/roles'),
  });

  const rows = roles.data?.roles ?? [];

  return (
    <section>
      <div style={{ display: 'flex', alignItems: 'baseline', justifyContent: 'space-between' }}>
        <h4 style={{ font: 'var(--type-h4)', margin: '0 0 var(--space-2)' }}>Roles</h4>
        {canEdit && (
          <Button variant="ghost" onClick={() => setAdding(true)}>
            Add role
          </Button>
        )}
      </div>

      {/* R-081, said where someone would otherwise look for an edit button. */}
      <Quiet>
        Named sets of permissions. The four Pando ships can&rsquo;t be edited — an install that
        quietly redefined what &ldquo;viewer&rdquo; means is an install where nobody can answer what
        a viewer can do.
      </Quiet>

      {roles.isError && <Banner tone="failed">{messageOf(roles.error)}</Banner>}

      <div style={{ marginTop: 'var(--space-4)' }}>
        <Table
          columns={[
            { key: 'name', header: 'Name', width: 'minmax(0,24ch)' },
            {
              key: 'scope',
              header: 'Applies to',
              width: '18ch',
              // R-080: the two scopes are never conflated. An install role and
              // an app role are different things and this column is where a
              // reader learns that.
              render: (row: Role) =>
                row.scope === 'install' ? 'The whole installation' : 'One app',
            },
            {
              key: 'builtin',
              header: 'Source',
              width: '14ch',
              muted: true,
              render: (row: Role) => (row.builtin ? 'Pando' : 'This installation'),
            },
            {
              key: 'verbs',
              header: 'Permissions',
              width: 'minmax(0,28ch)',
              render: (row: Role) => (
                <div style={{ display: 'flex', gap: 'var(--space-2)', flexWrap: 'wrap' }}>
                  {row.verbs.map((v) => (
                    <Tag key={v} mono>
                      {v}
                    </Tag>
                  ))}
                </div>
              ),
            },
          ]}
          rows={rows}
        />
      </div>

      {adding && <AddRole onClose={() => setAdding(false)} />}
    </section>
  );
}

function AddRole({ onClose }: { onClose: () => void }) {
  const queries = useQueryClient();
  const [name, setName] = useState('');
  const [scope, setScope] = useState('app');
  const [verbs, setVerbs] = useState<string[]>([]);

  const catalog = useQuery({
    queryKey: ['verbs'],
    queryFn: () => api.get<{ verbs: VerbRow[] | null }>('/verbs'),
  });

  const save = useMutation({
    mutationFn: () => api.post('/roles', { name, scope, verbs }),
    onSuccess: () => {
      void queries.invalidateQueries({ queryKey: ['roles'] });
      onClose();
    },
  });

  // Only the verbs of the chosen scope. A role mixing the two is refused by the
  // server (R-080), and offering the choice that will be refused is worse than
  // not offering it: the scopes are different questions, not a filter.
  const available = (catalog.data?.verbs ?? []).filter((v) => v.scope === scope);

  const toggle = (verb: string) =>
    setVerbs((current) =>
      current.includes(verb) ? current.filter((v) => v !== verb) : [...current, verb],
    );

  return (
    <Dialog
      open
      onClose={onClose}
      title="Add role"
      description="A role you add can be changed later. The ones Pando ships cannot."
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button
            variant="primary"
            disabled={save.isPending || !name || verbs.length === 0}
            onClick={() => save.mutate()}
          >
            {save.isPending ? 'Saving' : 'Add role'}
          </Button>
        </>
      }
    >
      <div style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-5)' }}>
        <Input
          label="Name"
          value={name}
          helper="What this set of permissions is called, for example deployer."
          onChange={(e) => setName(e.target.value)}
        />

        <Select
          label="Applies to"
          value={scope}
          options={[
            { value: 'app', label: 'One app it is granted on' },
            { value: 'install', label: 'The whole installation' },
          ]}
          helper="A role is one or the other. Holding permissions over the installation is not the same as holding them over an app."
          onChange={(e) => {
            setScope(e.target.value);
            setVerbs([]);
          }}
        />

        <div style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-3)' }}>
          {available.map((v) => (
            <Checkbox
              key={v.verb}
              checked={verbs.includes(v.verb)}
              label={v.verb}
              onChange={() => toggle(v.verb)}
            />
          ))}
        </div>

        {save.isError && <Banner tone="failed">{messageOf(save.error)}</Banner>}
      </div>
    </Dialog>
  );
}
