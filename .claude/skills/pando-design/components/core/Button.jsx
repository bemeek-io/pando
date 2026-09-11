import React, { useState } from 'react';

const VARIANTS = {
  primary: { background: 'var(--ink)', color: 'var(--paper)', border: '1px solid var(--ink)', hover: { background: 'var(--primary-hover)', borderColor: 'var(--primary-hover)' } },
  secondary: { background: 'var(--paper-raised)', color: 'var(--ink)', border: '1px solid var(--rule-strong)', hover: { borderColor: 'var(--ink-secondary)' } },
  ghost: { background: 'transparent', color: 'var(--ink)', border: '1px solid transparent', hover: { background: 'var(--paper-sunken)' } },
  destructive: { background: 'var(--paper-raised)', color: 'var(--marker-deep)', border: '1px solid var(--marker)', hover: { background: 'var(--destructive-hover)' } },
};

export function Button({ variant = 'secondary', size = 'console', icon = null, disabled = false, fullWidth = false, type = 'button', onClick, style, children, ...rest }) {
  const [hover, setHover] = useState(false);
  const [press, setPress] = useState(false);
  const v = VARIANTS[variant] || VARIANTS.secondary;
  const marketing = size === 'marketing';
  return (
    <button
      type={type} disabled={disabled} onClick={disabled ? undefined : onClick}
      onMouseEnter={() => setHover(true)}
      onMouseLeave={() => { setHover(false); setPress(false); }}
      onMouseDown={() => setPress(true)}
      onMouseUp={() => setPress(false)}
      style={{
        display: fullWidth ? 'flex' : 'inline-flex', width: fullWidth ? '100%' : undefined,
        alignItems: 'center', justifyContent: 'center', gap: 'var(--space-2)',
        height: marketing ? 'var(--control-marketing)' : 'var(--control-console)',
        padding: marketing ? '0 var(--button-padding-marketing)' : '0 var(--button-padding-console)',
        font: 'var(--type-label)', borderRadius: 'var(--radius-sm)',
        cursor: disabled ? 'not-allowed' : 'pointer', opacity: disabled ? 0.5 : 1,
        whiteSpace: 'nowrap',
        transform: press && !disabled ? 'scale(var(--press-scale))' : 'none',
        transition: 'background-color var(--dur-fast) var(--ease), border-color var(--dur-fast) var(--ease), color var(--dur-fast) var(--ease), transform var(--press-duration) var(--ease)',
        ...v, hover: undefined,
        ...(hover && !disabled ? v.hover : null),
        ...style,
      }}
      {...rest}
    >
      {icon}
      {children}
    </button>
  );
}
