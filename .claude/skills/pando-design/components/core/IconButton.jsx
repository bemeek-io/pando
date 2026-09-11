import React, { useState } from 'react';

export function IconButton({ label, size = 28, variant = 'ghost', onTerminal = false, disabled = false, onClick, style, children, ...rest }) {
  const [hover, setHover] = useState(false);
  const [press, setPress] = useState(false);
  return (
    <button
      type="button" aria-label={label} title={label} disabled={disabled} onClick={disabled ? undefined : onClick}
      onMouseEnter={() => setHover(true)}
      onMouseLeave={() => { setHover(false); setPress(false); }}
      onMouseDown={() => setPress(true)}
      onMouseUp={() => setPress(false)}
      style={{
        display: 'inline-flex', alignItems: 'center', justifyContent: 'center',
        width: size, height: size, padding: 0,
        borderRadius: 'var(--radius-sm)',
        border: variant === 'secondary' ? '1px solid var(--rule-strong)' : '1px solid transparent',
        background: hover && !disabled ? (onTerminal ? 'rgba(236,232,222,0.10)' : 'var(--paper-sunken)') : variant === 'secondary' ? 'var(--paper-raised)' : 'transparent',
        color: onTerminal ? 'var(--terminal-text)' : 'var(--ink-secondary)',
        cursor: disabled ? 'not-allowed' : 'pointer', opacity: disabled ? 0.5 : 1,
        transform: press && !disabled ? 'scale(var(--press-scale))' : 'none',
        transition: 'background-color var(--dur-fast) var(--ease), transform var(--press-duration) var(--ease)',
        ...style,
      }}
      {...rest}
    >
      {children}
    </button>
  );
}
