// A question for an AI function, and nothing else: one labeled field and one
// button beside it (R-343 … R-346).
//
// Each of the administrative AI functions takes one sentence and gives back a
// draft, a filter or an answer, so each screen that offers one offers it the
// same way. The button is secondary: every screen this sits on already has its
// own primary action, and asking the AI is never the main thing a screen does.

import { useState } from 'react';
import { Button, Input } from '@design';

import { refusal } from '../install/Accounts';
import { BesideField } from './BesideField';

export function AskAI({
  label,
  placeholder,
  action = 'Ask',
  pending,
  error,
  onAsk,
}: {
  label: string;
  placeholder?: string;
  /** The button's word. It keeps its name through the flow. */
  action?: string;
  pending: boolean;
  error?: unknown;
  onAsk: (text: string) => void;
}) {
  const [text, setText] = useState('');
  const ask = () => {
    if (text.trim() && !pending) onAsk(text.trim());
  };

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-2)' }}>
      <form
        onSubmit={(e) => {
          e.preventDefault();
          ask();
        }}
        style={{ display: 'flex', gap: 'var(--space-3)', alignItems: 'flex-end', flexWrap: 'wrap' }}
      >
        <Input
          label={label}
          placeholder={placeholder}
          value={text}
          onChange={(e) => setText(e.target.value)}
          style={{ flex: '1 1 24rem' }}
        />
        <BesideField>
          <Button type="submit" disabled={!text.trim() || pending}>
            {pending ? `${action}ing…` : action}
          </Button>
        </BesideField>
      </form>
      {error != null && (
        <p style={{ font: 'var(--type-body-ui)', color: 'var(--ink-secondary)', margin: 0 }}>{refusal(error)}</p>
      )}
    </div>
  );
}

/** Which adapter and model answered, said once under the answer (R-337's
 *  honesty, for these functions). */
export function AnsweredBy({ adapter, model }: { adapter?: string; model?: string }) {
  if (!adapter) return null;
  return (
    <p style={{ font: 'var(--type-caption)', color: 'var(--ink-secondary)', margin: 0 }}>
      From the AI adapter {adapter}
      {model ? `, ${model}` : ''}. Check it before you use it.
    </p>
  );
}
