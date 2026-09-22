// Matching for the console's search fields.
//
// Every search in the console filters a list the page already has. The lists
// are one installation's apps, accounts, groups and roles (R-015 — one install,
// one organization), so they are small, and asking the server again on every
// keystroke would be a round trip to learn nothing new. Nothing here is a
// capability the API lacks (R-261): the API returns the whole list; this only
// chooses which of it to show.

/**
 * Whether a query matches any of the given fields.
 *
 * Case-insensitive, and every word of the query has to appear somewhere — in
 * any field, in any order — so "grafana running" finds the running Grafana
 * without the words having to sit next to each other. An empty query matches
 * everything.
 */
export function matches(query: string, ...fields: Array<string | null | undefined>): boolean {
  const words = query.toLowerCase().split(/\s+/).filter(Boolean);
  if (words.length === 0) return true;
  const haystack = fields.filter(Boolean).join(' ').toLowerCase();
  return words.every((w) => haystack.includes(w));
}
