// One app: what it is, where it is, and the deploy that put it there.
//
// The log of that deploy is here because this is the screen somebody watches
// while it happens. Every earlier deploy, and the app's own output, are on the
// Logs tab — both are histories, and a history on the screen you deploy from is
// a screen that scrolls forever.

import { useQuery } from '@tanstack/react-query';
import { Banner, Button, Card, StatusIndicator, Tag } from '@design';

import { api } from '@api/client';
import type { App, Deployment } from '@api/types.gen';
import { statusLabel, statusSymbol } from '../ui/status';
import { InlineWarning } from '../ui/InlineWarning';
import { Parts } from './Parts';
import { Usage } from './Usage';
import { useNarrow } from '../ui/narrow';
import { MEASURE } from '../ui/layout';
import { LineSkeleton } from '../ui/Loading';
import { relative } from '../ui/time';
import { deployLabel, deployStatus } from '../ui/deploys';
import { Security } from './Security';
import { AppImage } from './AppImage';
import { AppName } from './AppName';
import { AppVerb, useCan } from './verbs';

interface SpecRevision {
  id: string;
  revision: number;
  body?: {
    warnings?: Array<{ code: string; message: string }>;
    workloads?: Array<{ env?: Array<{ key: string; value?: string; secret_ref?: string; slot_ref?: string }> }>;
  };
}

