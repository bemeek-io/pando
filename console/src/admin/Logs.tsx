// Logs (R-170, R-222).
//
// "Logs" means two different things to the person looking for them, and the
// console used to show neither properly: what the app has printed while
// running, and what a deploy printed while it was building. The first had no
// screen at all — `GET /apps/{id}/logs` existed and nothing called it — and the
// second was one unbounded box on Overview showing the newest deploy and no
// way back to the one before it.
//
// Both are bounded and scroll. A deploy log runs to thousands of lines, and a
// page that grows to that length pushes everything else on the screen out of
// reach and leaves no way back up but the scrollbar.

import { useEffect, useMemo, useRef, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Button, CodeBlock, StatusIndicator, Table } from '@design';

import { api, base } from '@api/client';
import type { App, Deployment } from '@api/types.gen';
import { Quiet, messageOf } from '../install/Accounts';
import { MEASURE } from '../ui/layout';

/**
 * A log, at a height that leaves the rest of the page reachable.
 *
 * It follows the tail while new lines arrive, and stops following the moment
 * the reader scrolls up — a log that yanks itself back to the bottom while
 * somebody is reading the failure three screens above is unreadable.
 */
export function LogBox({
  title,
  lines,
  empty,
}: {
  title: string;
  lines: string[];
  empty?: React.ReactNode;
}) {
  const box = useRef<HTMLDivElement>(null);
  const [follow, setFollow] = useState(true);

  useEffect(() => {
    const el = box.current;
    if (!el || !follow) return;
    el.scrollTop = el.scrollHeight;
  }, [lines, follow]);

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-2)' }}>
      <div
        ref={box}
        // Focusable, so the keyboard can scroll it: a log is read, and Page
        // Down is how a long one is read.
        tabIndex={0}
        onScroll={() => {
          const el = box.current;
          if (!el) return;
          // A few pixels of slack: a smooth scroll lands a fraction short of
          // the bottom, and an exact comparison would read that as "the reader
          // scrolled up" on every new line.
          setFollow(el.scrollHeight - el.scrollTop - el.clientHeight < 24);
        }}
        style={{
          maxHeight: '60vh',
          overflowY: 'auto',
          // The wheel stops here rather than carrying on down the page once
          // the log reaches its end.
          overscrollBehavior: 'contain',
          borderRadius: 'var(--radius-md)',
          border: 'var(--border-width) solid var(--rule-strong)',
          background: 'var(--terminal)',
        }}
      >
        <CodeBlock
          title={title}
          lines={lines}
          style={{ border: 'none', borderRadius: 0, overflow: 'visible' }}
        />
      </div>
      {lines.length === 0 && empty}
      {!follow && (
        <Button variant="ghost" onClick={() => setFollow(true)} style={{ alignSelf: 'flex-start' }}>
          Follow the end
        </Button>
      )}
    </div>
  );
}

/**
 * One deployment's build output.
 *
 * Streamed over EventSource rather than polled, because this is the one place
 * where somebody is watching a thing happen and latency is the experience.
 */
export function DeploymentLog({
  appID,
  deployment,
  fallback,
}: {
  appID: string;
  deployment: Deployment;
  /** Shown instead of the box when the stream ends with nothing in it. */
  fallback?: React.ReactNode;
}) {
  const [lines, setLines] = useState<string[]>([]);
  const [ended, setEnded] = useState(false);

  useEffect(() => {
    setLines([]);
    setEnded(false);

    const stream = new EventSource(`${base}/apps/${appID}/deployments/${deployment.id}/logs`);

    let silent: number | undefined;
    const done = () => {
      setEnded(true);
      stream.close();
      window.clearTimeout(silent);
    };

    stream.onmessage = (event) => {
      window.clearTimeout(silent);
      setLines((previous) => [...previous, event.data as string]);
    };
    stream.addEventListener('end', done);
    stream.onerror = done;

    // A deploy whose output Pando no longer holds never ends: the server's log
    // store has no stream for that ID, opens an empty one, and holds the
    // connection waiting for lines that will never come. Without this the box
    // stays black and empty forever — which is the bug this whole fallback
    // exists for, and the version that only listened for `end` did not catch
    // it. A finished deploy that has said nothing for four seconds has nothing
    // to say.
    if (deployment.status !== 'running') {
      silent = window.setTimeout(done, 4_000);
    }

    return () => {
      window.clearTimeout(silent);
      stream.close();
    };
  }, [appID, deployment.id, deployment.status]);

  // Deploy output lives in memory (`deploy.LogStore`) and is written nowhere,
  // so a deploy from before the last restart has none. An empty black box is
  // the worst way to say that — it reads as a deploy that printed nothing —
  // so the box is not drawn at all.
  if (ended && lines.length === 0) {
    return (
      fallback ?? (
        <Quiet>
          Pando has no output for this deploy. Deploy logs are held in memory and go when Pando
          restarts.
        </Quiet>
      )
    );
  }

  return <LogBox title={`Deploy log · ${deployLabel(deployment.status)}`} lines={lines} />;
}

