// A warning, rendered where it applies.
//
// Design 08 §1.3 requires warnings and blockers to be **visually distinct** —
// "a warning that looks like an error teaches people to ignore both" — and
// requires them inline at the thing they concern rather than stacked at the top
// of a page. Both constraints rule out `Banner`, whose own `.prompt.md` says
// never to stack banners and whose tones are info, running, building and
// failed. There is no warning tone, and there should not be one: the brand
// reserves marker red for exactly three things, one of which is a failed app.
//
// So a warning is distinct from an error by carrying **no red at all**. It sits
// on the sunken paper surface with a rule, an `info` symbol and its text. An
// error is red; a warning is quiet. That is a larger visual difference than any
// two tints would give, and it comes from the palette as it stands rather than
// from a color invented for the purpose.
//
// Dismissible and never blocking: a blocker is a PLAN_* error and looks
// completely different.

import { useState } from 'react';
import { Icon, IconButton, StatusSymbol } from '@design';

export interface InlineWarningProps {
  /** Rendered verbatim from the API. The console does not rewrite warning text. */
  children: React.ReactNode;
  /** The warning's code, for the dismissal to be recorded against. */
  code?: string;
  onDismiss?: (code: string) => void;
  /**
   * The way to the place this is fixed.
   *
   * A warning that says "define one here" and sits on a screen where nothing
   * can be defined is a warning nobody acts on — which is how people learn to
   * dismiss all of them. Where the console has a screen for the fix, the
   * warning carries a way to it.
   */
  action?: React.ReactNode;
}

export function InlineWarning({ children, code, onDismiss, action }: InlineWarningProps) {
  const [dismissed, setDismissed] = useState(false);
  if (dismissed) return null;

  function dismiss() {
    setDismissed(true);
    if (code && onDismiss) onDismiss(code);
  }

  return (
    <div
      role="note"
      style={{
        display: 'flex',
        alignItems: 'flex-start',
        gap: 'var(--space-3)',
        padding: 'var(--space-3)',
        background: 'var(--paper-sunken)',
        border: 'var(--border-width) solid var(--rule)',
        borderRadius: 'var(--radius-sm)',
      }}
    >
      <span style={{ paddingTop: 'var(--space-1)', flex: '0 0 auto' }}>
        <StatusSymbol status="info" />
      </span>
      <div style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-2)', flex: 1 }}>
        <p style={{ font: 'var(--type-body-ui)', color: 'var(--ink)', margin: 0 }}>{children}</p>
        {action}
      </div>
      <IconButton label="Dismiss this warning" onClick={dismiss}>
        <Icon name="x" size={16} />
      </IconButton>
    </div>
  );
}