export function AppOverview({
  app,
  onGo,
}: {
  app: App;
  /** Where a warning's fix lives: a tab, and the section on it. */
  onGo: (tab: string, focus?: string) => void;
}) {
  // A fix is offered only to somebody who can make it: "Add storage" in front
  // of a person who may only look at the app is a control that is refused.
  // Every fix on this screen is a spec edit.
  const canEdit = useCan(AppVerb.SpecEdit);
  const canReadLogs = useCan(AppVerb.LogsRead);
  const narrow = useNarrow();

  const deployments = useQuery({
    queryKey: ['apps', app.id, 'deployments'],
    queryFn: () => api.get<{ deployments: Deployment[] | null }>(`/apps/${app.id}/deployments`),
    refetchInterval: app.state === 'deploying' ? 3_000 : false,
  });

  // Every revision, so this screen can tell the pinned one from the newest.
  //
  // It used to fetch `/specs/1` — revision 1, hard-coded — so an app edited
  // even once showed the warnings of a spec it no longer ran.
  const specs = useQuery({
    queryKey: ['apps', app.id, 'specs'],
    queryFn: () =>
      api.get<{ revisions: SpecRevision[] | null; pinned_spec_id: string }>(`/apps/${app.id}/specs`),
    enabled: Boolean(app.pinned_spec_id),
  });

  const revisions = (specs.data?.revisions ?? []).slice().sort((a, b) => b.revision - a.revision);
  const newest = revisions[0];
  const pinned = revisions.find((r) => r.id === app.pinned_spec_id);

  // The list carries no bodies, so the warnings need the revision itself. This
  // is what `/specs/1` was reaching for before — correct for an app that had
  // never been edited, and wrong for every one that had.
  const pinnedSpec = useQuery({
    queryKey: ['apps', app.id, 'spec', pinned?.revision],
    queryFn: () => api.get<SpecRevision>(`/apps/${app.id}/specs/${pinned?.revision}`),
    enabled: Boolean(pinned),
  });

  // Editing anything — a slot, a volume, an environment variable, a deploy
  // setting — writes a new revision and leaves the pinned one alone, because
  // pinning is what the reconciler converges to and a dropdown should not
  // restart an app (R-152).
  //
  // Nothing then offered to deploy that revision, so every edit the console
  // could make was inert: the change was saved, and a deploy shipped the old
  // spec without saying so.
  const unshipped = Boolean(newest && pinned && newest.revision > pinned.revision);

  const latest = (deployments.data?.deployments ?? [])[0];
  const warnings = pinnedSpec.data?.body?.warnings ?? [];

  // Counted from the spec rather than carried in it. Detection reads the names
  // of an app's variables out of `.env.example` and cannot know the values, so
  // an accepted app arrives with a list of empty ones and nothing said about
  // it. A warning stored at detection would still be here after they were
  // filled; this one answers the question every time it is asked.
  const unset = (pinnedSpec.data?.body?.workloads ?? []).flatMap((w) =>
    (w.env ?? []).filter((e) => !e.secret_ref && !e.slot_ref && (e.value ?? '') === ''),
  );

  // Why an app is degraded, when it is made of several parts. The app's own
  // state is one word for all of them, and the part that is failing is the
  // thing somebody needs.
  const parts = <Parts app={app} onLogs={canReadLogs ? (workload) => onGo('logs', workload) : undefined} />;

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-5)' }}>
      {app.state === 'failed' && (
        // The one place red belongs on this screen. A banner, not stacked with
        // anything else — Banner's own rule is never to stack them.
        <Banner tone="failed">
          Pando has stopped trying to start this app. Fix what&rsquo;s wrong and deploy again.
        </Banner>
      )}

      {app.state === 'deploying' && (
        <Banner tone="building">
          Deploying. The app keeps serving its last good version until this finishes.
        </Banner>
      )}

      {unshipped && app.state !== 'deploying' && (
        <Banner tone="info">
          The configuration has changed since this app was last deployed. Deploy to apply it.
        </Banner>
      )}

      {/* On a wide screen, two columns: what the app is, and its parts, on the
          left at the card's width; what it is using on the right, running down
          beside both. On a phone everything follows the card, as the other
          sections do. */}
      <div style={{ display: 'flex', alignItems: 'flex-start', gap: 'var(--space-6)' }}>
        <div
          style={{
            maxWidth: MEASURE,
            flex: narrow ? '1 1 auto' : '1 1 0',
            minWidth: 0,
            display: 'flex',
            flexDirection: 'column',
            gap: 'var(--space-5)',
          }}
        >
          <Card padding="md">
            <Row label="Name">
              <AppName app={app} />
            </Row>
            <Row label="Status">
              <StatusIndicator status={statusSymbol(app.state)} label={statusLabel(app.state)} />
            </Row>
            <Row label="Address">
              {/* From the server, not assembled here: where an app is reached
                  follows its routing mode, and the console does not decide
                  routing (R-261). The old "/" + slug was the path-mode answer
                  shown for every app in every mode. */}
              {app.address ? (
                // Its own tab: an app is a different place from the console, and
                // opening it over the top leaves the browser's back button as
                // the only way back to what you were doing.
                <a href={app.address} target="_blank" rel="noopener noreferrer">
                  {app.address.replace(/^\/\//, '')}
                </a>
              ) : (
                <span style={{ color: 'var(--ink-secondary)' }}>
                  This app gets an address when it is first deployed.
                </span>
              )}
            </Row>
            <Row label="Launcher image">
              <AppImage app={app} />
            </Row>
            {app.source?.url && (
              <Row label="Repository">
                {app.source.url}
                {app.source.ref ? <> · <Tag mono>{app.source.ref}</Tag></> : null}
              </Row>
            )}
            {app.source?.commit && (
              <Row label="Commit">
                <span style={{ font: 'var(--type-code-sm)' }}>{app.source.commit.slice(0, 12)}</span>
              </Row>
            )}
            <Row label="Last deploy">
              {/* Not "Not deployed yet" while the list is on its way: that is a
                  claim about the app, and it would be wrong for most of them. */}
              {deployments.isPending ? (
                <LineSkeleton width="20ch" />
              ) : latest ? (
                <span style={{ display: 'inline-flex', alignItems: 'center', gap: 'var(--space-3)' }}>
                  <StatusIndicator
                    status={deployStatus(latest.status, latest.result_state)}
                    label={`${deployLabel(latest.status, latest.result_state)} ${relative(latest.finished_at ?? latest.started_at)}`}
                  />
                  {canReadLogs && (
                    <Button variant="secondary" onClick={() => onGo('logs')}>
                      Open logs
                    </Button>
                  )}
                </span>
              ) : (
                <span style={{ color: 'var(--ink-secondary)' }}>Not deployed yet.</span>
              )}
            </Row>
          </Card>
          {!narrow && parts}
        </div>
        {!narrow && (
          <div style={{ flex: '0 1 40ch', minWidth: '28ch' }}>
            <Usage app={app} layout="stack" />
          </div>
        )}
      </div>

      {narrow && parts}

      {/* What each part is using now, beside its limits (R-245). */}
      {narrow && <Usage app={app} />}

      {/* The security score, where the deploy log used to be — and across the
          measure rather than in the column that held it. A log is a column of
          short lines and a findings table is not: at 1.25fr, "Incorrect
          certificate validation during TLS session resumption" wrapped one
          word to a line and the section ran off the bottom of the page. */}
      <Security appID={app.id} />

      {unset.length > 0 && (
        <InlineWarning
          action={
            canEdit && (
              <Button
                variant="secondary"
                onClick={() => onGo('resources', 'variables')}
                style={{ alignSelf: 'flex-start' }}
              >
                Fill them in
              </Button>
            )
          }
        >
          {unsetMessage(unset.map((e) => e.key))}
        </InlineWarning>
      )}

      {/* Warnings inline where they apply, never stacked as banners, never
          looking like the failure above. */}
      {warnings.map((warning) => {
        const fix = FIXES[warning.code];
        return (
          <InlineWarning
            key={warning.code + warning.message}
            code={warning.code}
            action={
              fix && canEdit && (
                <Button
                  variant="secondary"
                  onClick={() => onGo(fix.tab, fix.focus)}
                  style={{ alignSelf: 'flex-start' }}
                >
                  {fix.label}
                </Button>
              )
            }
          >
            {warning.message}
          </InlineWarning>
        );
      })}

    </div>
  );
}

