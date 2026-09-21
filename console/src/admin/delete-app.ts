// The backup decision, as a URL (R-204, R-205).
//
// `DELETE /apps/{id}` refuses to guess: an app with storage and no decision
// comes back 409 STATE_BACKUP_DECISION_REQUIRED rather than picking one. The
// CLI answers that with a flag (R-205) and the console answers it with a
// question, which is what R-204 asks for — but both answers are this same
// query string, and it is small enough to be worth testing on its own.

/** What to do with the app's storage. `none` is for an app that keeps none. */
export type StorageDecision = 'backup' | 'discard' | 'none';

export function deletePath(appID: string, decision: StorageDecision): string {
  switch (decision) {
    case 'backup':
      return `/apps/${appID}?backup=true`;
    case 'discard':
      return `/apps/${appID}?force=true`;
    // An app with no volumes has nothing to decide, and saying `force=true`
    // anyway would write "forced" into the audit record of a deletion where
    // nothing was discarded.
    case 'none':
      return `/apps/${appID}`;
  }
}
