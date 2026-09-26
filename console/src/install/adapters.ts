// Adding and changing an adapter from the console — the form's logic, kept out
// of the component so it can be tested without rendering one.
//
// The form is built from GET /adapters/kinds, which each adapter package fills
// in about itself (R-254: capabilities, and settings, are data). Nothing here
// knows what an Anthropic or a Traefik adapter takes; a kind added to Pando is
// configurable here without this file changing (R-261).

/** One setting a kind of adapter takes. */
export interface KindField {
  key: string;
  label: string;
  help?: string;
  type: 'string' | 'int' | 'bool';
  required?: boolean;
  /** A secret such as an API key: sent in `credentials`, never in `config`
   *  (R-190), and never shown again. */
  credential?: boolean;
  /** What the adapter uses when the setting is left empty. Shown in the
   *  empty field, so what it says is what happens. */
  default?: string;
  /** An example, for a setting with no default. */
  placeholder?: string;
}

/** The text an empty field shows: the default when there is one, since
 *  that is what leaving it empty gets, and an example otherwise. */
export function fieldPlaceholder(f: KindField): string | undefined {
  return f.default || f.placeholder;
}

/** A kind of adapter this build of Pando can run. */
export interface AdapterKind {
  category: string;
  kind: string;
  name: string;
  description: string;
  id_prefix: string;
  fields: KindField[] | null;
}

/** What the form holds: text for every field, whatever its type, and a
 *  boolean for a bool — so a half-typed number is kept as typed. */
export interface AdapterForm {
  id: string;
  name: string;
  isDefault: boolean;
  values: Record<string, string | boolean>;
}

export interface AdapterRequest {
  id: string;
  category: string;
  kind: string;
  name: string;
  config: Record<string, string | number | boolean>;
  credentials?: Record<string, string>;
  is_default: boolean;
  enabled?: boolean;
}

/** The key a kind is chosen by in the form's select. */
export function kindKey(k: { category: string; kind: string }): string {
  return `${k.category}/${k.kind}`;
}

/** Category names as a person reads them. `ai` is an initialism; the rest are
 *  words, in sentence case like everything else. */
export function categoryLabel(category: string): string {
  if (category === 'ai') return 'AI';
  return category.charAt(0).toUpperCase() + category.slice(1);
}

// The categories in the order an installation is built up: where apps run and
// how they are reached first, then what they are built with and what they
// lean on, then the extras. One not listed here sorts after these, by name.
const CATEGORY_ORDER = ['runtime', 'routing', 'builder', 'services', 'secrets', 'backup', 'scanner', 'ai', 'notify', 'identity'];

const CATEGORY_NOTES: Record<string, string> = {
  runtime: 'Where apps run: it starts, stops and watches the containers each app is made of.',
  routing: 'How apps are reached: their addresses, and the edge that sends requests through Pando to them.',
  builder: 'What turns an app\u2019s source into an image Pando can run.',
  services: 'The databases and caches apps declare, such as PostgreSQL or Redis, provisioned beside each app.',
  secrets: 'Where secret values are kept, encrypted. Pando stores what this hands back, never the plain value.',
  backup: 'Where backups of Pando and of each app\u2019s storage are written.',
  scanner: 'What checks apps for known vulnerabilities, for the security score.',
  ai: 'An AI provider, such as Anthropic. It performs the AI functions chosen on it: repairing plans, drafting access and policy, searching the audit log, answering from the reference.',
  notify: 'Where notifications go, such as the console itself.',
  identity: 'Where accounts come from and how people sign in.',
};

/** One line on what a category of adapter is for. */
export function categoryNote(category: string): string {
  return CATEGORY_NOTES[category] ?? '';
}

/** Categories in the order the screen shows them. */
export function orderCategories(categories: Iterable<string>): string[] {
  const rank = (c: string) => {
    const i = CATEGORY_ORDER.indexOf(c);
    return i >= 0 ? i : CATEGORY_ORDER.length;
  };
  return [...new Set(categories)].sort((a, b) => rank(a) - rank(b) || a.localeCompare(b));
}

/** The kinds in the order the select offers them: by category, then by name. */
export function sortKinds(kinds: AdapterKind[]): AdapterKind[] {
  return [...kinds].sort(
    (a, b) => categoryLabel(a.category).localeCompare(categoryLabel(b.category)) || a.name.localeCompare(b.name),
  );
}

/** A new adapter's form, prefilled from its kind. */
export function blankForm(kind: AdapterKind, firstOfCategory: boolean): AdapterForm {
  return {
    id: `${kind.id_prefix}${kind.kind}`,
    name: kind.name,
    // The first adapter of a category is the one anything that needs that
    // category will use, so it is the default unless someone says otherwise.
    isDefault: firstOfCategory,
    values: {},
  };
}

/**
 * What is wrong with the form, by field key, in words that say how to fix it.
 * Empty when it can be sent.
 *
 * `credentialsSet` names the credentials already stored for the adapter being
 * changed: a required one of those may be left empty, which keeps it.
 */
export function formProblems(
  kind: AdapterKind,
  form: AdapterForm,
  credentialsSet: string[] = [],
): Record<string, string> {
  const problems: Record<string, string> = {};
  if (!form.id.trim()) problems.id = `An adapter needs an ID, for example ${kind.id_prefix}${kind.kind}.`;
  for (const f of kind.fields ?? []) {
    const v = form.values[f.key];
    if (f.type === 'bool') continue;
    const text = typeof v === 'string' ? v.trim() : '';
    if (text === '') {
      const kept = f.credential && credentialsSet.includes(f.key);
      if (f.required && !kept) problems[f.key] = `${f.label} is required.`;
      continue;
    }
    if (f.type === 'int' && !/^-?\d+$/.test(text)) {
      problems[f.key] = `${f.label} takes a whole number, such as ${[f.default, f.placeholder].find((x) => x && /^\d+$/.test(x)) ?? '30'}.`;
    }
  }
  return problems;
}

/**
 * The body of POST /adapters for a form.
 *
 * Credentials go in `credentials` and nowhere else — the server refuses them in
 * `config`, which is stored in the clear (R-190). An empty field is left out:
 * an empty credential keeps the stored one (a "" would remove it), and an empty
 * setting is the adapter's own default. Ints go as JSON numbers, because the
 * adapter reads its config as typed JSON. A bool is sent only when on, since
 * off is what an absent one means.
 *
 * Call formProblems first; a field it would refuse is not sent correctly here.
 */
export function adapterRequest(kind: AdapterKind, form: AdapterForm, enabled?: boolean): AdapterRequest {
  const config: AdapterRequest['config'] = {};
  const credentials: Record<string, string> = {};
  for (const f of kind.fields ?? []) {
    const v = form.values[f.key];
    if (f.type === 'bool') {
      if (v === true) config[f.key] = true;
      continue;
    }
    const text = typeof v === 'string' ? v.trim() : '';
    if (text === '') continue;
    if (f.credential) credentials[f.key] = text;
    else if (f.type === 'int') config[f.key] = parseInt(text, 10);
    else config[f.key] = text;
  }

  const body: AdapterRequest = {
    id: form.id.trim(),
    category: kind.category,
    kind: kind.kind,
    name: form.name.trim() || kind.name,
    config,
    is_default: form.isDefault,
  };
  if (Object.keys(credentials).length > 0) body.credentials = credentials;
  // Sent when changing an existing adapter, so saving one that was turned off
  // does not quietly turn it back on: the server's default is enabled.
  if (enabled !== undefined) body.enabled = enabled;
  return body;
}
