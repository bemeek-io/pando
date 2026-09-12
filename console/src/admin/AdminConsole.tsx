// The management console.
//
// Reached only from the launcher, and only by someone holding an administrative
// verb (R-265). Everything in here is app administration, scoped by the server:
// `GET /apps` is control-plane scoped and returns the apps this person may
// manage and no others.
//
// Install-level administration — accounts, host policy, the installation's
// adapters and capacity, the audit log — sits beside Apps in the sidebar, each
// item shown on the verb it needs and not on "is an administrator". There is no
// implication graph between verbs (R-082), so a sidebar that assumed one would
// offer a screen whose every request comes back 403.

import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Badge, Button, Logo, SidebarNav, StatusIndicator, Table, Tabs, Tooltip } from '@design';
import type { SidebarItem } from '@design';

import { api } from '@api/client';
import type { App } from '@api/types.gen';
import { InstallVerb, useInstallVerb } from '../app/principal';
import { Accounts } from '../install/Accounts';
import { Audit, Installation, Policy } from '../install/Installation';
import { statusLabel, statusSymbol } from '../ui/status';
import { DetectionReview } from './DetectionReview';
import { Sharing } from './Sharing';
import { AppOverview } from './AppOverview';
import { Terminal } from './Terminal';

type Section = 'apps' | 'accounts' | 'installation' | 'policy' | 'audit';

export function AdminConsole({ onLeave }: { onLeave: () => void }) {
  const [selected, setSelected] = useState<App | null>(null);
  const [section, setSection] = useState<Section>('apps');

  // One question per screen. `install.view` is not a master key: an account can
  // hold install.audit.read and nothing else, and for them the console is the
  // audit log.
  const canView = useInstallVerb(InstallVerb.View);
  const canManageUsers = useInstallVerb(InstallVerb.UsersManage);
  const canManagePolicy = useInstallVerb(InstallVerb.PolicyManage);
  const canReadAudit = useInstallVerb(InstallVerb.AuditRead);

  const apps = useQuery({
    queryKey: ['apps'],
    queryFn: () => api.get<{ apps: App[] | null }>('/apps'),
  });

  const rows = apps.data?.apps ?? [];

  const items: SidebarItem[] = [
    { value: 'apps', label: 'Apps', trailing: <Badge count={rows.length} /> },
  ];
  // Reading accounts needs install.view; changing one needs
  // install.users.manage. Either is a reason to see the screen, and the screen
  // itself is read-only without the second.
  if (canView || canManageUsers) items.push({ value: 'accounts', label: 'Accounts' });
  if (canView) items.push({ value: 'installation', label: 'Installation' });
  if (canView || canManagePolicy) items.push({ value: 'policy', label: 'Policy' });
  if (canReadAudit) items.push({ value: 'audit', label: 'Audit log' });

  return (
    <div style={{ display: 'flex', minHeight: '100vh', background: 'var(--paper)' }}>
      <SidebarNav
        value={section}
        onChange={(v) => {
          setSection(v as Section);
          setSelected(null);
        }}
        header={<Logo size={20} />}
        items={items}
        footer={
          <Button variant="ghost" onClick={onLeave}>
            Back to my apps
          </Button>
        }
      />

      <main style={{ flex: 1, minWidth: 0 }}>
        {section === 'accounts' && <Accounts />}
        {section === 'installation' && <Installation />}
        {section === 'policy' && <Policy canEdit={canManagePolicy} />}
        {section === 'audit' && <Audit />}
        {section === 'apps' &&
          (selected ? (
            <AppScreen app={selected} onBack={() => setSelected(null)} />
          ) : (
            <AppsList rows={rows} onOpen={setSelected} />
          ))}
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
        { value: 'terminal', label: 'Terminal' },
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
        {tab === 'terminal' && <Terminal appID={app.id} />}
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
