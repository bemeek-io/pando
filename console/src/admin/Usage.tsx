// What each part of an app is using now: CPU, memory, disk and its volumes,
// beside the limits it runs under (R-245).
//
// A reading, refreshed while the page is open — not a history. Pando keeps no
// metrics and draws no graphs (R-016); what it can answer is "is this part
// near its limit right now", which is the question somebody looking at a slow
// or crashing app has.
//
// Read from GET /apps/{id}/usage, the same endpoint `pando app usage` and
// pando_get_usage use.

import { useQuery } from '@tanstack/react-query';
import { Tag } from '@design';

import { api } from '@api/client';
import type { App } from '@api/types.gen';
import { Quiet, messageOf } from '../install/Accounts';
import { MEASURE } from '../ui/layout';
import { Table } from '../ui/Table';
import { bytes, cores } from './usage-format';

interface VolumeUsage {
  id: string;
  name: string;
  path: string;
  bytes: number;
}

interface PartUsage {
  name: string;
  primary: boolean;
  running: boolean;
  cpu_millis: number;
  cpu_limit_millis: number;
  memory_bytes: number;
  memory_limit_bytes: number;
  disk_bytes: number;
  volumes: VolumeUsage[];
}

interface Reading {
  supported: boolean;
  reported_at?: string;
  workloads?: PartUsage[];
  host_cpu_millis?: number;
  host_memory_bytes?: number;
}

export function Usage({ app }: { app: App }) {
  const reading = useQuery({
    queryKey: ['apps', app.id, 'usage'],
    queryFn: () => api.get<Reading>(`/apps/${app.id}/usage`),
    enabled: Boolean(app.pinned_spec_id),
    // Each reading takes the runtime about a second to sample; every ten is
    // often enough to watch a part climb toward its limit.
    refetchInterval: 10_000,
  });

  if (!app.pinned_spec_id) return null;
  const data = reading.data;
  const parts = data?.workloads ?? [];

  return (
    <section style={{ maxWidth: MEASURE }}>
      <h4 style={{ font: 'var(--type-h4)', margin: '0 0 var(--space-2)' }}>In use</h4>
      {reading.isError && <Quiet>{messageOf(reading.error)}</Quiet>}
      {data && !data.supported && (
        <Quiet>This app&rsquo;s runtime does not report what its parts are using.</Quiet>
      )}

      {data?.supported && (
        <div style={{ marginTop: 'var(--space-3)' }}>
          <Table
            columns={[
              {
                key: 'name',
                header: 'Part',
                width: 'minmax(0,18ch)',
                mono: true,
                render: (row: PartUsage) => (
                  <span style={{ display: 'inline-flex', alignItems: 'center', gap: 'var(--space-3)' }}>
                    {row.name}
                    {row.primary && parts.length > 1 && <Tag>address</Tag>}
                  </span>
                ),
              },
              {
                key: 'cpu',
                header: 'CPU',
                width: 'minmax(0,1fr)',
                render: (row: PartUsage) =>
                  row.running ? (
                    <Meter
                      used={row.cpu_millis}
                      limit={row.cpu_limit_millis}
                      host={data.host_cpu_millis}
                      format={cores}
                    />
                  ) : (
                    <Stopped />
                  ),
              },
              {
                key: 'memory',
                header: 'Memory',
                width: 'minmax(0,1fr)',
                render: (row: PartUsage) =>
                  row.running ? (
                    <Meter
                      used={row.memory_bytes}
                      limit={row.memory_limit_bytes}
                      host={data.host_memory_bytes}
                      format={bytes}
                    />
                  ) : (
                    <Stopped />
                  ),
              },
              {
                key: 'disk',
                header: 'Disk',
                width: 'minmax(0,1fr)',
                // The part's own layer, then each volume it mounts: the second
                // is where an app's data is, and usually the number that grows.
                render: (row: PartUsage) => (
                  <div style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-1)', padding: 'var(--space-2) 0' }}>
                    <span>{row.disk_bytes < 0 ? '—' : bytes(row.disk_bytes)}</span>
                    {row.volumes.map((v) => (
                      <span key={v.id} style={{ font: 'var(--type-caption)', color: 'var(--ink-secondary)' }}>
                        {v.name || v.id} at {v.path}: {v.bytes < 0 ? 'size unknown' : bytes(v.bytes)}
                      </span>
                    ))}
                  </div>
                ),
              },
            ]}
            rows={parts.map((p) => ({ ...p, id: p.name }))}
          />
          {data.reported_at && (
            <p style={{ font: 'var(--type-caption)', color: 'var(--ink-secondary)', margin: 'var(--space-2) 0 0' }}>
              As of {new Date(data.reported_at).toLocaleTimeString()}. Refreshes every ten seconds. Disk is what a part
              wrote outside its storage.
            </p>
          )}
        </div>
      )}
    </section>
  );
}

/**
 * A number beside its limit, with a bar. With no limit, the part may use
 * what the host has, so the host is the scale — and says so, rather than
 * drawing a bar against a limit that is not there.
 */
function Meter({
  used,
  limit,
  host,
  format,
}: {
  used: number;
  limit: number;
  host?: number;
  format: (n: number) => string;
}) {
  const scale = limit > 0 ? limit : host && host > 0 ? host : 0;
  const share = scale > 0 ? Math.min(1, used / scale) : 0;
  // Red only near a real limit: that is where a part is about to be throttled
  // or killed. Near the host's total is a different conversation.
  const near = limit > 0 && share >= 0.9;
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-1)', padding: 'var(--space-2) 0' }}>
      <span style={{ font: 'var(--type-body-ui)' }}>
        {format(used)}
        <span style={{ color: 'var(--ink-secondary)' }}>
          {limit > 0 ? ` of ${format(limit)}` : host && host > 0 ? ` of ${format(host)} on the host` : ''}
        </span>
      </span>
      {scale > 0 && (
        <div
          role="meter"
          aria-valuemin={0}
          aria-valuemax={scale}
          aria-valuenow={used}
          style={{ height: 'var(--space-1)', background: 'var(--paper-sunken)', borderRadius: 'var(--radius-xs)', overflow: 'hidden', maxWidth: '24ch' }}
        >
          <div
            style={{
              width: `${share * 100}%`,
              height: '100%',
              background: near ? 'var(--marker)' : 'var(--ink-secondary)',
              transition: 'width var(--dur-fast) var(--ease)',
            }}
          />
        </div>
      )}
    </div>
  );
}

function Stopped() {
  return <span style={{ color: 'var(--ink-secondary)' }}>Not running</span>;
}
