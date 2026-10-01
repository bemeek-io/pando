// A list edited one entry per line: an egress list, an app's own list.

import { useEffect, useState } from 'react';
import { Input } from '@design';

import { lines } from '../install/policyEgress';

/**
 * A list edited one entry per line.
 *
 * It keeps its own text: a field that re-rendered from the parsed list would
 * swallow the empty line a person has just started with Enter, and nobody
 * could type a second entry.
 */
export function ListField({
  value,
  onChange,
  label,
  helper,
  disabled,
  rows = 4,
}: {
  value: string[];
  onChange: (next: string[]) => void;
  label: string;
  helper?: string;
  disabled?: boolean;
  rows?: number;
}) {
  const outside = value.join('\n');
  const [text, setText] = useState(outside);
  useEffect(() => {
    // Changed from outside (Discard, a saved draft): show that instead.
    if (lines(text).join('\n') !== outside) setText(outside);
  }, [outside]);
  return (
    <Input
      label={label}
      as="textarea"
      rows={rows}
      mono
      disabled={disabled}
      value={text}
      helper={helper}
      onChange={(e) => {
        setText(e.target.value);
        onChange(lines(e.target.value));
      }}
    />
  );
}

