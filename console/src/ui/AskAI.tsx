// Asking AI to do something, marked as that (R-343 … R-346).
//
// The same shape as "Ask AI about this plan" on the plan page: the AI mark and
// a heading that says AI is being asked, one line on what it will do and that
// nothing happens until you act on it, then the field and an Ask AI button.
// Secondary, never the screen's primary action: every screen this sits on
// already has one, and asking AI is never the main thing a screen does.

import { useState } from 'react';
import { Button, Input } from '@design';

import { refusal } from '../install/Accounts';
import { AiStar } from './AiStar';
import { BesideField } from './BesideField';

export function AskAI({
  heading,
  explanation,
  label,
  placeholder,
  pending,
  error,
  onAsk,
}: {
  /** "Ask AI to …", so there is no doubt who is being asked. */
  heading: string;
  /** What AI will do with it, and what it will not. */
  explanation: string;
  /** The field's own label. */
  label: string;
  placeholder?: string;
  pending: boolean;
  error?: unknown;
  onAsk: (text: string) => void;
}) {
  const [text, setText] = useState('');
  const ask = () => {
    if (text.trim() && !pending) onAsk(text.trim());
  };

  return (
    <section style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-3)' }}>
      <div style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-1)' }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 'var(--space-2)' }}>
          <AiStar size={18} />
          <h4 style={{ font: 'var(--type-h4)', margin: 0 }}>{heading}</h4>
        </div>
        <span style={{ font: 'var(--type-body-ui)', color: 'var(--ink-secondary)' }}>{explanation}</span>
      </div>
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
            {pending ? 'Asking' : 'Ask AI'}
          </Button>
        </BesideField>
      </form>
      {error != null && (
        <p style={{ font: 'var(--type-body-ui)', color: 'var(--ink-secondary)', margin: 0 }}>{refusal(error)}</p>
      )}
    </section>
  );
}

/** Which adapter and model answered, said once under the answer, with the AI
 *  mark so the answer is never mistaken for Pando's own. */
export function AnsweredBy({ adapter, model }: { adapter?: string; model?: string }) {
  if (!adapter) return null;
  return (
    <p
      style={{
        display: 'flex',
        alignItems: 'center',
        gap: 'var(--space-2)',
        font: 'var(--type-caption)',
        color: 'var(--ink-secondary)',
        margin: 0,
      }}
    >
      <AiStar size={12} />
      <span>
        Written by AI ({adapter}
        {model ? `, ${model}` : ''}). Check it before you use it.
      </span>
    </p>
  );
}
