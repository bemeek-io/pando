import React from 'react';
import { ContourMap } from '../brand/ContourMap.jsx';

export function EmptyState({ heading, children, action, size = 120, style, ...rest }) {
  return (
    <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'center', textAlign: 'center', gap: 'var(--space-3)', padding: 'var(--space-7) var(--space-5)', ...style }} {...rest}>
      <ContourMap size={size} rings={4} style={{ marginBottom: 'var(--space-2)' }} />
      <h3 style={{ font: 'var(--type-h3)', color: 'var(--ink)' }}>{heading}</h3>
      {children && <p style={{ font: 'var(--type-body-ui)', color: 'var(--ink-secondary)', maxWidth: '46ch' }}>{children}</p>}
      {action && <div style={{ marginTop: 'var(--space-2)' }}>{action}</div>}
    </div>
  );
}
