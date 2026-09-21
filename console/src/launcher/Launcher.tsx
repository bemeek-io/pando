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
import { Card, EmptyState, Logo, StatusIndicator } from '@design';

import { ThemeToggle } from '../ui/ThemeToggle';
import { SignOut } from '../ui/SignOut';

import { api } from '@api/client';
import type { App } from '@api/types.gen';
import { statusLabel, statusSymbol } from '../ui/status';
import { Sheet } from '../ui/Sheet';
import { TopoBackground } from '../ui/TopoBackground';

export function Launcher({
  onAdmin,
  onReference,
  onSignedOut,
}: {
  onAdmin?: () => void;
  /** The API screen, which everyone can reach (R-261, R-262). */
  onReference?: () => void;
  onSignedOut?: () => void;
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
        <ThemeToggle />
        {onReference && (
          <button
            onClick={onReference}
            style={{
              border: 'none',
              background: 'transparent',
              padding: 0,
              cursor: 'pointer',
              font: 'var(--type-body-ui)',
              color: 'var(--ink-secondary)',
            }}
          >
            API and tools
          </button>
        )}
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
        <SignOut onSignedOut={onSignedOut} />
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
        gridTemplateColumns: 'repeat(auto-fill, minmax(22ch, 1fr))',
        gap: 'var(--space-4)',
      }}
    >
      {apps.map((app) => (
        <Tile key={app.id} app={app} />
      ))}
    </div>
  );
}

function Tile({ app }: { app: App }) {
  // The tile is the whole link, which is what `interactive` is for. Every one
  // of these addresses goes through Pando's proxy — there is no other way in
  // (R-023) — but *which* address depends on how the app is routed, and the
  // server is what knows. This used to be "/" + slug, the path-mode answer,
  // handed out on installs where no app is in path mode: on a laptop every app
  // is on its own port, and every tile here linked to a page that was not it.
  //
  // An app that has never been deployed has no address at all, and its tile is
  // not a link. Falling back to a guess would put the same wrong address back,
  // just for fewer apps — and the status underneath already says why there is
  // nowhere to go yet.
  const body = (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-2)' }}>
      <span style={{ font: 'var(--type-h4)', color: 'var(--ink)' }}>{app.name}</span>
      <StatusIndicator status={statusSymbol(app.state)} label={statusLabel(app.state)} />
    </div>
  );

  if (!app.address) {
    return <Card padding="md">{body}</Card>;
  }

  return (
    <Card
      as="a"
      interactive
      padding="md"
      // An app is a different place from the console. Opening it over the top
      // of Pando means the way back is the browser's history, and for someone
      // who came to the launcher to open two apps it means coming back here
      // every time.
      {...({ href: app.address, target: '_blank', rel: 'noopener noreferrer' } as object)}
      style={{ textDecoration: 'none' }}
    >
      {body}
    </Card>
  );
}

function Quiet({ children }: { children: React.ReactNode }) {
  return (
    <p style={{ font: 'var(--type-body-ui)', color: 'var(--ink-secondary)' }}>{children}</p>
  );
}
