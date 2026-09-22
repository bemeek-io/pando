// What each part of an app is doing (R-221, R-261).
//
// "Degraded" is one word for a whole app, and an app can be a web service, a
// worker and a database it brought with it. crewmate reported degraded while
// its application container restarted every two seconds on a missing variable,
// and the only place that was visible was `docker ps` on the host — which is
// exactly the thing Pando exists so nobody has to do.
//
// Read from GET /apps/{id}/status, the same endpoint `pando app status` and
// pando_get_status use.

import { useQuery } from '@tanstack/react-query';
import { Button, StatusIndicator, Tag } from '@design';

import { api } from '@api/client';
import type { App } from '@api/types.gen';
import { Quiet, messageOf } from '../install/Accounts';
import { MEASURE } from '../ui/layout';
import { Table } from '../ui/Table';

export interface Part {
  name: string;
  primary: boolean;
  present: boolean;
  running: boolean;
  restarting: boolean;
  restart_count: number;
  healthy: boolean | null;
  exit_code?: number;
}

interface Status {
  state: string;
  desired_state: string;
  observability?: string;
  workloads?: Part[] | null;
}

export function useParts(app: App) {
  return useQuery({
    queryKey: ['apps', app.id, 'status'],
    queryFn: () => api.get<Status>(`/apps/${app.id}/status`),
    enabled: Boolean(app.pinned_spec_id),
    // A restarting workload changes state faster than anything else on this
    // screen, and a stale count is the thing that makes a crash loop look like
    // a single restart.
    refetchInterval: 5_000,
  });
}

export function Parts({ app, onLogs }: { app: App; onLogs?: (workload: string) => void }) {
  const status = useParts(app);
  const parts = status.data?.workloads ?? [];

  // One part is the app, and the app's own status line already says how it is.
  // Nothing while the status loads either, not a skeleton: most apps are one
  // part, and a placeholder for a section that then does not appear moves
  // everything under it twice.
  if (!app.pinned_spec_id || parts.length < 2) return null;

  return (
    <section style={{ maxWidth: MEASURE }}>
      <h4 style={{ font: 'var(--type-h4)', margin: '0 0 var(--space-2)' }}>Parts</h4>
      <Quiet>
        This app runs as more than one thing. Each has its own state and its own log.
      </Quiet>

      {status.data?.observability === 'unreachable' && (
        <Quiet>
          Pando couldn’t reach the runtime, so this is unknown rather than false.
        </Quiet>
      )}
      {status.isError && <Quiet>{messageOf(status.error)}</Quiet>}

      <div style={{ marginTop: 'var(--space-4)' }}>
        <Table
          columns={[
            {
              key: 'name',
              header: 'Part',
              width: 'minmax(0,24ch)',
              mono: true,
              render: (row: Part) => (
                <span style={{ display: 'inline-flex', alignItems: 'center', gap: 'var(--space-3)' }}>
                  {row.name}
                  {row.primary && <Tag>address</Tag>}
                </span>
              ),
            },
            {
              key: 'state',
              header: 'State',
              width: '22ch',
              render: (row: Part) => <StatusIndicator status={symbolFor(row)} label={labelFor(row)} />,
            },
            {
              key: 'healthy',
              header: 'Health',
              width: '16ch',
              muted: true,
              // R-221: no health check is not a failing one.
              render: (row: Part) =>
                row.healthy === null ? 'No check' : row.healthy ? 'Healthy' : 'Failing',
            },
            {
              key: 'restart_count',
              header: 'Restarts',
              width: '12ch',
              align: 'right',
              muted: true,
            },
            {
              key: 'logs',
              header: '',
              width: '12ch',
              align: 'right',
              render: (row: Part) =>
                onLogs && (
                  <Button variant="secondary" onClick={() => onLogs(row.name)}>
                    Logs
                  </Button>
                ),
            },
          ]}
          rows={parts}
        />
      </div>
    </section>
  );
}

/**
 * Restarting first.
 *
 * A container the runtime keeps restarting is up at almost every instant
 * anybody looks at it, so "running" is the reading that makes a crash-looping
 * app look fine — which is how this one went unnoticed.
 */
export function symbolFor(part: Part): 'running' | 'failed' | 'building' | 'stopped' {
  if (part.restarting) return 'building';
  if (part.running) return 'running';
  if (part.present) return 'failed';
  return 'stopped';
}

export function labelFor(part: Part): string {
  if (part.restarting) {
    return part.restart_count > 1 ? `Restarting, ${part.restart_count} times` : 'Restarting';
  }
  if (part.running) return 'Running';
  if (part.exit_code !== undefined && part.exit_code !== null) return `Exited (${part.exit_code})`;
  if (part.present) return 'Stopped';
  return 'Not running';
}
