// The last scheduled backup of an app, as a symbol and a word (issue #87).
//
// Status is a symbol plus a word, never color alone. A backup that failed is
// not a failed app, and marker red is reserved for failed apps and destructive
// actions — so a failed backup takes the stopped dash and says "Failed", and
// the message beside it says why.

import type { StatusIndicatorProps } from '@design';
import type { BackupAttempt } from '@api/types.gen';

type Symbol = NonNullable<StatusIndicatorProps['status']>;

export function attemptSymbol(attempt: BackupAttempt): Symbol {
  if (attempt.outcome === 'taken') return 'running';
  if (attempt.outcome === 'failed') return 'stopped';
  return 'info';
}

export function attemptLabel(attempt: BackupAttempt): string {
  if (attempt.outcome === 'taken') return 'Taken';
  if (attempt.outcome === 'failed') return 'Failed';
  return 'Skipped';
}

/** What went wrong and what to do about it, as the server said it (R-105). */
export function attemptDetail(attempt: BackupAttempt): string {
  return [attempt.message, attempt.remedy].filter(Boolean).join(' ');
}
