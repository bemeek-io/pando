// What the audit log can be narrowed by, shared by the Audit log screen and an
// account's page, which shows the same log for one account and links to the
// screen with its filters carried over.
//
// Each field maps to a GET /audit parameter, and they combine — "Dana's changes
// to roles in the last day" is one query.

export interface AuditFilters {
  action: string;
  /** A principal ID, or `kind:<kind>` for a whole kind of actor. */
  actor: string;
  targetKind: string;
  targetID: string;
  /** An ID that is the actor or the target: everything to do with one account. */
  involving: string;
  /** A WHEN preset, or `custom` for since/until. */
  when: string;
  /** datetime-local values, in the viewer's own clock. */
  since: string;
  until: string;
}

export const NO_FILTERS: AuditFilters = {
  action: '',
  actor: '',
  targetKind: '',
  targetID: '',
  involving: '',
  when: '',
  since: '',
  until: '',
};

export const WHEN: { value: string; label: string; hours?: number }[] = [
  { value: '', label: 'All time' },
  { value: '1h', label: 'Last hour', hours: 1 },
  { value: '24h', label: 'Last 24 hours', hours: 24 },
  { value: '7d', label: 'Last 7 days', hours: 24 * 7 },
  { value: '30d', label: 'Last 30 days', hours: 24 * 30 },
  { value: 'custom', label: 'Custom range' },
];

/** The GET /audit query string for a set of filters, times resolved now. */
export function auditQuery(f: AuditFilters, before?: string, now = Date.now()): string {
  const q = new URLSearchParams();
  if (f.action) q.set('action', f.action);
  // A whole kind of actor — the system, anonymous — is a kind, not an ID.
  if (f.actor.startsWith('kind:')) q.set('principal_kind', f.actor.slice('kind:'.length));
  else if (f.actor) q.set('principal_id', f.actor);
  if (f.targetKind) q.set('target_kind', f.targetKind);
  if (f.targetID) q.set('target_id', f.targetID.trim());
  if (f.involving) q.set('involving', f.involving);
  const preset = WHEN.find((w) => w.value === f.when);
  if (preset?.hours) q.set('since', new Date(now - preset.hours * 3_600_000).toISOString());
  if (f.when === 'custom') {
    // datetime-local is the viewer's own clock; the wire is UTC (RFC 3339).
    if (f.since) q.set('since', new Date(f.since).toISOString());
    if (f.until) q.set('until', new Date(f.until).toISOString());
  }
  if (before) q.set('before', before);
  const s = q.toString();
  return s ? `?${s}` : '';
}

// The console's own address for a set of filters: /admin/audit?… . Not the API
// query — a preset stays a preset ("last 7 days" from whenever the link is
// opened), and the names are the screen's fields rather than the wire's.
const LINK_KEYS: (keyof AuditFilters)[] = [
  'action',
  'actor',
  'targetKind',
  'targetID',
  'involving',
  'when',
  'since',
  'until',
];

/** Filters as the query string of a link to the Audit log screen, without `?`. */
export function linkQuery(f: AuditFilters): string {
  const q = new URLSearchParams();
  for (const k of LINK_KEYS) if (f[k]) q.set(k, f[k]);
  return q.toString();
}

/** The inverse of linkQuery. Unknown keys and presets are ignored. */
export function filtersFrom(query: string | undefined): AuditFilters {
  const q = new URLSearchParams(query ?? '');
  const f: AuditFilters = { ...NO_FILTERS };
  for (const k of LINK_KEYS) f[k] = q.get(k) ?? '';
  if (!WHEN.some((w) => w.value === f.when)) f.when = '';
  if (f.when !== 'custom') {
    f.since = '';
    f.until = '';
  }
  return f;
}
