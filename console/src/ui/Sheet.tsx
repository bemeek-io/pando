// A console page, printed as a survey sheet.
//
// The brand's one bold element is the contour figure, and it is confined to
// four places — hero, docs home header, empty states, 404. None of them is a
// console screen, which is why the console carried no trace of the brand at
// all: every screen was correct paper, correct ink, correct type, and could
// have belonged to any product.
//
// The other half of the topo language is the sheet itself: the neatline with
// its corner ticks, and the marginal data printed outside it. That is what
// `MapCollar` draws, and unlike the contour it is structure rather than
// illustration, so it can appear on every screen without the system's one-bold-
// element rule ever coming into it. The spec's own hero prints a contour inside
// a collar, which is the precedent.
//
// Every admin screen and the launcher go through here, so the frame is defined
// once. Three page wrappers had already drifted into three copies of the same
// header before this existed.

import { MapCollar } from '@design';

export function Sheet({
  heading,
  action,
  // Marginal data: short, machine, and real. A quadrangle's collar carries the
  // sheet's own identifiers, so this is where an app's id belongs — the thing
  // somebody needs when they move from the console to the CLI, and which had
  // nowhere to be shown. Never invented: coordinates printed on a settings
  // screen are costume, and the brand's first rule is that the heritage lives
  // in color, type and one illustration system, not in period dress.
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
  return (
    <div style={{ maxWidth: 'var(--console-max)', padding: 'var(--console-padding)' }}>
      <MapCollar inset={false}>
        {(heading || action || note || lede) && (
          <header
            style={{
              display: 'flex',
              flexDirection: 'column',
              gap: 'var(--space-2)',
              padding: 'var(--space-5) var(--space-5) var(--space-4)',
              borderBottom: 'var(--border-width) solid var(--rule)',
            }}
          >
            <div
              style={{
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'space-between',
                gap: 'var(--space-4)',
              }}
            >
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
        <div style={{ padding: 'var(--space-5)' }}>{children}</div>
      </MapCollar>
    </div>
  );
}
