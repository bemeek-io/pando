import React, { useState } from 'react';

export function Input({ label, helper, error, mono = false, as = 'input', rows = 4, disabled = false, id, style, ...rest }) {
  const [focus, setFocus] = useState(false);
  const inputId = id || (label ? 'f-' + label.replace(/\W+/g, '-').toLowerCase() : undefined);
  const Tag = as === 'textarea' ? 'textarea' : 'input';
  const shared = {
    width: '100%',
    height: as === 'textarea' ? undefined : 'var(--control-input)',
    padding: as === 'textarea' ? 'var(--space-2) 10px' : '0 10px',
    background: 'var(--paper-raised)',
    border: '1px solid ' + (error ? 'var(--marker)' : 'var(--field-border)'),
    borderRadius: 'var(--radius-sm)',
    font: mono ? 'var(--type-code)' : 'var(--type-body-ui)',
    color: disabled ? 'var(--ink-muted)' : 'var(--ink)',
    outline: focus ? '2px solid var(--ink)' : 'none',
    outlineOffset: focus ? 2 : 0,
    resize: as === 'textarea' ? 'vertical' : undefined,
  };
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-1)', ...style }}>
      {label && <label htmlFor={inputId} style={{ font: 'var(--type-label)', color: 'var(--ink)' }}>{label}</label>}
      <Tag id={inputId} rows={as === 'textarea' ? rows : undefined} disabled={disabled}
        onFocus={() => setFocus(true)} onBlur={() => setFocus(false)}
        style={shared} {...rest} />
      {(helper || error) && (
        <span style={{ font: 'var(--type-caption)', color: error ? 'var(--marker-deep)' : 'var(--ink-secondary)' }}>{error || helper}</span>
      )}
    </div>
  );
}
