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
//
// R-341: favorites are the same list, split in two. There is no second
// request, so the two sections cannot disagree about what you can open.

import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { EmptyState, Icon, IconButton, Logo } from '@design';

import { api, base } from '@api/client';
import type { App } from '@api/types.gen';
import { statusLabel } from '../ui/status';
import { Sheet } from '../ui/Sheet';
import { TopoBackground, TopoTile } from '../ui/TopoBackground';

type MyApps = { apps: App[] | null };

export function Launcher({
  onAdmin,
  onSettings,
}: {
  onAdmin?: () => void;
  /** Theme and signing out, which everyone can reach. */
  onSettings: () => void;
}) {
  const queries = useQueryClient();
  const apps = useQuery({
    queryKey: ['me', 'apps'],
    queryFn: () => api.get<MyApps>('/me/apps'),
  });

  // Optimistic: the star and the section move the moment it is clicked, and
  // move back if the server refuses. A pin that takes a round trip to show is
  // a pin people click twice.
  const favorite = useMutation({
    mutationFn: ({ app, on }: { app: App; on: boolean }) =>
      on ? api.put<void>(`/me/favorites/${app.id}`) : api.del<void>(`/me/favorites/${app.id}`),
    onMutate: async ({ app, on }) => {
      await queries.cancelQueries({ queryKey: ['me', 'apps'] });
      const before = queries.getQueryData<MyApps>(['me', 'apps']);
      queries.setQueryData<MyApps>(['me', 'apps'], (old) => ({
        apps: (old?.apps ?? []).map((a) => (a.id === app.id ? { ...a, favorite: on } : a)),
      }));
      return { before };
    },
    onError: (_err, _vars, context) => {
      if (context?.before) queries.setQueryData(['me', 'apps'], context.before);
    },
    onSettled: () => void queries.invalidateQueries({ queryKey: ['me', 'apps'] }),
  });
  const toggle = (app: App) => favorite.mutate({ app, on: !app.favorite });

  const all = apps.data?.apps ?? [];
  const pinned = all.filter((a) => a.favorite);
  const rest = all.filter((a) => !a.favorite);

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
        {/* Above the rest, and only when there is something in it: an empty
            "Favorites" heading on every launcher would be a section explaining
            a feature rather than showing anything. */}
        {pinned.length > 0 && (
          <Sheet heading="Favorites">
            <Grid apps={pinned} onToggle={toggle} />
          </Sheet>
        )}

        {/* Hidden when every app is a favorite, rather than saying there is
            nothing shared with you directly under the apps that were. */}
        {(rest.length > 0 || pinned.length === 0) && (
          <Sheet heading="Your apps">
            {apps.isPending && <Quiet>Loading your apps.</Quiet>}

            {apps.isError && (
              <Quiet>Pando couldn&rsquo;t load your apps. Reload the page to try again.</Quiet>
            )}

            {apps.data &&
              (rest.length === 0 ? (
                // Not "you have no apps" — nothing has gone wrong, and an empty
                // launcher is the normal state for someone who has just been
                // given an account.
                <EmptyState heading="Nothing shared with you yet">
                  When someone shares an app with you, it shows up here.
                </EmptyState>
              ) : (
                <Grid apps={rest} onToggle={toggle} />
              ))}
          </Sheet>
        )}
      </main>
    </div>
  );
}

function Grid({ apps, onToggle }: { apps: App[]; onToggle: (app: App) => void }) {
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
        <Tile key={app.id} app={app} onToggle={() => onToggle(app)} />
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

function Tile({ app, onToggle }: { app: App; onToggle: () => void }) {
  // A square and a name, and no status line. The launcher is for someone who
  // came to open an app (R-005), and "running" is the normal case — a word on
  // every tile saying so is noise. What they need to know is which tiles will
  // not open, and a greyed-out tile says that without a word.
  //
  // The state is still in the accessible name and the hover title, so the
  // difference is never carried by appearance alone.
  const open = reachable(app);
  const label = open ? app.name : `${app.name} — ${statusLabel(app.state)}`;

  // The star shows on a favorite always, and on anything else when the tile is
  // pointed at or tabbed into — twenty outlined stars at rest would be twenty
  // things to look past.
  const [near, setNear] = useState(false);
  const favorite = Boolean(app.favorite);

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

  return (
    // The star is a sibling of the link, not inside it: a button in an anchor
    // is two controls in one, and a click on the star would open the app.
    <div
      style={{ position: 'relative' }}
      onMouseEnter={() => setNear(true)}
      onMouseLeave={() => setNear(false)}
      onFocus={() => setNear(true)}
      onBlur={() => setNear(false)}
    >
      {open ? (
        // An app is a different place from the console. Opening it over the
        // top of Pando means the way back is the browser's history, and for
        // someone who came to the launcher to open two apps it means coming
        // back here every time.
        //
        // Every one of these addresses goes through Pando's proxy — there is
        // no other way in (R-023) — but *which* address depends on how the app
        // is routed, and the server is what knows.
        <a
          href={app.address}
          target="_blank"
          rel="noopener noreferrer"
          aria-label={label}
          style={{ textDecoration: 'none', color: 'inherit', display: 'block' }}
        >
          {body}
        </a>
      ) : (
        <div title={statusLabel(app.state)} aria-label={label} aria-disabled="true">
          {body}
        </div>
      )}

      {(favorite || near) && (
        <div style={{ position: 'absolute', top: 'var(--space-2)', right: 'var(--space-2)' }}>
          <IconButton
            variant="secondary"
            label={favorite ? `Remove ${app.name} from favorites` : `Add ${app.name} to favorites`}
            aria-pressed={favorite}
            onClick={onToggle}
          >
            <Icon name={favorite ? 'star-filled' : 'star'} size={16} color="var(--ink)" />
          </IconButton>
        </div>
      )}
    </div>
  );
}

/**
 * The tile's picture: the app's image when it has one (R-340), and a patch of
 * terrain generated from its ID when it does not.
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
        overflow: 'hidden',
      }}
    >
      {src ? (
        <img src={src} alt="" style={{ width: '100%', height: '100%', objectFit: 'cover', display: 'block' }} />
      ) : (
        <TopoTile seed={app.id} />
      )}
    </div>
  );
}

function Quiet({ children }: { children: React.ReactNode }) {
  return (
    <p style={{ font: 'var(--type-body-ui)', color: 'var(--ink-secondary)' }}>{children}</p>
  );
}
