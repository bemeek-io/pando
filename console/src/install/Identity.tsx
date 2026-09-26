// Groups and roles — R-078 and R-082, which had a full API and no screen.
//
// The two are deliberately not one screen with two tabs pretending to be the
// same idea. A group is *who*: a set of accounts, named once and granted access
// as a unit. A role is *what*: a named set of verbs. Conflating them is how an
// authorization model becomes a list of people with special powers, which is
// the thing R-078 exists to avoid.

import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Badge, Banner, Button, Checkbox, Dialog, Input, Select, Tag } from '@design';

import { api } from '@api/client';
import { Quiet, RoleLabel, Screen, messageOf, refusal, sentence } from './Accounts';
import { AccountApps } from './AccountApps';
import { NoMatches, SearchField } from '../ui/SearchField';
import { matches } from '../ui/search';
import { Table } from '../ui/Table';
import { LineSkeleton, Loading } from '../ui/Loading';
import { AnsweredBy, AskAI } from '../ui/AskAI';
import { useAIFunctionOn } from './AIFunctions';

interface Group {
  id: string;
  name: string;
  source?: string;
  members?: string[];
  /** The installation role everyone in the group holds; '' for none. */
  install_role_id?: string;
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
  // One search for both lists: they are one screen, and someone looking for
  // "engineering" should not have to know first whether it is a group or a role.
  const [query, setQuery] = useState('');
  return (
    <Screen
      heading="Groups and roles"
      action={<SearchField value={query} onChange={setQuery} placeholder="Search groups and roles" />}
    >
      <div
        style={{
          display: 'flex',
          flexDirection: 'column',
          gap: 'var(--space-7)',
        }}
      >
        {canEdit && <DraftAccess />}
        <Groups canEdit={canEdit} query={query} />
        <Roles canEdit={canEdit} query={query} />
      </div>
    </Screen>
  );
}

// --- drafting with AI -------------------------------------------------------

/** What POST /ai/access/draft answers (R-343). */
interface AccessDraft {
  role?: { name: string; scope: string; verbs: string[] };
  group?: { name: string; members?: string[] };
  reply: string;
  refused?: string[];
  adapter_id?: string;
  model?: string;
}

/**
 * A role and a group from a sentence (R-343). The AI drafts; Create makes them
 * through the same endpoints Add group and Add role use, under your own
 * authority, so nothing is created that you could not have created yourself.
 * Shown only when access drafting is assigned to an adapter.
 */
function DraftAccess() {
  const queries = useQueryClient();
  const on = useAIFunctionOn('draft_access');
  const [created, setCreated] = useState<string | null>(null);

  const draft = useMutation({
    mutationFn: (description: string) => api.post<AccessDraft>('/ai/access/draft', { description }),
    onSuccess: () => setCreated(null),
  });
  const create = useMutation({
    mutationFn: async (d: AccessDraft) => {
      const made: string[] = [];
      let roleID = '';
      if (d.role) {
        const role = await api.post<{ id: string }>('/roles', d.role);
        roleID = role.id;
        made.push(`the role ${d.role.name}`);
      }
      if (d.group) {
        const group = await api.post<{ id: string }>('/groups', { name: d.group.name, members: d.group.members ?? [] });
        made.push(`the group ${d.group.name}`);
        // An installation role is held by a group directly (R-080). An app
        // role is granted on each app, which is that app's decision.
        if (roleID && d.role?.scope === 'install') {
          await api.put(`/groups/${group.id}/role`, { role_id: roleID });
        }
      }
      return made;
    },
    onSuccess: (made) => {
      setCreated(`Created ${made.join(' and ')}.`);
      draft.reset();
      void queries.invalidateQueries({ queryKey: ['groups'] });
      void queries.invalidateQueries({ queryKey: ['roles'] });
    },
  });

  if (!on) return null;
  const d = draft.data;
  return (
    <section style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-2)', maxWidth: '68ch' }}>
      <AskAI
        heading="Ask AI to draft access"
        explanation="Describe who should be able to do what. AI drafts a role and a group from Pando's permissions; nothing is created until you choose Create."
        label="Who should be able to do what"
        placeholder="Release managers can deploy and restart any app"
        pending={draft.isPending}
        error={draft.error}
        onAsk={(text) => draft.mutate(text)}
      />
      {created && <Quiet>{created}</Quiet>}
      {d && (
        <>
          {d.reply && <p style={{ font: 'var(--type-body-ui)', margin: 0 }}>{d.reply}</p>}
          {d.role && (
            <div style={{ display: 'flex', flexWrap: 'wrap', gap: 'var(--space-2)', alignItems: 'center' }}>
              <span style={{ font: 'var(--type-label)' }}>
                Role: {d.role.name} ({d.role.scope === 'install' ? 'installation' : 'app'})
              </span>
              {d.role.verbs.map((v) => (
                <Tag key={v}>{v}</Tag>
              ))}
            </div>
          )}
          {d.group && (
            <span style={{ font: 'var(--type-label)' }}>
              Group: {d.group.name}, {(d.group.members?.length ?? 0) === 1 ? '1 person' : `${d.group.members?.length ?? 0} people`}
            </span>
          )}
          {(d.refused ?? []).map((r) => (
            <Quiet key={r}>{r}</Quiet>
          ))}
          <AnsweredBy adapter={d.adapter_id} model={d.model} />
          {(d.role || d.group) && (
            <div>
              <Button variant="secondary" disabled={create.isPending} onClick={() => create.mutate(d)}>
                Create
              </Button>
            </div>
          )}
          {create.isError && <Quiet>{refusal(create.error)}</Quiet>}
        </>
      )}
    </section>
  );
}

