import React from 'react';

export function Switch({ checked = false, disabled = false, label, description, onChange, style, ...rest }) {
  return (
    <label style={{ display: 'inline-flex', alignItems: 'flex-start', gap: 'var(--space-3)', cursor: disabled ? 'not-allowed' : 'pointer', opacity: disabled ? 0.55 : 1, ...style }}>
      <input type="checkbox" role="switch" checked={checked} disabled={disabled} onChange={onChange} style={{ position: 'absolute', opacity: 0, width: 0, height: 0 }} {...rest} />
      <span aria-hidden="true" style={{
        position: 'relative', flex: '0 0 auto', width: 34, height: 20, marginTop: 1,
        borderRadius: 'var(--radius-pill)',
        background: checked ? 'var(--ink)' : 'var(--paper-sunken)',
        border: '1px solid ' + (checked ? 'var(--ink)' : 'var(--field-border)'),
        transition: 'background-color var(--dur-fast) var(--ease), border-color var(--dur-fast) var(--ease)',
      }}>
        <span style={{
          position: 'absolute', top: 2, left: checked ? 16 : 2, width: 14, height: 14,
          borderRadius: 'var(--radius-pill)', background: 'var(--paper-raised)',
          transition: 'left var(--dur-fast) var(--ease)',
        }} />
      </span>
      {(label || description) && (
        <span style={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
          {label && <span style={{ font: 'var(--type-body-ui)', color: 'var(--ink)' }}>{label}</span>}
          {description && <span style={{ font: 'var(--type-caption)', color: 'var(--ink-secondary)' }}>{description}</span>}
        </span>
      )}
    </label>
  );
}
