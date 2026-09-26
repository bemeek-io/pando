// Adding or changing an adapter — the console's form for POST /adapters.
//
// The API could configure an adapter and the console could not, which is the
// gap R-261 exists to close: every surface is a client of the same API, and
// none may lack a capability it has. The form is generated from
// GET /adapters/kinds, so a kind added to Pando appears here without this file
// learning about it.

import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Banner, Button, Checkbox, Dialog, Input, Select } from '@design';

import { api } from '@api/client';
import { Quiet, refusal } from './Accounts';
import { FieldSkeleton, Loading } from '../ui/Loading';
import { adapterRequest, blankForm, categoryLabel, categoryNote, fieldPlaceholder, formProblems, kindKey, orderCategories, sortKinds } from './adapters';
import type { AdapterForm, AdapterKind, KindField } from './adapters';
import { AdapterFunctions, choiceChanges, currentChoice } from './AdapterFunctions';
import type { FunctionChoice } from './AdapterFunctions';
import type { AIFunction } from './AIFunctions';

/** A configured adapter, as GET /adapters returns it. */
export interface ConfiguredAdapter {
  id: string;
  category: string;
  kind: string;
  name?: string;
  is_default?: boolean;
  enabled?: boolean;
  healthy?: boolean;
  /** Which credentials are stored — names only; no value is ever returned. */
  credentials_set?: string[];
  /** The stored settings (never credentials), returned to whoever may change
   *  adapters, so a change starts from what is there. */
  config?: Record<string, unknown>;
}

/** An adapter's stored settings as the form's values: text for strings and
 *  numbers, as the inputs hold them, and booleans as they are. */
function storedValues(config: Record<string, unknown> | undefined): Record<string, string | boolean> {
  const out: Record<string, string | boolean> = {};
  for (const [key, value] of Object.entries(config ?? {})) {
    if (typeof value === 'boolean') out[key] = value;
    else if (typeof value === 'string' || typeof value === 'number') out[key] = String(value);
  }
  return out;
}

