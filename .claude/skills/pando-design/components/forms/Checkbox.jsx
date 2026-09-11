import React from 'react';

export function Checkbox({ label, description, checked = false, indeterminate = false, disabled = false, onChange, style, ...rest }) {
  const on = checked || indeterminate;
  return (
    <label style={{ display: 'inline-flex', alignItems: 'flex-start', gap: 'var(--space-2)', cursor: disabled ? 'not-allowed' : 'pointer', opacity: disabled ? 0.55 : 1, ...style }}>
      <input type="checkbox" checked={checked} disabled={disabled} onChange={onChange} style={{ position: 'absolute', opacity: 0, width: 0, height: 0 }} {...rest} />
      <span aria-hidden="true" style={{
        display: 'inline-flex', alignItems: 'center', justifyContent: 'center',
        width: 16, height: 16, marginTop: 2, flex: '0 0 auto',
        borderRadius: 'var(--radius-xs)',
        border: '1px solid ' + (on ? 'var(--ink)' : 'var(--field-border)'),
        background: on ? 'var(--ink)' : 'var(--paper-raised)',
      }}>
        {on && (
          <svg width="10" height="10" viewBox="0 0 10 10">
            {indeterminate
              ? <path d="M2 5h6" stroke="var(--paper)" strokeWidth="1.5" fill="none" />
              : <path d="M1.5 5.2l2.3 2.3L8.5 2.8" stroke="var(--paper)" strokeWidth="1.5" fill="none" />}
          </svg>
        )}
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
