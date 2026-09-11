import React, { useState } from 'react';

const PADS = { none: 0, sm: 'var(--space-3)', md: 'var(--space-4)', lg: 'var(--space-5)' };

export function Card({ tone = 'paper', padding = 'md', interactive = false, as = 'div', style, children, ...rest }) {
  const [hover, setHover] = useState(false);
  const Tag = as;
  const tones = {
    paper: { background: 'var(--paper-raised)', borderColor: 'var(--rule)' },
    sunken: { background: 'var(--paper-sunken)', borderColor: 'var(--rule)' },
    plain: { background: 'transparent', borderColor: 'var(--rule)' },
  };
  return (
    <Tag
      onMouseEnter={interactive ? () => setHover(true) : undefined}
      onMouseLeave={interactive ? () => setHover(false) : undefined}
      style={{
        border: '1px solid', borderStyle: 'solid',
        borderRadius: 'var(--radius-md)',
        padding: PADS[padding] !== undefined ? PADS[padding] : PADS.md,
        boxShadow: 'none',
        cursor: interactive ? 'pointer' : undefined,
        transition: 'border-color var(--dur-fast) var(--ease), background-color var(--dur-fast) var(--ease)',
        ...tones[tone] || tones.paper,
        ...(interactive && hover ? { borderColor: 'var(--ink-secondary)' } : null),
        ...style,
      }}
      {...rest}
    >
      {children}
    </Tag>
  );
}
