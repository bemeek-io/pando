import React, { useState } from 'react';
import { IconButton } from '../core/IconButton.jsx';
import { Icon } from '../core/Icon.jsx';

/** Terminal surface: always ink background with terminal-text, in both themes. */
export function CodeBlock({ lines = [], prompt = false, title, copyable = true, dense = false, style, children, ...rest }) {
  const [copied, setCopied] = useState(false);
  const rows = Array.isArray(lines) ? lines : String(lines).split('\n');
  const copy = () => { setCopied(true); window.setTimeout(() => setCopied(false), 1400); };
  return (
    <div
      style={{
        position: 'relative',
        background: 'var(--terminal)', color: 'var(--terminal-text)',
        border: '1px solid var(--rule-strong)',
        borderRadius: 'var(--radius-md)',
        padding: dense ? 'var(--space-3)' : 'var(--space-4)',
        font: 'var(--type-code)',
        overflow: 'hidden',
        ...style,
      }}
      {...rest}
    >
      {title && (
        <div style={{ font: 'var(--type-code-sm)', color: 'rgba(236,232,222,0.55)', marginBottom: 'var(--space-2)' }}>{title}</div>
      )}
      {copyable && (
        <div style={{ position: 'absolute', top: 'var(--space-2)', right: 'var(--space-2)', display: 'flex', alignItems: 'center', gap: 'var(--space-2)' }}>
          {copied && <span style={{ font: 'var(--type-code-sm)', color: 'rgba(236,232,222,0.55)' }}>Copied</span>}
          <IconButton label="Copy" onTerminal onClick={copy}><Icon name="copy" size={14} color="var(--terminal-text)" /></IconButton>
        </div>
      )}
      {children || rows.map((l, i) => {
        const line = typeof l === 'string' ? { text: l } : l;
        return (
          <div key={i} style={{ display: 'flex', gap: 'var(--space-2)', whiteSpace: 'pre-wrap', color: line.tone === 'muted' ? 'rgba(236,232,222,0.55)' : line.tone === 'ok' ? 'var(--vegetation-deep)' : line.tone === 'fail' ? 'var(--marker)' : 'var(--terminal-text)' }}>
            {prompt && <span style={{ color: 'var(--terminal-prompt)', flex: '0 0 auto' }}>$</span>}
            {line.time && <span style={{ color: 'rgba(236,232,222,0.38)', flex: '0 0 auto' }}>{line.time}</span>}
            <span>{line.text}</span>
          </div>
        );
      })}
    </div>
  );
}
