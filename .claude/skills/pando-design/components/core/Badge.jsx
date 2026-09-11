import React from 'react';

export function Badge({ count, style, children, ...rest }) {
  return (
    <span
      style={{
        display: 'inline-flex', alignItems: 'center', justifyContent: 'center',
        minWidth: 18, height: 18, padding: '0 5px',
        borderRadius: 'var(--radius-xs)',
        background: 'var(--paper-sunken)', color: 'var(--ink-secondary)',
        font: 'var(--type-code-sm)',
        ...style,
      }}
      {...rest}
    >
      {count !== undefined ? count : children}
    </span>
  );
}
