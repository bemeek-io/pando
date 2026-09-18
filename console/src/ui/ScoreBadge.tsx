// The security score, as a badge (R-310, R-320).
//
// The number is always in it. R-320 rules out a grade or a color standing on
// its own — "F" tells a deployer nothing they can act on, and a red pill tells
// somebody who cannot see red nothing at all — so the badge is the score, out
// of 100, with color as the second signal rather than the only one. The design
// system's own rule is the same shape: status is a symbol plus a word, never a
// colored pill.
//
// The color says what the number means *here*. An installation with a threshold
// colors against the threshold; one without colors against bands, because a
// score of 20 is worth noticing even where nothing is enforced.

const TONES = {
  bad: { fg: 'var(--marker-deep)', bg: 'var(--status-failed-tint)', border: 'var(--marker)' },
  warn: { fg: 'var(--contour-text)', bg: 'var(--status-building-tint)', border: 'var(--contour)' },
  good: { fg: 'var(--vegetation-deep)', bg: 'var(--status-running-tint)', border: 'var(--vegetation-deep)' },
  none: { fg: 'var(--ink-secondary)', bg: 'var(--paper-sunken)', border: 'var(--rule-strong)' },
} as const;

export type Verdict = 'ok' | 'insecure' | 'unscanned' | 'inert' | '';

export function tone(score: number | null | undefined, verdict?: Verdict, threshold = 0) {
  if (score === null || score === undefined) return 'none' as const;
  if (verdict === 'insecure') return 'bad' as const;

  // Within ten of the floor: passing, and one new CVE from not passing.
  if (threshold > 0 && score < threshold + 10) return 'warn' as const;
  if (threshold > 0) return 'good' as const;

  // No threshold set. Bands, so the number still reads as something.
  if (score < 60) return 'bad' as const;
  if (score < 80) return 'warn' as const;
  return 'good' as const;
}

export function ScoreBadge({
  score,
  verdict,
  threshold = 0,
  full = false,
}: {
  score?: number | null;
  verdict?: Verdict;
  /** The installation's minimum, when the reader is allowed to know it. */
  threshold?: number;
  /** Show "/ 100". On in a table, where the bare number is ambiguous. */
  full?: boolean;
}) {
  const colors = TONES[tone(score, verdict, threshold)];

  if (score === null || score === undefined) {
    return (
      <span style={{ ...base, ...colors }} title="This app has not been scanned yet.">
        Not scanned
      </span>
    );
  }

  return (
    <span
      style={{ ...base, ...colors }}
      title={
        verdict === 'insecure'
          ? `Below this installation's requirement of ${threshold}`
          : 'Security score, out of 100'
      }
    >
      {score}
      {full ? ' / 100' : ''}
    </span>
  );
}

// 2px, the radius the system gives a tag: this is a label on an object, not an
// object of its own, and a pill is reserved for status dots and the switch.
const base = {
  display: 'inline-flex',
  alignItems: 'center',
  gap: 'var(--space-1)',
  padding: '0 var(--space-2)',
  font: 'var(--type-code-sm)',
  borderRadius: 'var(--radius-xs)',
  border: 'var(--border-width) solid',
  whiteSpace: 'nowrap',
} as const;
