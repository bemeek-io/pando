// A console page: heading, action beside it, content below.
//
// Uncapped, to match ui/layout.ts: rows and their rules run the full window,
// and what is capped is the content inside a section, by MEASURE. No frame —
// the topographic terrain lives behind the console (TopoBackground), not
// around each page.
//
// Every admin screen and the launcher go through here, so the page layout is
// defined once. Three page wrappers had drifted into three copies of the same
// header before this existed.

export function Sheet({
  heading,
  action,
  // An app's id, shown under its name: the value the CLI and the API want,
  // which the console otherwise only showed in the address bar.
  note,
  lede,
  children,
}: {
  heading?: React.ReactNode;
  action?: React.ReactNode;
  note?: string;
  lede?: React.ReactNode;
  children: React.ReactNode;
}) {
  const hasHeader = Boolean(heading || action || note || lede);

  return (
    <div>
      {hasHeader && (
        <header
          style={{
            display: 'flex',
            flexDirection: 'column',
            gap: 'var(--space-2)',
            padding: 'var(--space-6) var(--console-padding) var(--space-4)',
          }}
        >
          {/* The action sits beside the heading rather than at the far end of
              an uncapped row, where on a wide display it would be a long way
              from the thing it acts on. It wraps rather than overflowing: on a
              phone a heading, a button and a search do not fit on one line,
              and the page should not scroll sideways. */}
          <div style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 'var(--space-3) var(--space-4)' }}>
            {typeof heading === 'string' ? (
              <h3 style={{ font: 'var(--type-h3)', margin: 0 }}>{heading}</h3>
            ) : (
              heading
            )}
            {action}
          </div>
          {lede}
          {note && (
            <span style={{ font: 'var(--type-code-sm)', color: 'var(--ink-secondary)' }}>
              {note}
            </span>
          )}
        </header>
      )}
      <div
        style={{
          padding: hasHeader
            ? '0 var(--console-padding) var(--space-7)'
            : 'var(--space-6) var(--console-padding) var(--space-7)',
        }}
      >
        {children}
      </div>
    </div>
  );
}
