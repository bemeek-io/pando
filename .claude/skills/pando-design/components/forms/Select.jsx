import React, { useState } from 'react';

export function Select({ label, helper, options = [], mono = false, disabled = false, id, style, ...rest }) {
  const [focus, setFocus] = useState(false);
  const selectId = id || (label ? 's-' + label.replace(/\W+/g, '-').toLowerCase() : undefined);
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-1)', ...style }}>
      {label && <label htmlFor={selectId} style={{ font: 'var(--type-label)', color: 'var(--ink)' }}>{label}</label>}
      <div style={{ position: 'relative', display: 'flex' }}>
        <select id={selectId} disabled={disabled}
          onFocus={() => setFocus(true)} onBlur={() => setFocus(false)}
          style={{
            appearance: 'none', WebkitAppearance: 'none', flex: 1,
            height: 'var(--control-input)', padding: '0 28px 0 10px',
            background: 'var(--paper-raised)',
            border: '1px solid var(--field-border)', borderRadius: 'var(--radius-sm)',
            font: mono ? 'var(--type-code)' : 'var(--type-body-ui)',
            color: disabled ? 'var(--ink-muted)' : 'var(--ink)',
            outline: focus ? '2px solid var(--ink)' : 'none', outlineOffset: focus ? 2 : 0,
            cursor: disabled ? 'not-allowed' : 'pointer',
          }}
          {...rest}
        >
          {options.map((o) => {
            const opt = typeof o === 'string' ? { value: o, label: o } : o;
            return <option key={opt.value} value={opt.value}>{opt.label}</option>;
          })}
        </select>
        <svg aria-hidden="true" width="10" height="6" viewBox="0 0 10 6" style={{ position: 'absolute', right: 10, top: '50%', marginTop: -3, pointerEvents: 'none' }}>
          <path d="M1 1l4 4 4-4" fill="none" stroke="var(--ink-secondary)" strokeWidth="1.5" />
        </svg>
      </div>
      {helper && <span style={{ font: 'var(--type-caption)', color: 'var(--ink-secondary)' }}>{helper}</span>}
    </div>
  );
}
