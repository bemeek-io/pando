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

import { api, RequestFailed } from '@api/client';
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
  const [deleting, setDeleting] = useState<Group | null>(null);

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
              width: '24ch',
              align: 'right',
              // Not for a synced group: the identity provider would make it
              // again at the next sign-in (R-078).
              render: (row: Group) =>
                canEdit && !row.source ? (
                  <span style={{ display: 'inline-flex', gap: 'var(--space-2)' }}>
                    <Button variant="ghost" onClick={() => setEditing(row)}>
                      Change people
                    </Button>
                    <Button variant="ghost" onClick={() => setDeleting(row)}>
                      Delete
                    </Button>
                  </span>
                ) : null,
            },
          ]}
          rows={rows}
          empty={<Quiet>No groups yet. Apps can still be shared with one person at a time.</Quiet>}
        />
      </div>

      {editing && <EditGroup group={editing} onClose={() => setEditing(null)} />}
      {deleting && <DeleteGroup group={deleting} onClose={() => setDeleting(null)} />}
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
  const [deleting, setDeleting] = useState<Role | null>(null);

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
        Named sets of permissions. The five Pando ships can&rsquo;t be edited or deleted — an install that
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
                <div
                  style={{
                    display: 'flex',
                    gap: 'var(--space-2)',
                    flexWrap: 'wrap',
                    padding: 'var(--space-2) 0',
                  }}
                >
                  {row.verbs.map((v) => (
                    <Tag key={v} mono>
                      {v}
                    </Tag>
                  ))}
                </div>
              ),
            },
            {
              key: 'delete',
              header: '',
              width: '10ch',
              align: 'right',
              // R-081: built-ins are not deletable, so they are not offered.
              render: (row: Role) =>
                canEdit && !row.builtin ? (
                  <Button variant="ghost" onClick={() => setDeleting(row)}>
                    Delete
                  </Button>
                ) : null,
            },
          ]}
          rows={rows}
        />
      </div>

      {adding && <AddRole onClose={() => setAdding(false)} />}
      {deleting && <DeleteRole role={deleting} onClose={() => setDeleting(null)} />}
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
          helper="What this set of permissions is called, for example support or release manager."
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

// --- deleting ----------------------------------------------------------------

/** The server's message and, when it gave one, its remedy (R-105). A refusal
 *  here is the last-administrator rule, and the remedy is the way through. */
function refusal(error: unknown): string {
  const remedy = error instanceof RequestFailed ? error.remedy : undefined;
  return remedy ? `${messageOf(error)} ${remedy}` : messageOf(error);
}
//
// Both are allowed, and both take access away from people who may not know it
// is happening, so each says whose access goes and whose stays before anything
// is deleted. The server refuses the one case that would lock the installation
// (R-088) and says why; that message is shown as-is.

function DeleteGroup({ group, onClose }: { group: Group; onClose: () => void }) {
  const queries = useQueryClient();
  const remove = useMutation({
    mutationFn: () => api.del<void>(`/groups/${group.id}`),
    onSuccess: () => {
      void queries.invalidateQueries();
      onClose();
    },
  });
  const people = group.members?.length ?? 0;

  return (
    <Dialog
      open
      onClose={onClose}
      title={`Delete the ${group.name} group`}
      description="Everyone in it loses what was shared with the group."
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button variant="destructive" disabled={remove.isPending} onClick={() => remove.mutate()}>
            {remove.isPending ? 'Deleting' : 'Delete group'}
          </Button>
        </>
      }
    >
      <div style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-3)' }}>
        <p style={{ font: 'var(--type-body-ui)', margin: 0 }}>
          {people === 1 ? 'The one person' : `The ${people} people`} in {group.name} lose every app that was
          shared with the group — both opening it and any role the group had for managing it. Their accounts
          are kept, and so is anything shared with them directly or through another group.
        </p>
        <p style={{ font: 'var(--type-body-ui)', color: 'var(--ink-secondary)', margin: 0 }}>
          This can&rsquo;t be undone. Making a group with the same name later does not bring the access back.
        </p>
        {remove.isError && <Banner tone="failed">{refusal(remove.error)}</Banner>}
      </div>
    </Dialog>
  );
}

function DeleteRole({ role, onClose }: { role: Role; onClose: () => void }) {
  const queries = useQueryClient();
  const remove = useMutation({
    mutationFn: () => api.del<void>(`/roles/${role.id}`),
    onSuccess: () => {
      void queries.invalidateQueries();
      onClose();
    },
  });

  return (
    <Dialog
      open
      onClose={onClose}
      title={`Delete the ${role.name} role`}
      description="Anyone given this role loses what it allowed."
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button variant="destructive" disabled={remove.isPending} onClick={() => remove.mutate()}>
            {remove.isPending ? 'Deleting' : 'Delete role'}
          </Button>
        </>
      }
    >
      <div style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-3)' }}>
        <p style={{ font: 'var(--type-body-ui)', margin: 0 }}>
          {role.scope === 'install'
            ? `Everyone given ${role.name} across the installation loses the permissions it gave them.`
            : `Everyone given ${role.name} on an app — directly or through a group — loses the access it gave them on that app.`}{' '}
          They keep anything they hold another way.
        </p>
        <p style={{ font: 'var(--type-body-ui)', color: 'var(--ink-secondary)', margin: 0 }}>
          This can&rsquo;t be undone. Making a role with the same name later does not give it back to anyone.
        </p>
        {remove.isError && <Banner tone="failed">{refusal(remove.error)}</Banner>}
      </div>
    </Dialog>
  );
}
