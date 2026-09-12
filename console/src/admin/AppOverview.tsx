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

export function AppOverview({ app }: { app: App }) {
  const queries = useQueryClient();

  const deployments = useQuery({
    queryKey: ['apps', app.id, 'deployments'],
    queryFn: () => api.get<{ deployments: Deployment[] | null }>(`/apps/${app.id}/deployments`),
    refetchInterval: app.state === 'deploying' ? 3_000 : false,
  });

  const spec = useQuery({
    queryKey: ['apps', app.id, 'spec', app.pinned_spec_id],
    queryFn: () => api.get<{ body: { warnings?: Array<{ code: string; message: string }> } }>(
      `/apps/${app.id}/specs/1`,
    ),
    enabled: Boolean(app.pinned_spec_id),
  });

  const deploy = useMutation({
    mutationFn: () => api.post(`/apps/${app.id}/deployments`, {}),
    onSuccess: () => queries.invalidateQueries({ queryKey: ['apps', app.id] }),
  });

  const latest = (deployments.data?.deployments ?? [])[0];
  const warnings = spec.data?.body?.warnings ?? [];

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