export function AdapterDialog({
  existing,
  category: startCategory,
  adapters,
  onClose,
  onSaved,
}: {
  /** The adapter being changed, or none to add one. */
  existing?: ConfiguredAdapter;
  /** The category to start in, when adding from a category's section. */
  category?: string;
  /** Every configured adapter, to know whether a category has one yet. */
  adapters: ConfiguredAdapter[];
  onClose: () => void;
  /** Told the saved adapter's name, for the restart notice. */
  onSaved: (name: string) => void;
}) {
  const kinds = useQuery({
    queryKey: ['adapter-kinds'],
    queryFn: () => api.get<{ kinds: AdapterKind[] | null }>('/adapters/kinds'),
  });
  const catalog = sortKinds(kinds.data?.kinds ?? []);

  const [category, setCategory] = useState(existing?.category ?? startCategory ?? '');
  const [picked, setPicked] = useState(existing ? kindKey(existing) : '');
  const categories = orderCategories(catalog.map((k) => k.category));
  const inCategory = catalog.filter((k) => k.category === category);
  // A category with one kind has nothing to choose between.
  const only = inCategory.length === 1 ? inCategory[0] : undefined;
  const chosen = picked || (only ? kindKey(only) : '');
  const [draft, setDraft] = useState<AdapterForm | null>(null);
  const [tried, setTried] = useState(false);

  const kind = catalog.find((k) => kindKey(k) === chosen);
  const stored = existing?.credentials_set ?? [];

  // The form starts from the kind until someone types into it.
  const initial: AdapterForm | null = !kind
    ? null
    : existing
      ? {
          id: existing.id,
          name: existing.name || kind.name,
          isDefault: existing.is_default ?? false,
          values: storedValues(existing.config),
        }
      : blankForm(kind, !adapters.some((a) => a.category === kind.category));
  const form = draft ?? initial;
  const edit = (patch: Partial<AdapterForm>) => form && setDraft({ ...form, ...patch });
  const setValue = (key: string, value: string | boolean) =>
    form && setDraft({ ...form, values: { ...form.values, [key]: value } });

  const problems = kind && form ? formProblems(kind, form, stored) : {};
  const shown = (key: string) => (tried ? problems[key] : undefined);

  // An AI adapter's functions are chosen here, on the adapter (R-259). Only a
  // running one can be given functions: the server checks what it advertises.
  const queries = useQueryClient();
  const isAI = Boolean(existing) && (existing?.category ?? category) === 'ai';
  const functions = useQuery({
    queryKey: ['ai-functions'],
    queryFn: () => api.get<{ functions: AIFunction[] }>('/ai/functions'),
    enabled: isAI,
  });
  const had = currentChoice(functions.data?.functions ?? [], existing?.id ?? '');
  const [choice, setChoice] = useState<FunctionChoice | null>(null);
  const chosenFunctions = choice ?? had;

  const save = useMutation({
    mutationFn: async () => {
      // The adapter's own settings are saved only when they changed: that is
      // what needs a restart, and choosing functions does not.
      const settingsChanged = draft !== null || !existing;
      if (settingsChanged) {
        await api.post<{ id: string; note?: string }>('/adapters', adapterRequest(kind!, form!, existing?.enabled));
      }
      if (existing && choice) {
        for (const c of choiceChanges(existing.id, had, choice)) {
          if (c.method === 'DELETE') await api.del(`/ai/functions/${c.fn}`);
          else await api.put(`/ai/functions/${c.fn}`, { adapter_id: c.adapterID, model: c.model ?? '' });
        }
      }
      return settingsChanged;
    },
    onSuccess: (settingsChanged) => {
      if (settingsChanged) onSaved(form!.name.trim() || kind!.name);
      else onClose();
    },
    onSettled: () => void queries.invalidateQueries({ queryKey: ['ai-functions'] }),
  });

  const submit = () => {
    setTried(true);
    if (!kind || !form) return;
    // Settings left as they were are not re-checked: only functions changed.
    if ((draft !== null || !existing) && Object.keys(problems).length > 0) return;
    save.mutate();
  };

  // Saving replaces the stored settings whole. GET /adapters returns them to
  // whoever may change adapters, and the form starts from them; if they did
  // not come back, a change starts from empty fields, and that is said before
  // anything is typed rather than discovered after a restart.
  const settings = (kind?.fields ?? []).filter((f) => !f.credential);
  const missingKind = existing && kinds.isSuccess && !kind;

  return (
    <Dialog
      open
      title={existing ? `Edit ${existing.name || existing.id}` : 'Add adapter'}
      description="Pando reads adapters when it starts, so a saved change takes effect after a restart."
      onClose={onClose}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button variant="primary" disabled={!kind || !form || save.isPending} onClick={submit}>
            {existing ? (save.isPending ? 'Saving' : 'Save adapter') : save.isPending ? 'Adding' : 'Add adapter'}
          </Button>
        </>
      }
    >
      <div style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-4)' }}>
        {kinds.isPending ? (
          <Loading gap="var(--space-4)">
            <FieldSkeleton />
            <FieldSkeleton />
          </Loading>
        ) : kinds.isError ? (
          <Banner tone="failed">{refusal(kinds.error)}</Banner>
        ) : missingKind ? (
          <Quiet>
            This build of Pando has no {existing.category} adapter of kind {existing.kind}, so it can&rsquo;t be
            changed here.
          </Quiet>
        ) : (
          <>
            {/* Category first, then the adapter within it: the question someone
                arrives with is "I need a builder", not a list of every kind. Both
                fixed when changing: a different kind under the same ID would be
                a different adapter, and is added as one. */}
            <div>
              <Select
                label="Category"
                disabled={Boolean(existing)}
                value={category}
                options={[
                  ...(category ? [] : [{ value: '', label: 'Choose a category' }]),
                  ...categories.map((c) => ({ value: c, label: categoryLabel(c) })),
                ]}
                onChange={(e) => {
                  setCategory(e.target.value);
                  setPicked('');
                  setDraft(null);
                  setTried(false);
                }}
              />
              {category && categoryNote(category) && (
                <p style={{ font: 'var(--type-caption)', color: 'var(--ink-secondary)', margin: 'var(--space-1) 0 0' }}>
                  {categoryNote(category)}
                </p>
              )}
            </div>
            {category && (
              <div>
                <Select
                  label="Adapter"
                  disabled={Boolean(existing)}
                  value={chosen}
                  options={[
                    ...(chosen ? [] : [{ value: '', label: 'Choose an adapter' }]),
                    ...inCategory.map((k) => ({ value: kindKey(k), label: k.name })),
                  ]}
                  onChange={(e) => {
                    setPicked(e.target.value);
                    setDraft(null);
                    setTried(false);
                  }}
                />
                {kind && (
                  <p style={{ font: 'var(--type-caption)', color: 'var(--ink-secondary)', margin: 'var(--space-1) 0 0' }}>
                    {kind.description}
                  </p>
                )}
              </div>
            )}

            {kind && form && (
              <>
                {existing && !existing.config && settings.length > 0 && (
                  <Banner tone="info">
                    Pando doesn&rsquo;t show an adapter&rsquo;s current settings. Enter each one you want to keep:
                    saving clears a setting left empty. A stored credential is kept unless you enter a new one.
                  </Banner>
                )}

                <Input
                  label="ID"
                  mono
                  // Fixed when changing: saving under another ID adds a second
                  // adapter rather than renaming this one.
                  disabled={Boolean(existing)}
                  value={form.id}
                  helper="How specs and the CLI refer to this adapter. It can't be changed later."
                  error={shown('id')}
                  onChange={(e) => edit({ id: e.target.value })}
                />
                <Input label="Name" value={form.name} onChange={(e) => edit({ name: e.target.value })} />

                {(kind.fields ?? []).map((f) => (
                  <FieldInput
                    key={f.key}
                    field={f}
                    value={form.values[f.key]}
                    stored={existing ? stored.includes(f.key) : undefined}
                    error={shown(f.key)}
                    onChange={(v) => setValue(f.key, v)}
                  />
                ))}

                {/* An AI adapter has no default: it handles the functions
                    chosen on it (R-259). A new one is not running until Pando
                    restarts, so its functions are chosen after that. */}
                {kind.category === 'ai' ? (
                  existing ? (
                    functions.isSuccess && (
                      <AdapterFunctions
                        adapterID={existing.id}
                        functions={functions.data.functions}
                        choice={chosenFunctions}
                        onChange={setChoice}
                      />
                    )
                  ) : (
                    <p style={{ font: 'var(--type-caption)', color: 'var(--ink-secondary)', margin: 0 }}>
                      After Pando restarts, open this adapter again to choose what it handles.
                    </p>
                  )
                ) : (
                  <Checkbox
                    label={`Use as the default ${kind.category} adapter`}
                    description="Used by anything that needs this kind of adapter and doesn't name one."
                    checked={form.isDefault}
                    onChange={(e) => edit({ isDefault: e.target.checked })}
                  />
                )}
              </>
            )}

            {save.isError && <Banner tone="failed">{refusal(save.error)}</Banner>}
          </>
        )}
      </div>
    </Dialog>
  );
}

