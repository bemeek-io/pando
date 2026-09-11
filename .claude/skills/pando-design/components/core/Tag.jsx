import React from 'react';

export function Tag({ mono = false, icon = null, tone = 'default', style, children, ...rest }) {
  const tones = {
    default: { background: 'var(--paper-sunken)', color: 'var(--ink-secondary)', borderColor: 'var(--rule)' },
    contour: { background: 'transparent', color: 'var(--contour-text)', borderColor: 'var(--contour-line)' },
    vegetation: { background: 'var(--vegetation)', color: 'var(--ink)', borderColor: 'transparent' },
  };
  return (
    <span
      style={{
        display: 'inline-flex', alignItems: 'center', gap: 'var(--space-1)',
        padding: '1px 6px', borderRadius: 'var(--radius-xs)',
        border: '1px solid', borderStyle: 'solid',
        font: mono ? 'var(--type-code-sm)' : 'var(--type-caption)',
        ...tones[tone] || tones.default,
        ...style,
      }}
      {...rest}
    >
      {icon}{children}
    </span>
  );
}
