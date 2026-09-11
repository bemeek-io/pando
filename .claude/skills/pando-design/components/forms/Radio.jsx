import React from 'react';

export function Radio({ label, description, checked = false, disabled = false, name, value, onChange, style, ...rest }) {
  return (
    <label style={{ display: 'inline-flex', alignItems: 'flex-start', gap: 'var(--space-2)', cursor: disabled ? 'not-allowed' : 'pointer', opacity: disabled ? 0.55 : 1, ...style }}>
      <input type="radio" name={name} value={value} checked={checked} disabled={disabled} onChange={onChange} style={{ position: 'absolute', opacity: 0, width: 0, height: 0 }} {...rest} />
      <span aria-hidden="true" style={{
        display: 'inline-flex', alignItems: 'center', justifyContent: 'center',
        width: 16, height: 16, marginTop: 2, flex: '0 0 auto',
        borderRadius: 'var(--radius-pill)',
        border: '1px solid ' + (checked ? 'var(--ink)' : 'var(--field-border)'),
        background: 'var(--paper-raised)',
      }}>
        {checked && <span style={{ width: 8, height: 8, borderRadius: 'var(--radius-pill)', background: 'var(--ink)' }} />}
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
