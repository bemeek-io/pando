// Environment variables the app needs and detection did not find.
//
// R-102 is "ask, never guess", and R-103 promises detection is reviewable
// rather than authoritative. Both imply the answer can be corrected — but the
// console offered no way to add a variable detection missed, so an app needing
// one setting Pando could not infer could not be configured at all without the
// API.
//
// Two kinds, and the difference is not cosmetic. A plain value goes into the
// spec, which is exportable and meant to be safe to hand to somebody (design 01
// §5). A secret goes through the secrets adapter and the spec carries only a
// reference (R-190, R-191) — so pasting an API key into the wrong box would put
// it in every export of this app forever. The form asks which, and defaults to
// the safe one.

import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Banner, Button, Dialog, Input, Select, Table } from '@design';

import { api } from '@api/client';
import type { AppSpec, EnvEntry, Workload } from '@api/types.gen';
import { Quiet, messageOf } from '../install/Accounts';

interface Revision {
  id: string;
  revision: number;
  body: AppSpec;
}

interface Row {
  key: string;
  workload: string;
  kind: 'value' | 'secret' | 'slot';
  shown: string;
}

export function Environment({ appID }: { appID: string }) {
  const queries = useQueryClient();
  const [adding, setAdding] = useState(false);

  // Two requests, because the list deliberately carries no bodies — fifty
  // revisions each with a full spec is a heavy response for a list nobody
  // reads that way. The list says which revision is newest; the second request
  // fetches that one's spec.
  const specs = useQuery({
    queryKey: ['apps', appID, 'specs'],
    queryFn: () => api.get<{ revisions: Revision[] | null; pinned_spec_id: string }>(`/apps/${appID}/specs`),
  });

  // The newest revision is what an edit builds on, not the pinned one: two
  // edits in a row should both survive, and building each on the pinned spec
  // would silently discard the first.
  const newest = (specs.data?.revisions ?? [])
    .slice()
    .sort((a, b) => b.revision - a.revision)[0];

  const full = useQuery({
    queryKey: ['apps', appID, 'spec', newest?.revision],
    queryFn: () => api.get<Revision>(`/apps/${appID}/specs/${newest?.revision}`),
    enabled: Boolean(newest),
  });

  const latest = full.data;

  const save = useMutation({
    mutationFn: async (entry: { key: string; value: string; secret: boolean; workload: string }) => {
      if (!latest) throw new Error('no spec');

      let env: EnvEntry;
      if (entry.secret) {
        // Stored through the secrets adapter; the spec gets a reference. This
        // is what keeps "an exported spec is safe to hand to someone" true
        // without a special case for this one field.
        await api.put(`/apps/${appID}/secrets/${encodeURIComponent(entry.key)}`, { value: entry.value });
        env = { key: entry.key, secret_ref: entry.key, source: 'user' };
      } else {
        env = { key: entry.key, value: entry.value, source: 'user' };
      }

      const body: AppSpec = {
        ...latest.body,
        workloads: (latest.body.workloads ?? []).map((w: Workload) =>
          w.name === entry.workload
            ? { ...w, env: [...(w.env ?? []).filter((e) => e.key !== entry.key), env] }
            : w,
        ),
      };
      return api.post(`/apps/${appID}/specs`, body);
    },
    onSuccess: () => {
      void queries.invalidateQueries({ queryKey: ['apps', appID] });
      void queries.invalidateQueries({ queryKey: ['apps', appID, 'specs'] });
      setAdding(false);
    },
  });

  const remove = useMutation({
    mutationFn: (row: Row) => {
      if (!latest) throw new Error('no spec');
      const body: AppSpec = {
        ...latest.body,
        workloads: (latest.body.workloads ?? []).map((w: Workload) =>
          w.name === row.workload ? { ...w, env: (w.env ?? []).filter((e) => e.key !== row.key) } : w,
        ),
      };
      return api.post(`/apps/${appID}/specs`, body);
    },
    onSuccess: () => {
      void queries.invalidateQueries({ queryKey: ['apps', appID] });
      void queries.invalidateQueries({ queryKey: ['apps', appID, 'specs'] });
    },
  });

  const rows = envRows(latest?.body);
  const workloads = (latest?.body?.workloads ?? []).map((w: Workload) => w.name);

  return (
    <section>
      <div style={{ display: 'flex', alignItems: 'baseline', justifyContent: 'space-between' }}>
        <h4 style={{ font: 'var(--type-h4)', margin: '0 0 var(--space-2)' }}>Environment</h4>
        {latest && (
          <Button variant="ghost" onClick={() => setAdding(true)}>
            Add variable
          </Button>
        )}
      </div>

      <Quiet>
        What the app reads from its environment. Pando works out what it can; add anything it
        missed.
      </Quiet>

      {specs.isError && <Banner tone="failed">{messageOf(specs.error)}</Banner>}
      {remove.isError && <Banner tone="failed">{messageOf(remove.error)}</Banner>}

      <div style={{ marginTop: 'var(--space-4)' }}>
        <Table
          columns={[
            { key: 'key', header: 'Name', width: 'minmax(0,26ch)', mono: true },
            { key: 'shown', header: 'Value', width: 'minmax(0,28ch)', mono: true, muted: true },
            { key: 'workload', header: 'Part of the app', width: '18ch', muted: true },
            {
              key: 'actions',
              header: '',
              width: '12ch',
              align: 'right',
              render: (row: Row) =>
                // A slot is filled on the Dependencies list, and removing it
                // here would take away the only thing pointing at a database.
                row.kind === 'slot' ? null : (
                  <Button variant="ghost" onClick={() => remove.mutate(row)}>
                    Remove
                  </Button>
                ),
            },
          ]}
          rows={rows}
          empty={<Quiet>This app reads nothing from its environment.</Quiet>}
        />
      </div>

      {adding && latest && (
        <AddVariable
          workloads={workloads}
          onClose={() => setAdding(false)}
          onSave={(entry) => save.mutate(entry)}
          saving={save.isPending}
          error={save.isError ? messageOf(save.error) : undefined}
        />
      )}
    </section>
  );
}