/**
 * What to say about variables nobody has filled in.
 *
 * Names first, because the person reading knows their own app: seeing
 * ANTHROPIC_API_KEY in the sentence is what makes it obvious which of them
 * matter. Four of them, then a count — a list of thirty is a wall.
 */
function unsetMessage(keys: string[]): string {
  const known = 'Pando read the names from this app’s own configuration and cannot know what they should be.';
  if (keys.length === 1) {
    return `${keys[0]} is declared with no value. ${known}`;
  }
  const shown = keys.slice(0, 4).join(', ');
  const rest = keys.length > 4 ? `, and ${keys.length - 4} more` : '';
  return `${keys.length} variables are declared with no value: ${shown}${rest}. ${known}`;
}

/**
 * Where each warning is fixed.
 *
 * R-201's persistence warning ends "define one here", and on this screen there
 * is no here — storage is defined on Settings. A warning a person cannot act on
 * from where they are reading it is how people learn to dismiss warnings, so
 * every code the console has a screen for carries the way to it. A code that is
 * not listed renders as before: text, with nothing claiming to be actionable.
 */
const FIXES: Record<string, { label: string; tab: string; focus?: string }> = {
  WARN_NO_PERSISTENT_VOLUME: { label: 'Add storage', tab: 'resources', focus: 'storage' },
  WARN_COMPOSE_CONSTRUCT_REWRITTEN: { label: 'Review the configuration', tab: 'detection' },
  WARN_UNDECLARED_DEPENDENCY_SUSPECTED: { label: 'Open dependencies', tab: 'resources' },
};

function Row({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div
      style={{
        display: 'flex',
        gap: 'var(--space-4)',
        padding: 'var(--space-2) 0',
        borderBottom: 'var(--border-width) solid var(--rule)',
      }}
    >
      <span
        style={{
          width: 'var(--space-8)',
          flex: '0 0 auto',
          font: 'var(--type-body-ui)',
          color: 'var(--ink-secondary)',
        }}
      >
        {label}
      </span>
      <span style={{ font: 'var(--type-body-ui)', color: 'var(--ink)', minWidth: 0 }}>
        {children}
      </span>
    </div>
  );
}

