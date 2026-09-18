// One app: what it is, where it is, and the deploy that put it there.
//
// The log of that deploy is here because this is the screen somebody watches
// while it happens. Every earlier deploy, and the app's own output, are on the
// Logs tab — both are histories, and a history on the screen you deploy from is
// a screen that scrolls forever.

import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { Banner, Button, Card, StatusIndicator, Tag } from '@design';

import { api, RequestFailed } from '@api/client';
import type { App, Deployment } from '@api/types.gen';
import { statusLabel, statusSymbol } from '../ui/status';
import { InlineWarning } from '../ui/InlineWarning';
import { MEASURE } from '../ui/layout';
import { relative } from '../ui/time';
import { DeploymentLog, deployLabel, deployStatus } from './Logs';
import { Security } from './Security';

interface SpecRevision {
  id: string;
  revision: number;
  body?: { warnings?: Array<{ code: string; message: string }> };
}

export function AppOverview({
  app,
  onGo,
}: {
  app: App;
  /** Where a warning's fix lives: a tab, and the section on it. */
  onGo: (tab: string, focus?: string) => void;
}) {
  const queries = useQueryClient();

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

  const deploy = useMutation({
    // The newest revision when there is an unshipped one, so Deploy ships what
    // the screen is showing. An empty body deploys the pinned spec, which is
    // right only when they are the same.
    mutationFn: () =>
      api.post(`/apps/${app.id}/deployments`, unshipped ? { spec_revision: newest?.revision } : {}),
    // ['apps'] so the list's status follows the app through building to
    // running, rather than only this screen.
    onSuccess: () => queries.invalidateQueries({ queryKey: ['apps'] }),
  });

  const latest = (deployments.data?.deployments ?? [])[0];
  const warnings = pinnedSpec.data?.body?.warnings ?? [];

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

      <div
        style={{
          display: 'grid',
          gridTemplateColumns: 'minmax(0,1fr) minmax(0,1.25fr)',
          gap: 'var(--space-6)',
          alignItems: 'start',
          maxWidth: MEASURE,
        }}
      >
        <Card padding="md">
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
        </Card>

        {latest ? (
          <DeploymentLog
            appID={app.id}
            deployment={latest}
            // A finished deploy whose output Pando no longer holds gets its
            // result, not an empty terminal. The log of every deploy, and the
            // app's own output, are one tab away.
            fallback={
              <Card padding="md">
                <div
                  style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-3)', alignItems: 'flex-start' }}
                >
                  <StatusIndicator
                    status={deployStatus(latest.status)}
                    label={`${deployLabel(latest.status)} ${relative(latest.finished_at ?? latest.started_at)}`}
                  />
                  <Button variant="ghost" onClick={() => onGo('logs')}>
                    Open logs
                  </Button>
                </div>
              </Card>
            }
          />
        ) : (
          <Card padding="md">
            <p style={{ font: 'var(--type-body-ui)', color: 'var(--ink-secondary)', margin: 0 }}>
              This app hasn&rsquo;t been deployed yet.
            </p>
          </Card>
        )}
      </div>

      {/* Warnings inline where they apply, never stacked as banners, never
          looking like the failure above. */}
      {warnings.map((warning) => {
        const fix = FIXES[warning.code];
        return (
          <InlineWarning
            key={warning.code + warning.message}
            code={warning.code}
            action={
              fix && (
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

      {/* The security score, with the app's other facts rather than three tabs
          away in settings: it is a property of what is running, and the person
          who has to act on it is the person looking at this page. */}
      <Security appID={app.id} />

      <div style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-2)' }}>
        <Button
          variant="primary"
          onClick={() => deploy.mutate()}
          disabled={deploy.isPending || app.state === 'deploying'}
          style={{ alignSelf: 'flex-start' }}
        >
          Deploy
        </Button>
        {deploy.isError && <Failure error={deploy.error} />}
      </div>
    </div>
  );
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

function Failure({ error }: { error: unknown }) {
  const failed = error instanceof RequestFailed ? error : null;
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-1)' }}>
      <p style={{ font: 'var(--type-body-ui)', color: 'var(--marker-deep)', margin: 0 }}>
        {failed?.message ?? 'Pando couldn’t start a deploy. Try again.'}
      </p>
      {failed?.remedy && (
        <p style={{ font: 'var(--type-caption)', color: 'var(--ink-secondary)', margin: 0 }}>
          {failed.remedy}
        </p>
      )}
    </div>
  );
}
