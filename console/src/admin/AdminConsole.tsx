// The management console.
//
// Reached only from the launcher, and only by someone holding an
// administrative verb (R-265). What it shows should be scoped to the verbs they
// hold — see the note in app/principal.ts for why that scoping cannot be
// implemented yet (O-17).

import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Badge, Button, Logo, SidebarNav, StatusIndicator, Table, Tabs, Tooltip } from '@design';

import { api } from '@api/client';
import type { App } from '@api/types.gen';
import { statusLabel, statusSymbol } from '../ui/status';
import { DetectionReview } from './DetectionReview';
import { Sharing } from './Sharing';
import { AppOverview } from './AppOverview';

export function AdminConsole({ onLeave }: { onLeave: () => void }) {
  const [selected, setSelected] = useState<App | null>(null);

  const apps = useQuery({
    queryKey: ['apps'],
    queryFn: () => api.get<{ apps: App[] | null }>('/apps'),
  });

  const rows = apps.data?.apps ?? [];

  return (
    <div style={{ display: 'flex', minHeight: '100vh', background: 'var(--paper)' }}>
      <SidebarNav
        value="apps"
        header={<Logo size={20} />}
        items={[{ value: 'apps', label: 'Apps', trailing: <Badge count={rows.length} /> }]}
        footer={
          <Button variant="ghost" onClick={onLeave}>
            Back to my apps
          </Button>
        }
      />

      <main style={{ flex: 1, minWidth: 0 }}>
        {selected ? (
          <AppScreen app={selected} onBack={() => setSelected(null)} />
        ) : (
          <AppsList rows={rows} onOpen={setSelected} />
        )}
      </main>
    </div>
  );
}

function AppsList({ rows, onOpen }: { rows: App[]; onOpen: (app: App) => void }) {
  return (
    <div style={{ maxWidth: 'var(--console-max)' }}>
      <header style={{ padding: 'var(--space-6) var(--console-padding) var(--space-4)' }}>
        <h3 style={{ font: 'var(--type-h3)', margin: 0 }}>Apps</h3>
      </header>
      <div style={{ padding: '0 var(--console-padding) var(--space-7)' }}>
        <Table
          onRowClick={onOpen}
          columns={[
            { key: 'name', header: 'Name', width: 'minmax(0,1.4fr)' },
            {
              key: 'state',
              header: 'Status',
              width: '16ch',
              render: (row: App) => (
                <StatusIndicator
                  status={statusSymbol(row.state)}
                  label={statusLabel(row.state)}
                />
              ),
            },
            {
              key: 'updated_at',
              header: 'Updated',
              width: '14ch',
              muted: true,
              render: (row: App) => (
                <Tooltip content={row.updated_at}>
                  <span>{relative(row.updated_at)}</span>
                </Tooltip>
              ),
            },
          ]}
          rows={rows}
        />
      </div>
    </div>
  );
}

function AppScreen({ app, onBack }: { app: App; onBack: () => void }) {
  // An app with no pinned spec has never been through review, so detection is
  // the only thing worth showing it.
  const reviewed = Boolean(app.pinned_spec_id);
  const [tab, setTab] = useState(reviewed ? 'overview' : 'detection');

  const tabs = reviewed
    ? [
        { value: 'overview', label: 'Overview' },
        { value: 'sharing', label: 'Sharing' },
        { value: 'detection', label: 'Configuration' },
      ]
    : [{ value: 'detection', label: 'Set up' }];

  return (
    <div style={{ maxWidth: 'var(--console-max)' }}>
      <header
        style={{
          display: 'flex',
          flexDirection: 'column',
          gap: 'var(--space-3)',
          padding: 'var(--space-6) var(--console-padding) var(--space-4)',
        }}
      >
        <Button variant="ghost" onClick={onBack} style={{ alignSelf: 'flex-start' }}>
          Apps
        </Button>
        <div style={{ display: 'flex', alignItems: 'center', gap: 'var(--space-3)' }}>
          <h3 style={{ font: 'var(--type-h3)', margin: 0 }}>{app.name}</h3>
          <StatusIndicator status={statusSymbol(app.state)} label={statusLabel(app.state)} />
        </div>
      </header>

      <div style={{ padding: '0 var(--console-padding)' }}>
        <Tabs value={tab} onChange={setTab} items={tabs} />
      </div>

      <div style={{ padding: 'var(--space-5) var(--console-padding) var(--space-7)' }}>
        {tab === 'detection' && <DetectionReview appID={app.id} />}
        {tab === 'sharing' && <Sharing appID={app.id} appName={app.name} />}
        {tab === 'overview' && <AppOverview app={app} />}
      </div>
    </div>
  );
}

/** Relative time in tables, with the exact value in a tooltip. */
function relative(iso: string): string {
  const then = new Date(iso).getTime();
  if (Number.isNaN(then)) return iso;

  const seconds = Math.round((Date.now() - then) / 1000);
  if (seconds < 45) return 'Just now';
  if (seconds < 90) return '1 min ago';
  if (seconds < 3600) return `${Math.round(seconds / 60)} min ago`;
  if (seconds < 7200) return '1 hour ago';
  if (seconds < 86400) return `${Math.round(seconds / 3600)} hours ago`;
  if (seconds < 172800) return 'Yesterday';
  return `${Math.round(seconds / 86400)} days ago`;
}