/** Every environment entry across the app's workloads, flattened for a table. */
function envRows(body?: AppSpec): Row[] {
  const out: Row[] = [];
  for (const w of body?.workloads ?? []) {
    for (const e of w.env ?? []) {
      out.push({
        key: e.key,
        workload: w.name,
        kind: e.secret_ref ? 'secret' : e.slot_ref ? 'slot' : 'value',
        // A stored secret is never shown, here or anywhere (R-194). A slot
        // says where its value comes from rather than what it is, because at
        // this point it does not have one yet.
        shown: e.secret_ref
          ? 'Set, and not shown'
          : e.slot_ref
            ? `From the ${e.slot_ref} dependency`
            : (e.value ?? ''),
      });
    }
  }
  return out;
}

function AddVariable({
  workloads,
  onClose,
  onSave,
  saving,
  error,
}: {
  workloads: string[];
  onClose: () => void;
  onSave: (entry: { key: string; value: string; secret: boolean; workload: string }) => void;
  saving: boolean;
  error?: string;
}) {
  const [key, setKey] = useState('');
  const [value, setValue] = useState('');
  const [secret, setSecret] = useState(false);
  const [workload, setWorkload] = useState(workloads[0] ?? '');

  return (
    <Dialog
      open
      onClose={onClose}
      title="Add an environment variable"
      description="This takes effect at the next deploy."
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button
            variant="primary"
            disabled={saving || key.trim() === '' || value === '' || workload === ''}
            onClick={() => onSave({ key: key.trim(), value, secret, workload })}
          >
            {saving ? 'Adding' : 'Add variable'}
          </Button>
        </>
      }
    >
      <div style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-5)' }}>
        <Input
          label="Name"
          mono
          autoFocus
          value={key}
          placeholder="STRIPE_API_KEY"
          helper="Exactly as the app reads it."
          onChange={(e) => setKey(e.target.value)}
        />

        <Select
          label="Kind"
          value={secret ? 'secret' : 'value'}
          options={[
            { value: 'value', label: 'An ordinary setting' },
            { value: 'secret', label: 'A secret — a key, token or password' },
          ]}
          onChange={(e) => setSecret(e.target.value === 'secret')}
        />

        <Input
          label="Value"
          mono
          type={secret ? 'password' : 'text'}
          value={value}
          helper={
            secret
              ? 'Stored separately and never shown again. It will not appear in this app’s exported configuration.'
              : 'Kept in the app’s configuration, which is exportable — don’t put a key or password here.'
          }
          onChange={(e) => setValue(e.target.value)}
        />

        {workloads.length > 1 && (
          <Select
            label="Part of the app"
            value={workload}
            options={workloads.map((w) => ({ value: w, label: w }))}
            onChange={(e) => setWorkload(e.target.value)}
          />
        )}

        {error && <Banner tone="failed">{error}</Banner>}
      </div>
    </Dialog>
  );
}
