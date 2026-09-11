import React from 'react';

export function InlineCode({ style, children, ...rest }) {
  return (
    <code style={{ background: 'var(--vegetation)', color: 'var(--ink)', borderRadius: 'var(--radius-xs)', padding: '1px 4px', font: 'var(--type-code)', ...style }} {...rest}>
      {children}
    </code>
  );
}