// --- groups ----------------------------------------------------------------

function Groups({ canEdit, query }: { canEdit: boolean; query: string }) {
  const [editing, setEditing] = useState<Group | 'new' | null>(null);
  const [deleting, setDeleting] = useState<Group | null>(null);
  const [showing, setShowing] = useState<string | null>(null);

  const groups = useQuery({
    queryKey: ['groups'],
    queryFn: () => api.get<{ groups: Group[] | null }>('/groups'),
  });
  // Installation roles only: a group's role here applies across the
  // installation (R-080). What it can do on one app is set in its apps.
  const roles = useQuery({
    queryKey: ['roles'],
    queryFn: () => api.get<{ roles: Role[] }>('/roles'),
  });
  const installRoles = roles.data?.roles ?? [];
  const roleName = (id?: string) => installRoles.find((r) => r.id === id)?.name ?? '';

  const all = groups.data?.groups ?? [];
  const rows = all.filter((g) => matches(query, g.name, g.source ?? 'Pando', roleName(g.install_role_id)));
  // Looked up rather than kept, so the panel follows a rename or a delete.
  const shown = all.find((g) => g.id === showing);

  return (
    <section>
      <div
        style={{
          display: 'flex',
          alignItems: 'baseline',
          justifyContent: 'space-between',
        }}
      >
        <h4 style={{ font: 'var(--type-h4)', margin: '0 0 var(--space-2)' }}>Groups</h4>
        {canEdit && (
          <Button variant="secondary" onClick={() => setEditing('new')}>
            Add group
          </Button>
        )}
      </div>

      {/* R-078: an identity provider says who is in a group; Pando decides what
          that group can do. Saying so here is what stops someone looking for
          permissions on this screen. */}
      <Quiet>
        Named sets of people, so access is given to a team once instead of to each person. Everyone in a group holds its
        role and its access to apps.
      </Quiet>

      {groups.isError && <Banner tone="failed">{messageOf(groups.error)}</Banner>}

      <div style={{ marginTop: 'var(--space-4)' }}>
        <Table
          loading={groups.isPending}
          skeletonRows={3}
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
              key: 'role',
              header: 'Role',
              width: 'minmax(0,24ch)',
              // Allowed on a synced group too: the provider says who is in it,
              // Pando says what it can do (R-078).
              render: (row: Group) =>
                canEdit ? (
                  <div style={{ padding: 'var(--space-2) 0' }}>
                    <GroupRole group={row} roles={installRoles} />
                  </div>
                ) : (
                  <RoleLabel roleID={row.install_role_id ?? ''} roles={installRoles} />
                ),
            },
            {
              key: 'edit',
              header: '',
              width: '32ch',
              align: 'right',
              render: (row: Group) => (
                <span style={{ display: 'inline-flex', gap: 'var(--space-2)' }}>
                  <Button variant="secondary" onClick={() => setShowing(showing === row.id ? null : row.id)}>
                    Apps
                  </Button>
                  {/* Not for a synced group: the identity provider would make
                      it again at the next sign-in (R-078). */}
                  {canEdit && !row.source && (
                    <>
                      <Button variant="secondary" onClick={() => setEditing(row)}>
                        Change people
                      </Button>
                      <Button variant="secondary" onClick={() => setDeleting(row)}>
                        Delete
                      </Button>
                    </>
                  )}
                </span>
              ),
            },
          ]}
          rows={rows}
          empty={
            query.trim() && all.length > 0 ? (
              <NoMatches what="groups" query={query} />
            ) : (
              <Quiet>No groups yet. Apps can still be shared with one person at a time.</Quiet>
            )
          }
        />
      </div>

      {/* Below the table rather than in a dialog: giving access to an app
          opens a dialog of its own, and a dialog over a dialog is one too
          many. */}
      {shown && (
        <div style={{ marginTop: 'var(--space-6)' }}>
          <AccountApps
            key={shown.id}
            principal={{ kind: 'group', id: shown.id, name: shown.name }}
            heading={`Apps for ${shown.name}`}
            action={
              <Button variant="ghost" onClick={() => setShowing(null)}>
                Close
              </Button>
            }
          />
        </div>
      )}

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
      creating ? api.post('/groups', { name, members }) : api.put(`/groups/${group.id}/members`, { members }),
    onSuccess: () => {
      void queries.invalidateQueries({ queryKey: ['groups'] });
      onClose();
    },
  });

  const toggle = (id: string) =>
    setMembers((current) => (current.includes(id) ? current.filter((m) => m !== id) : [...current, id]));

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
          <Button variant="primary" disabled={save.isPending || (creating && !name)} onClick={() => save.mutate()}>
            {save.isPending ? 'Saving' : 'Save'}
          </Button>
        </>
      }
    >
      <div
        style={{
          display: 'flex',
          flexDirection: 'column',
          gap: 'var(--space-5)',
        }}
      >
        {creating && (
          <Input
            label="Name"
            value={name}
            helper="What this set of people is called, for example platform."
            onChange={(e) => setName(e.target.value)}
          />
        )}

        <div
          style={{
            display: 'flex',
            flexDirection: 'column',
            gap: 'var(--space-3)',
          }}
        >
          {/* A checkbox's line each, until the accounts arrive. */}
          {accounts.isPending && (
            <Loading>
              {[0, 1, 2].map((n) => (
                <LineSkeleton key={n} width={`${24 - n * 4}ch`} />
              ))}
            </Loading>
          )}
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

