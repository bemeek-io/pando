// Which AI adapter handles each AI function, and on which model (R-259).
//
// One adapter can handle any number of functions and each function has at most
// one adapter, so this is a list of functions, not of adapters: the question an
// administrator is answering is "who does audit search", and the table is
// shaped like the answer. A function with no adapter is off, which is an
// ordinary state (R-106) and is shown as one, not as a fault.

import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Button, Dialog, Input, Select, StatusIndicator } from '@design';

import { api } from '@api/client';
import { Table } from '../ui/Table';
import { Quiet, messageOf, refusal } from './Accounts';
import type { ConfiguredAdapter } from './AdapterDialog';

export interface AIFunction {
  function: string;
  title: string;
  adapter_id?: string;
  model?: string;
  effective_model?: string;
  on: boolean;
  off?: string;
  source?: { kind: string; name?: string; key?: string };
  overridden?: { adapter_id: string; model?: string };
}

/** Where the config file assigns a function, in words (R-271). */
export function declaredIn(f: AIFunction): string | null {
  if (f.source?.kind !== 'file') return null;
  return `${f.source.name ?? 'the config file'}, at ${f.source.key ?? ''}`;
}

/**
 * Whether an AI function is on, for a screen deciding whether to offer it.
 * AI appears only where it can do something (design 08): a question field for
 * a function nobody assigned would only ever answer "not assigned". False
 * while loading, and when the caller cannot read the list.
 */
export function useAIFunctionOn(fn: string): boolean {
  return useAIFunctionState(fn) === 'on';
}

/** On, off, or unknown — the last when the caller cannot read the list, which
 *  needs install.view. A screen anyone may use offers the function unless it
 *  is known to be off, and the server's answer says the rest. */
export function useAIFunctionState(fn: string): 'on' | 'off' | 'unknown' {
  const functions = useQuery({
    queryKey: ['ai-functions'],
    queryFn: () => api.get<{ functions: AIFunction[] }>('/ai/functions'),
    retry: false,
  });
  const f = functions.data?.functions.find((x) => x.function === fn);
  if (!f) return functions.isPending ? 'off' : 'unknown';
  return f.on ? 'on' : 'off';
}

export function AIFunctions({ canManage, adapters }: { canManage: boolean; adapters: ConfiguredAdapter[] }) {
  const [editing, setEditing] = useState<AIFunction | null>(null);
  const functions = useQuery({
    queryKey: ['ai-functions'],
    queryFn: () => api.get<{ functions: AIFunction[] }>('/ai/functions'),
  });

  return (
    <section>
      <h4 style={{ font: 'var(--type-h4)', margin: 'var(--space-6) 0 var(--space-2)' }}>AI functions</h4>
      <Quiet>
        Each AI function is handled by one AI adapter, on its own model if you choose one. A function with no
        adapter is off, and Pando works without it.
      </Quiet>
      {functions.isError && <Quiet>{messageOf(functions.error)}</Quiet>}
      <div style={{ marginTop: 'var(--space-3)' }}>
        <Table
          loading={functions.isPending}
          rows={functions.data?.functions ?? []}
          columns={[
            { key: 'title', header: 'Function', width: 'minmax(0,1fr)' },
            {
              key: 'adapter_id',
              header: 'Adapter',
              width: 'minmax(0,18ch)',
              mono: true,
              render: (f: AIFunction) => f.adapter_id ?? '',
            },
            {
              key: 'effective_model',
              header: 'Model',
              width: 'minmax(0,22ch)',
              mono: true,
              muted: true,
              render: (f: AIFunction) => f.effective_model ?? '',
            },
            {
              key: 'on',
              header: 'State',
              width: '10ch',
              // Symbol and word, and the reason on hover: "off" alone does not
              // say whether anybody chose it.
              render: (f: AIFunction) => (
                <span title={f.off}>
                  <StatusIndicator status={f.on ? 'running' : 'stopped'} label={f.on ? 'On' : 'Off'} />
                </span>
              ),
            },
            {
              key: 'actions',
              header: '',
              width: '18ch',
              align: 'right' as const,
              render: (f: AIFunction) => {
                const where = declaredIn(f);
                if (where) {
                  return (
                    <span title={where} style={{ font: 'var(--type-caption)', color: 'var(--ink-secondary)' }}>
                      Set in config file
                    </span>
                  );
                }
                return (
                  canManage && (
                    <Button variant="secondary" onClick={() => setEditing(f)}>
                      Change
                    </Button>
                  )
                );
              },
            },
          ]}
        />
      </div>
      {editing && <AssignDialog fn={editing} adapters={adapters} onClose={() => setEditing(null)} />}
    </section>
  );
}

function AssignDialog({
  fn,
  adapters,
  onClose,
}: {
  fn: AIFunction;
  adapters: ConfiguredAdapter[];
  onClose: () => void;
}) {
  const queries = useQueryClient();
  const [adapter, setAdapter] = useState(fn.adapter_id ?? '');
  const [model, setModel] = useState(fn.model ?? '');

  const save = useMutation({
    mutationFn: async () => {
      // Moving a function is removing it from one adapter and giving it to
      // another: the server refuses a second adapter for a function outright
      // (R-259), so the console does the two steps it says to.
      if (fn.adapter_id && fn.adapter_id !== adapter) await api.del(`/ai/functions/${fn.function}`);
      if (adapter) await api.put(`/ai/functions/${fn.function}`, { adapter_id: adapter, model });
      else if (fn.adapter_id) await api.del(`/ai/functions/${fn.function}`);
    },
    onSuccess: () => {
      void queries.invalidateQueries({ queryKey: ['ai-functions'] });
      onClose();
    },
    onSettled: () => void queries.invalidateQueries({ queryKey: ['ai-functions'] }),
  });

  return (
    <Dialog
      open
      title={fn.title}
      description="Choose the AI adapter that handles this function. Takes effect at once."
      onClose={onClose}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button variant="primary" disabled={save.isPending} onClick={() => save.mutate()}>
            Save
          </Button>
        </>
      }
    >
      <div style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-4)' }}>
        <Select
          label="Adapter"
          value={adapter}
          options={[
            { value: '', label: 'None — this function is off' },
            ...adapters.map((a) => ({ value: a.id, label: a.name ? `${a.name} (${a.id})` : a.id })),
          ]}
          onChange={(e) => setAdapter(e.target.value)}
        />
        <Input
          label="Model"
          mono
          value={model}
          disabled={!adapter}
          placeholder="The adapter's own"
          helper="A model for this function only, so lighter work can run on a cheaper one. Leave empty for the adapter's own."
          onChange={(e) => setModel(e.target.value)}
        />
        {fn.overridden && (
          <Quiet>
            The config file replaces what was set here before ({fn.overridden.adapter_id}). Remove it there to use
            that again.
          </Quiet>
        )}
        {save.isError && <Quiet>{refusal(save.error)}</Quiet>}
      </div>
    </Dialog>
  );
}
