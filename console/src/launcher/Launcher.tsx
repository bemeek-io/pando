// The launcher (R-264).
//
// Root is this, not a dashboard. A non-technical user's first experience is a
// page of tiles for the apps they can reach — and R-005 is the reason: someone
// who may not know what a port is should not be met by a management console.
//
// Tiles come from GET /me/apps, which is scoped to **data-plane** grants. That
// is a different list from GET /apps, which is control-plane scoped: two
// planes, two endpoints (R-070, R-071). Using the wrong one here would show
// someone an app they can administer but not open, or hide one they use daily.
//
// R-266: sharing sends no message. An app appearing here is the notification,
// so the list is the whole mechanism and has to be right.

import { useQuery } from '@tanstack/react-query';
import { EmptyState, Icon, IconButton, Logo } from '@design';


import { api, base } from '@api/client';
import type { App } from '@api/types.gen';
import { statusLabel } from '../ui/status';
import { Sheet } from '../ui/Sheet';
import { TopoBackground } from '../ui/TopoBackground';

export function Launcher({
  onAdmin,
  onSettings,
}: {
  onAdmin?: () => void;
  /** Theme and signing out, which everyone can reach. */
  onSettings: () => void;
}) {
  const apps = useQuery({
    queryKey: ['me', 'apps'],
    queryFn: () => api.get<{ apps: App[] | null }>('/me/apps'),
  });

  return (
    <div style={{ minHeight: '100vh', background: 'var(--paper)', position: 'relative', isolation: 'isolate' }}>
      <TopoBackground seed="launcher" />
      <header
        style={{
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
          padding: 'var(--space-5) var(--console-padding)',
          borderBottom: 'var(--border-width) solid var(--rule)',
        }}
      >
        <Logo size={20} />
        <div style={{ display: 'flex', alignItems: 'center', gap: 'var(--space-4)' }}>
        {onAdmin && (
          <button
            onClick={onAdmin}
            style={{
              border: 'none',
              background: 'transparent',
              padding: 0,
              cursor: 'pointer',
              font: 'var(--type-body-ui)',
              color: 'var(--ink-secondary)',
            }}
          >
            Admin
          </button>
        )}
        <IconButton label="Settings" onClick={onSettings}>
          <Icon name="settings" size={16} />
        </IconButton>
        </div>
      </header>

      <main style={{ maxWidth: 'var(--console-max)', margin: '0 auto' }}>
        {/* The same sheet the admin console is printed on, so the two halves of
            the product read as one thing. The launcher is the screen a
            non-technical person meets first (R-005), and it gets the frame and
            the corner ticks and nothing else — the contour only turns up when
            there is nothing to show. */}
        <Sheet heading="Your apps">
          {apps.isPending && <Quiet>Loading your apps.</Quiet>}

          {apps.isError && (
            <Quiet>Pando couldn&rsquo;t load your apps. Reload the page to try again.</Quiet>
          )}

          {apps.data && <Tiles apps={apps.data.apps ?? []} />}
        </Sheet>
      </main>
    </div>
  );
}

function Tiles({ apps }: { apps: App[] }) {
  if (apps.length === 0) {
    // Not "you have no apps" — nothing has gone wrong, and an empty launcher is
    // the normal state for someone who has just been given an account.
    return (
      <EmptyState heading="Nothing shared with you yet">
        When someone shares an app with you, it shows up here.
      </EmptyState>
    );
  }

  return (
    <div
      style={{
        display: 'grid',
        // Fixed tracks, not 1fr: a square that stretched to fill the row would
        // be a different size on every window, and a poster on a wide one.
        gridTemplateColumns: 'repeat(auto-fill, 16ch)',
        gap: 'var(--space-6) var(--space-5)',
      }}
    >
      {apps.map((app) => (
        <Tile key={app.id} app={app} />
      ))}
    </div>
  );
}

/**
 * Whether opening the app would reach it.
 *
 * Running or degraded — degraded is still serving, just not every part of it
 * — and with an address. Anything else is greyed out and is not a link.
 */
function reachable(app: App): boolean {
  return Boolean(app.address) && (app.state === 'running' || app.state === 'degraded');
}

function Tile({ app }: { app: App }) {
  // A square and a name, and no status line. The launcher is for someone who
  // came to open an app (R-005), and "running" is the normal case — a word on
  // every tile saying so is noise. What they need to know is which tiles will
  // not open, and a greyed-out tile says that without a word.
  //
  // The state is still in the accessible name and the hover title, so the
  // difference is never carried by appearance alone.
  const open = reachable(app);
  const label = open ? app.name : `${app.name} — ${statusLabel(app.state)}`;

  const body = (
    <div
      style={{
        display: 'flex',
        flexDirection: 'column',
        gap: 'var(--space-2)',
        opacity: open ? 1 : 0.45,
        filter: open ? 'none' : 'grayscale(1)',
      }}
    >
      <Square app={app} />
      <span
        style={{
          font: 'var(--type-body-ui)',
          color: 'var(--ink)',
          textAlign: 'center',
          overflow: 'hidden',
          textOverflow: 'ellipsis',
          whiteSpace: 'nowrap',
        }}
      >
        {app.name}
      </span>
    </div>
  );

  if (!open) {
    return (
      <div title={statusLabel(app.state)} aria-label={label} aria-disabled="true">
        {body}
      </div>
    );
  }

  // An app is a different place from the console. Opening it over the top of
  // Pando means the way back is the browser's history, and for someone who
  // came to the launcher to open two apps it means coming back here every time.
  //
  // Every one of these addresses goes through Pando's proxy — there is no
  // other way in (R-023) — but *which* address depends on how the app is
  // routed, and the server is what knows.
  return (
    <a
      href={app.address}
      target="_blank"
      rel="noopener noreferrer"
      aria-label={label}
      style={{ textDecoration: 'none', color: 'inherit' }}
    >
      {body}
    </a>
  );
}

/**
 * The tile's picture: the app's image when it has one (R-340), and its
 * initial when it does not.
 */
function Square({ app }: { app: App }) {
  // The timestamp is in the URL so a new image is a new address, and the
  // browser's cached copy of the old one is never shown.
  const src = app.icon_updated_at
    ? `${base}/apps/${app.id}/icon?v=${encodeURIComponent(app.icon_updated_at)}`
    : null;

  return (
    <div
      style={{
        aspectRatio: '1 / 1',
        borderRadius: 'var(--radius-md)',
        border: 'var(--border-width) solid var(--rule)',
        background: 'var(--paper-sunken)',
        overflow: 'hidden',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
      }}
    >
      {src ? (
        <img src={src} alt="" style={{ width: '100%', height: '100%', objectFit: 'cover', display: 'block' }} />
      ) : (
        <span aria-hidden="true" style={{ font: 'var(--type-display)', color: 'var(--ink-secondary)' }}>
          {initial(app.name)}
        </span>
      )}
    </div>
  );
}

function initial(name: string): string {
  return Array.from(name.trim())[0]?.toUpperCase() ?? '';
}

function Quiet({ children }: { children: React.ReactNode }) {
  return (
    <p style={{ font: 'var(--type-body-ui)', color: 'var(--ink-secondary)' }}>{children}</p>
  );
}