/** The group's installation role. Everyone in the group holds it, so a new
 *  member of a team gets what the team has without being given it one by one. */
function GroupRole({ group, roles }: { group: Group; roles: Role[] }) {
  const queries = useQueryClient();
  const [error, setError] = useState<string>();

  const change = useMutation({
    mutationFn: (roleID: string) =>
      roleID === ''
        ? api.del<void>(`/groups/${group.id}/role`)
        : api.put<unknown>(`/groups/${group.id}/role`, { role_id: roleID }),
    onSuccess: () => {
      setError(undefined);
      void queries.invalidateQueries({ queryKey: ['groups'] });
      // Every member's verbs just changed, possibly your own.
      void queries.invalidateQueries({ queryKey: ['users'] });
      void queries.invalidateQueries({ queryKey: ['me'] });
    },
    // Taking the role away from the only group that lets anyone manage
    // accounts is refused (R-088); the remedy says what to do instead.
    onError: (e) => setError(refusal(e)),
  });

  return (
    <Select
      aria-label={`Role for ${group.name}`}
      value={group.install_role_id ?? ''}
      disabled={change.isPending}
      onChange={(e) => change.mutate(e.target.value)}
      options={[{ value: '', label: 'None' }, ...roles.map((r) => ({ value: r.id, label: sentence(r.name) }))]}
      helper={error}
    />
  );
}

// --- roles -----------------------------------------------------------------

