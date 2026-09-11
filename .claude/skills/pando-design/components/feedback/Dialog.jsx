import React from 'react';

export function Dialog({ open = false, title, description, footer, width = 460, onClose, style, children, ...rest }) {
  if (!open) return null;
  return (
    <div
      onClick={onClose}
      style={{ position: 'absolute', inset: 0, zIndex: 60, display: 'flex', alignItems: 'center', justifyContent: 'center', padding: 'var(--space-6)', background: 'var(--scrim)' }}
    >
      <div
        role="dialog" aria-modal="true" onClick={(e) => e.stopPropagation()}
        style={{
          width, maxWidth: '100%',
          background: 'var(--paper-raised)',
          border: '1px solid var(--rule)', borderRadius: 'var(--radius-lg)',
          boxShadow: 'var(--shadow-popover)',
          ...style,
        }}
        {...rest}
      >
        {(title || description) && (
          <div style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-2)', padding: 'var(--space-5) var(--space-5) var(--space-3)' }}>
            {title && <h3 style={{ font: 'var(--type-h3)', color: 'var(--ink)' }}>{title}</h3>}
            {description && <p style={{ font: 'var(--type-body-ui)', color: 'var(--ink-secondary)' }}>{description}</p>}
          </div>
        )}
        {children && <div style={{ padding: '0 var(--space-5) var(--space-4)' }}>{children}</div>}
        {footer && (
          <div style={{ display: 'flex', justifyContent: 'flex-end', gap: 'var(--space-2)', padding: 'var(--space-4) var(--space-5)', borderTop: '1px solid var(--rule)' }}>{footer}</div>
        )}
      </div>
    </div>
  );
}
