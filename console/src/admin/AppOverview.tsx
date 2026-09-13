// One app: what it is, where it is, and its build log.
//
// The log streams over EventSource rather than polling, because a deploy log is
// the one place where a user is watching a thing happen and latency is the
// whole experience.

import { useEffect, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Banner, Button, Card, CodeBlock, StatusIndicator, Tag } from '@design';

import { api, RequestFailed } from '@api/client';
import type { App, Deployment } from '@api/types.gen';
import { statusLabel, statusSymbol } from '../ui/status';
import { InlineWarning } from '../ui/InlineWarning';

interface SpecRevision {
  id: string;
  revision: number;
  body?: { warnings?: Array<{ code: string; message: string }> };
}

export function AppOverview({ app }: { app: App }) {
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
        }}
      >
        <Card padding="md">
          <Row label="Status">
            <StatusIndicator status={statusSymbol(app.state)} label={statusLabel(app.state)} />
          </Row>
          <Row label="Address">
            <a href={`/${app.slug}/`}>/{app.slug}/</a>
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
          <DeploymentLog appID={app.id} deployment={latest} />
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
      {warnings.map((warning) => (
        <InlineWarning key={warning.code + warning.message} code={warning.code}>
          {warning.message}
        </InlineWarning>
      ))}

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

function DeploymentLog({ appID, deployment }: { appID: string; deployment: Deployment }) {
  const [lines, setLines] = useState<string[]>([]);

  useEffect(() => {
    // EventSource, not polling: the server already streams this and a deploy
    // log is watched while it happens.
    const stream = new EventSource(`/api/v1/apps/${appID}/deployments/${deployment.id}/logs`);

    stream.onmessage = (event) => setLines((previous) => [...previous, event.data as string]);
    stream.addEventListener('end', () => stream.close());
    stream.onerror = () => stream.close();

    return () => stream.close();
  }, [appID, deployment.id]);

  return <CodeBlock title={`Deploy log · ${deployment.status}`} lines={lines} />;
}

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
