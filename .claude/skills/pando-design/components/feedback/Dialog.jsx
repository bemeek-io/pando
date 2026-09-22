import React from 'react';

export function Dialog({ open = false, title, description, footer, width = 460, onClose, style, children, ...rest }) {
  // Escape closes it. A modal that can only be dismissed by finding the Cancel
  // button is one people back out of with the browser's back button, which on a
  // single-page app leaves the page somewhere they did not ask to be.
  React.useEffect(() => {
    if (!open || !onClose) return undefined;
    const onKey = (e) => { if (e.key === 'Escape') onClose(); };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [open, onClose]);

  if (!open) return null;
  return (
    <div
      onClick={onClose}
      style={{
        // Fixed, not absolute. Absolute sizes to the initial containing block,
        // so the scrim covered exactly one viewport-height of the document and
        // scrolled away with the page: scroll down and the content below was
        // undarkened and still clickable, behind a dialog claiming
        // aria-modal="true". Fixed pins it to the viewport, which is what a
        // modal means.
        position: 'fixed', inset: 0, zIndex: 60,
        display: 'flex', alignItems: 'center', justifyContent: 'center',
        padding: 'var(--space-6)', background: 'var(--scrim)',
      }}
    >
      <div
        role="dialog" aria-modal="true" onClick={(e) => e.stopPropagation()}
        style={{
          width, maxWidth: '100%',
          // Never taller than the window. A dialog that was, centered in it, had
          // its top pushed off the screen where no scrolling reaches — the
          // title and the first fields gone. Capped instead, with the title
          // and the footer fixed and only the body between them scrolling, so
          // the confirming button is always on screen too.
          maxHeight: 'calc(100vh - 2 * var(--space-6))',
          display: 'flex', flexDirection: 'column',
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
        {children && <div style={{ padding: '0 var(--space-5) var(--space-4)', overflowY: 'auto', flex: '1 1 auto', minHeight: 0 }}>{children}</div>}
        {footer && (
          <div style={{ display: 'flex', justifyContent: 'flex-end', gap: 'var(--space-2)', padding: 'var(--space-4) var(--space-5)', borderTop: '1px solid var(--rule)' }}>{footer}</div>
        )}
      </div>
    </div>
  );
}
