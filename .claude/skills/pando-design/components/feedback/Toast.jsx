import React from 'react';
import { StatusSymbol } from '../data/StatusIndicator.jsx';
import { IconButton } from '../core/IconButton.jsx';
import { Icon } from '../core/Icon.jsx';

export function Toast({ status = 'info', action, onDismiss, style, children, ...rest }) {
  return (
    <div
      role="status"
      style={{
        display: 'flex', alignItems: 'center', gap: 'var(--space-3)',
        maxWidth: 420, padding: 'var(--space-3) var(--space-3) var(--space-3) var(--space-4)',
        background: 'var(--paper-raised)',
        border: '1px solid var(--rule-strong)', borderRadius: 'var(--radius-sm)',
        boxShadow: 'var(--shadow-popover)',
        font: 'var(--type-body-ui)', color: 'var(--ink)',
        ...style,
      }}
      {...rest}
    >
      <StatusSymbol status={status} size={8} />
      <span style={{ flex: 1 }}>{children}</span>
      {action}
      {onDismiss && <IconButton label="Dismiss" size={24} onClick={onDismiss}><Icon name="x" size={14} /></IconButton>}
    </div>
  );
}