/** The app's own output, from the runtime, and the deploys before this one. */
export function Logs({ app }: { app: App }) {
  const deployments = useQuery({
    queryKey: ['apps', app.id, 'deployments'],
    queryFn: () => api.get<{ deployments: Deployment[] | null }>(`/apps/${app.id}/deployments`),
    refetchInterval: app.state === 'deploying' ? 3_000 : false,
  });

  const rows = deployments.data?.deployments ?? [];
  const [selected, setSelected] = useState<string | null>(null);
  const shown = rows.find((d) => d.id === selected) ?? rows[0];

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-7)' }}>
      <AppOutput app={app} />

      <section style={{ maxWidth: MEASURE }}>
        <h4 style={{ font: 'var(--type-h4)', margin: '0 0 var(--space-2)' }}>Deploys</h4>

        <div style={{ marginTop: 'var(--space-4)' }}>
          <Table
            onRowClick={(row: Deployment) => setSelected(row.id)}
            columns={[
              {
                key: 'started_at',
                header: 'Started',
                width: 'minmax(0,26ch)',
                render: (row: Deployment) => new Date(row.started_at).toLocaleString(),
              },
              {
                key: 'status',
                header: 'Result',
                width: '18ch',
                render: (row: Deployment) => (
                  <StatusIndicator status={deployStatus(row.status)} label={deployLabel(row.status)} />
                ),
              },
              { key: 'trigger', header: 'Started by', width: '18ch', muted: true },
              {
                key: 'id',
                header: 'Reference',
                width: 'minmax(0,22ch)',
                mono: true,
                muted: true,
              },
            ]}
            rows={rows}
            empty={<Quiet>This app hasn’t been deployed yet.</Quiet>}
          />
        </div>

        {shown && (
          <div style={{ marginTop: 'var(--space-5)' }}>
            <DeploymentLog appID={app.id} deployment={shown} />
          </div>
        )}
      </section>
    </div>
  );
}

const TAIL = 500;

function AppOutput({ app }: { app: App }) {
  // Only an app that has been deployed has a runtime to ask. The endpoint says
  // so itself — it refuses an app with no pinned spec — and asking anyway would
  // put an error on the screen where the answer is "not yet".
  const enabled = Boolean(app.pinned_spec_id);

  const output = useQuery({
    queryKey: ['apps', app.id, 'output'],
    queryFn: () => api.text(`/apps/${app.id}/logs?tail=${TAIL}`),
    enabled,
    // The runtime is asked once and re-asked on demand, except while the app is
    // running, where a log nobody has to refresh is the point of having one.
    refetchInterval: app.state === 'running' ? 5_000 : false,
  });

  // A trailing newline is one empty line, not one line of nothing: split it off
  // so an app that has printed nothing reads as nothing rather than as a black
  // box with a blank line in it.
  //
  // Memoized because the box follows the end of the log while `lines` changes,
  // and a fresh array on every render is a change on every render — it pulled
  // itself back to the bottom five seconds after the reader scrolled up.
  const lines = useMemo(() => {
    const text = (output.data ?? '').replace(/\n$/, '');
    return text ? text.split('\n') : [];
  }, [output.data]);

  return (
    <section style={{ maxWidth: MEASURE }}>
      <div style={{ display: 'flex', alignItems: 'baseline', justifyContent: 'space-between' }}>
        <h4 style={{ font: 'var(--type-h4)', margin: '0 0 var(--space-2)' }}>Output</h4>
        <Button variant="ghost" disabled={!enabled} onClick={() => void output.refetch()}>
          Refresh
        </Button>
      </div>

      <div style={{ marginTop: 'var(--space-4)' }}>
        {!enabled ? (
          <Quiet>This app hasn’t been deployed yet, so it hasn’t printed anything.</Quiet>
        ) : output.isError ? (
          <Quiet>{messageOf(output.error)}</Quiet>
        ) : (
          <LogBox
            title={app.name}
            lines={lines}
            empty={<Quiet>This app hasn’t printed anything.</Quiet>}
          />
        )}
      </div>
    </section>
  );
}

/** Sentence case, as everything in the console is. */
export function deployLabel(status: string): string {
  switch (status) {
    case 'succeeded':
      return 'Deployed';
    case 'failed':
      return 'Failed';
    case 'rolled_back':
      return 'Rolled back';
    case 'running':
      return 'Deploying';
    default:
      return status.charAt(0).toUpperCase() + status.slice(1).replace(/_/g, ' ');
  }
}

/** Deploy statuses onto the design system's symbols. */
export function deployStatus(status: string): 'running' | 'building' | 'failed' | 'stopped' {
  switch (status) {
    case 'succeeded':
      return 'running';
    case 'failed':
      return 'failed';
    case 'rolled_back':
      return 'stopped';
    default:
      return 'building';
  }
}