function Roles({ canEdit, query }: { canEdit: boolean; query: string }) {
  const [adding, setAdding] = useState(false);
  const [deleting, setDeleting] = useState<Role | null>(null);

  const roles = useQuery({
    // Both scopes. Without it this listed only the installation roles, and the
    // app roles — three of Pando's five, and any made here — were not on the
    // screen that says what every role is.
    queryKey: ['roles', 'all'],
    queryFn: () => api.get<{ roles: Role[] | null }>('/roles?scope=all'),
  });

  // By name, by what it applies to, and by any permission it holds — so
  // "secrets" finds every role that can touch them.
  const rows = (roles.data?.roles ?? []).filter((r) =>
    matches(
      query,
      r.name,
      r.scope === 'install' ? 'whole installation' : 'one app',
      r.builtin ? 'Pando' : 'This installation',
      ...r.verbs,
    ),
  );

  return (
    <section>
      <div
        style={{
          display: 'flex',
          alignItems: 'baseline',
          justifyContent: 'space-between',
        }}
      >
        <h4 style={{ font: 'var(--type-h4)', margin: '0 0 var(--space-2)' }}>Roles</h4>
        {canEdit && (
          <Button variant="secondary" onClick={() => setAdding(true)}>
            Add role
          </Button>
        )}
      </div>

      {/* R-081, said where someone would otherwise look for an edit button. */}
      <Quiet>
        Named sets of permissions. The five Pando ships can&rsquo;t be edited or deleted — an install that quietly
        redefined what &ldquo;viewer&rdquo; means is an install where nobody can answer what a viewer can do.
      </Quiet>

      {roles.isError && <Banner tone="failed">{messageOf(roles.error)}</Banner>}

      <div style={{ marginTop: 'var(--space-4)' }}>
        <Table
          loading={roles.isPending}
          skeletonRows={5}
          columns={[
            { key: 'name', header: 'Name', width: 'minmax(0,24ch)' },
            {
              key: 'scope',
              header: 'Applies to',
              width: '18ch',
              // R-080: the two scopes are never conflated. An install role and
              // an app role are different things and this column is where a
              // reader learns that.
              render: (row: Role) => (row.scope === 'install' ? 'The whole installation' : 'One app'),
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
                  <Button variant="secondary" onClick={() => setDeleting(row)}>
                    Delete
                  </Button>
                ) : null,
            },
          ]}
          rows={rows}
          empty={query.trim() ? <NoMatches what="roles" query={query} /> : undefined}
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

  // Every role, the shipped ones included, so a name already taken is said
  // while it is typed. The server refuses it too, ignoring case and spaces:
  // the built-ins are stored lowercase and shown capitalized, and
  // "Administrator" beside the real one would be indistinguishable in a
  // picker.
  const existing = useQuery({
    queryKey: ['roles', 'all'],
    queryFn: () => api.get<{ roles: Role[] }>('/roles?scope=all'),
  });
  const clash = (existing.data?.roles ?? []).find((r) => r.name.trim().toLowerCase() === name.trim().toLowerCase());
  const taken =
    name.trim() && clash
      ? clash.builtin
        ? `Pando already has a built-in role called ${sentence(clash.name)}. Choose a different name.`
        : `There is already a role called ${clash.name}. Choose a different name.`
      : undefined;

  const save = useMutation({
    mutationFn: () => api.post('/roles', { name: name.trim(), scope, verbs }),
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
    setVerbs((current) => (current.includes(verb) ? current.filter((v) => v !== verb) : [...current, verb]));

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
            disabled={save.isPending || !name.trim() || Boolean(taken) || verbs.length === 0}
            onClick={() => save.mutate()}
          >
            {save.isPending ? 'Saving' : 'Add role'}
          </Button>
        </>
      }
    >
      <div
        style={{
          display: 'flex',
          flexDirection: 'column',
          gap: 'var(--space-5)',
        }}
      >
        <Input
          label="Name"
          value={name}
          helper="What this set of permissions is called, for example support or release manager."
          error={taken}
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

        <div
          style={{
            display: 'flex',
            flexDirection: 'column',
            gap: 'var(--space-3)',
          }}
        >
          {catalog.isPending && (
            <Loading>
              {[0, 1, 2, 3].map((n) => (
                <LineSkeleton key={n} width={`${22 - (n % 2) * 6}ch`} />
              ))}
            </Loading>
          )}
          {available.map((v) => (
            <Checkbox key={v.verb} checked={verbs.includes(v.verb)} label={v.verb} onChange={() => toggle(v.verb)} />
          ))}
        </div>

        {save.isError && <Banner tone="failed">{messageOf(save.error)}</Banner>}
      </div>
    </Dialog>
  );
}

// --- deleting ----------------------------------------------------------------
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
      <div
        style={{
          display: 'flex',
          flexDirection: 'column',
          gap: 'var(--space-3)',
        }}
      >
        <p style={{ font: 'var(--type-body-ui)', margin: 0 }}>
          {people === 1 ? 'The one person' : `The ${people} people`} in {group.name} lose every app that was shared with
          the group — both opening it and any role the group had for managing it. Their accounts are kept, and so is
          anything shared with them directly or through another group.
        </p>
        <p
          style={{
            font: 'var(--type-body-ui)',
            color: 'var(--ink-secondary)',
            margin: 0,
          }}
        >
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
      <div
        style={{
          display: 'flex',
          flexDirection: 'column',
          gap: 'var(--space-3)',
        }}
      >
        <p style={{ font: 'var(--type-body-ui)', margin: 0 }}>
          {role.scope === 'install'
            ? `Everyone given ${role.name} across the installation loses the permissions it gave them.`
            : `Everyone given ${role.name} on an app — directly or through a group — loses the access it gave them on that app.`}{' '}
          They keep anything they hold another way.
        </p>
        <p
          style={{
            font: 'var(--type-body-ui)',
            color: 'var(--ink-secondary)',
            margin: 0,
          }}
        >
          This can&rsquo;t be undone. Making a role with the same name later does not give it back to anyone.
        </p>
        {remove.isError && <Banner tone="failed">{refusal(remove.error)}</Banner>}
      </div>
    </Dialog>
  );
}