/** A caption for a credential field. */
const CREDENTIAL = 'Stored encrypted. Pando never shows it again.';

/** One setting, as its kind describes it. */
function FieldInput({
  field,
  value,
  stored,
  error,
  onChange,
}: {
  field: KindField;
  value: string | boolean | undefined;
  /** Changing an adapter: whether this credential is already stored. */
  stored?: boolean;
  error?: string;
  onChange: (v: string | boolean) => void;
}) {
  const label = field.required ? `${field.label} (required)` : field.label;

  if (field.type === 'bool') {
    return (
      <Checkbox label={label} description={field.help} checked={value === true} onChange={(e) => onChange(e.target.checked)} />
    );
  }

  // A credential's helper says what happens to what is typed. The kind's own
  // help comes first when it has one, since it may say more — such as where
  // the key is read from when none is stored.
  let helper = field.help;
  if (field.credential) {
    if (stored === true) helper = 'One is stored. Leave empty to keep the current one.';
    else if (stored === false) helper = `None is stored. ${field.help ?? CREDENTIAL}`;
    else helper = field.help ?? CREDENTIAL;
  }

  return (
    <Input
      label={label}
      type={field.credential ? 'password' : field.type === 'int' ? 'number' : 'text'}
      autoComplete={field.credential ? 'new-password' : 'off'}
      value={typeof value === 'string' ? value : ''}
      placeholder={fieldPlaceholder(field)}
      helper={helper}
      error={error}
      onChange={(e) => onChange(e.target.value)}
    />
  );
}
