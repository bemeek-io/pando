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

import { useEffect, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Badge, Banner, Button, EmptyState, Logo, SidebarNav, StatusIndicator, Table, Tabs, Tooltip } from '@design';
import type { SidebarItem } from '@design';

import { api } from '@api/client';
import type { App } from '@api/types.gen';
import { InstallVerb, useInstallVerb } from '../app/principal';
import { Accounts, messageOf } from '../install/Accounts';
import { Identity } from '../install/Identity';
import { Backups } from '../install/Backups';
import { Audit, Installation, Policy } from '../install/Installation';
import { statusLabel, statusSymbol } from '../ui/status';
import { DetectionReview } from './DetectionReview';
import { Sharing } from './Sharing';
import { AppOverview } from './AppOverview';
import { Logs } from './Logs';
import { Resources } from './Resources';
import { AddApp } from './AddApp';
import { Reference } from './Reference';
import { DeleteApp } from './DeleteApp';
import type { Route, Section } from '../app/route';
import { ThemeToggle } from '../ui/ThemeToggle';
import { relative } from '../ui/time';
import { ScoreBadge } from '../ui/ScoreBadge';
import { Terminal } from './Terminal';

export function AdminConsole({
  route,
  go,
  onLeave,
  administrative,
}: {
  route: Route;
  go: (next: Route, replace?: boolean) => void;
  onLeave: () => void;
  /** Whether this person administers anything. False for somebody who came
   *  here for the API screen, which is open to everyone. */
  administrative: boolean;
}) {
  // Where we are comes from the address bar, so a reload lands back here and a
  // link to an app is a link to an app.
  //
  // The app is identified by id rather than held as a value. It used to hold
  // the App object captured when the row was clicked, and that object never
  // changed again — so accepting a proposal left this screen rendering the app
  // as it was before, with no way to deploy.
  const section = route.section;
  const selectedID = route.appID ?? null;
  const setSection = (next: Section) => go({ view: 'admin', section: next });
  const setSelectedID = (id: string | null) =>
    go(id ? { view: 'admin', section: 'apps', appID: id } : { view: 'admin', section: 'apps' });

  // One question per screen. `install.view` is not a master key: an account can
  // hold install.audit.read and nothing else, and for them the console is the
  // audit log.
  const canView = useInstallVerb(InstallVerb.View);
  const canManageUsers = useInstallVerb(InstallVerb.UsersManage);
  const canManagePolicy = useInstallVerb(InstallVerb.PolicyManage);
  const canReadAudit = useInstallVerb(InstallVerb.AuditRead);
  const canManageBackups = useInstallVerb(InstallVerb.BackupManage);

  const apps = useQuery({
    queryKey: ['apps'],
    queryFn: () => api.get<{ apps: App[] | null }>('/apps'),
    enabled: administrative,
  });

  const rows = apps.data?.apps ?? [];

  // Apps only for somebody who administers one. A person who reached this
  // console for the API screen alone has no apps to manage, and a list of none
  // offering to add one they cannot create is a screen that answers 403.
  const items: SidebarItem[] = [];
  if (administrative) {
    items.push({ value: 'apps', label: 'Apps', trailing: <Badge count={rows.length} /> });
  }
  // Reading accounts needs install.view; changing one needs
  // install.users.manage. Either is a reason to see the screen, and the screen
  // itself is read-only without the second.
  if (canView || canManageUsers) items.push({ value: 'accounts', label: 'Accounts' });
  // Groups and roles are the same verb pair as accounts, and a separate screen:
  // who someone is and what a role can do are different questions, and one
  // screen answering both is how an authorization model turns into a list of
  // people with special powers (R-078).
  if (canView || canManageUsers) items.push({ value: 'identity', label: 'Groups and roles' });
  if (canView) items.push({ value: 'installation', label: 'Installation' });
  if (canView || canManagePolicy) items.push({ value: 'policy', label: 'Policy' });
  if (canManageBackups) items.push({ value: 'backups', label: 'Backups' });
  if (canReadAudit) items.push({ value: 'audit', label: 'Audit log' });

  // Last, and for everyone. The API is the product (R-261) and an agent holding
  // a token is an ordinary principal (R-262), so the manual and the way to mint
  // a token are not administration — a developer with one app shared with them
  // needs both.
  items.push({ value: 'api', label: 'API and tools' });

  return (
    <div style={{ display: 'flex', minHeight: '100vh', background: 'var(--paper)' }}>
      <SidebarNav
        value={section}
        // One navigation, not two. setSection already drops the selected app,
        // and calling both pushed two history entries — so one Back went to a
        // URL that looked identical and nothing appeared to happen.
        onChange={(v) => setSection(v as Section)}
        header={<Logo size={20} />}
        items={items}
        footer={
          <div style={{ display: 'flex', alignItems: 'center', gap: 'var(--space-2)' }}>
            <Button variant="ghost" onClick={onLeave}>
              Back to my apps
            </Button>
            <ThemeToggle />
          </div>
        }
      />

      <main style={{ flex: 1, minWidth: 0 }}>
        {section === 'accounts' && <Accounts />}
        {section === 'identity' && <Identity canEdit={canManageUsers} />}
        {section === 'installation' && <Installation />}
        {section === 'policy' && <Policy canEdit={canManagePolicy} />}
        {section === 'backups' && <Backups />}
        {section === 'audit' && <Audit />}
        {section === 'api' && <Reference />}
        {section === 'apps' &&
          (selectedID ? (
            <AppScreen
              appID={selectedID}
              listed={rows.find((a) => a.id === selectedID)}
              tab={route.tab}
              onTab={(tab) => go({ view: 'admin', section: 'apps', appID: selectedID, tab }, true)}
              onBack={() => setSelectedID(null)}
            />
          ) : (
            // Adding an app opens it. Detection is already running by the time
            // the request returns, and the next thing to do is look at what it
            // found — landing back on a list with a new row saying "draft"
            // leaves the person to work that out.
            <AppsList rows={rows} onOpen={(app) => setSelectedID(app.id)} onAdded={(app) => setSelectedID(app.id)} />
          ))}
      </main>
    </div>
  );
}

function AppsList({
  rows,
  onOpen,
  onAdded,
}: {
  rows: App[];
  onOpen: (app: App) => void;
  onAdded: (app: App) => void;
}) {
  const [adding, setAdding] = useState(false);

  // The page is not capped — a table's rows and rules run to the edge of the
  // window, which is what a wide display should look like. Its content is: the
  // columns are sized in `ch`, so Status and Updated sit next to the name
  // rather than two thousand pixels away from it.
  //
  // The action sits beside the heading rather than opposite it. Pushed to the
  // far end of a measure it is a long way from the word it belongs to, and on
  // a wide window the eye has to cross the whole page to find out what a screen
  // offers.
  return (
    <div>
      <header
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: 'var(--space-4)',
          padding: 'var(--space-6) var(--console-padding) var(--space-4)',
        }}
      >
        <h3 style={{ font: 'var(--type-h3)', margin: 0 }}>Apps</h3>
        <Button variant="primary" onClick={() => setAdding(true)}>
          Add app
        </Button>
      </header>
      <div style={{ padding: '0 var(--console-padding) var(--space-7)' }}>
        <Table
          onRowClick={onOpen}
          // A fresh install has no apps, and the list was a set of column
          // headers over nothing. R-002 is about the tenth app; the first one
          // is what makes the install anything at all.
          empty={
            <EmptyState
              heading="Add your first app"
              action={
                <Button variant="primary" onClick={() => setAdding(true)}>
                  Add app
                </Button>
              }
            >
              Point Pando at a repository and it works out how to build and run it.
            </EmptyState>
          }
          columns={[
            { key: 'name', header: 'Name', width: 'minmax(0,40ch)' },
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
              key: 'security_score',
              header: 'Security',
              width: '14ch',
              render: (row: App) => (
                <ScoreBadge score={row.security_score} verdict={row.security_verdict as never} full />
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

      {adding && (
        <AddApp
          onClose={() => setAdding(false)}
          onAdded={(app) => {
            setAdding(false);
            onAdded(app);
          }}
        />
      )}
    </div>
  );
}

function AppScreen({
  appID,
  listed,
  tab: routeTab,
  onTab,
  onBack,
}: {
  appID: string;
  /** The row from the list, which is all that is left when the app's own
   *  record will not load. */
  listed?: App;
  tab?: string;
  onTab: (tab: string) => void;
  onBack: () => void;
}) {
  // Read live, not handed down. Everything on this screen changes underneath
  // it: accepting a proposal pins a spec, deploying moves the app through
  // building to running. A snapshot taken when the row was clicked is wrong by
  // the time anything interesting has happened.
  const app = useQuery({
    queryKey: ['apps', appID],
    queryFn: () => api.get<App>(`/apps/${appID}`),
  });

  // An app with no pinned spec has never been through review, so detection is
  // the only thing worth showing it.
  const reviewed = Boolean(app.data?.pinned_spec_id);

  // The tab is in the address bar, so a reload comes back to it. Replace
  // rather than push when switching: flicking between tabs should not make the
  // back button walk them one at a time before leaving the app.
  const tab = routeTab ?? (reviewed ? 'overview' : 'detection');

  // Which section of a tab to open at, when something sent you there. A
  // warning about storage should land on storage, not on the top of a settings
  // tab with four sections above it. Cleared by any ordinary tab click, so it
  // only ever applies to the trip it was set for.
  const [focus, setFocus] = useState<string | undefined>(undefined);
  const setTab = (next: string, at?: string) => {
    setFocus(at);
    onTab(next);
  };

  // Accepting a proposal is the moment `reviewed` flips, and leaving somebody
  // on the setup tab afterwards hides the thing they came for — the deploy
  // button is on Overview. Only from 'detection', so a reviewed app whose
  // owner deliberately opened Configuration stays where they put themselves.
  //
  // `reviewed` alone in the dependency list, on purpose: this fires on the
  // transition, not on every change of tab. Including routeTab would move
  // somebody off Configuration the moment they opened it.
  useEffect(() => {
    if (reviewed && routeTab === 'detection') onTab('overview');
  }, [reviewed]);

  if (app.isPending) return null;
  if (app.isError || !app.data) {
    // An app whose record will not load is exactly the app somebody is trying
    // to get rid of, and this screen used to offer them a back button and
    // nothing else — no name, no reason, no way out but the list they came
    // from. The list's own row carries the name, so the delete still knows what
    // it is about.
    return (
      <div
        style={{
          display: 'flex',
          flexDirection: 'column',
          alignItems: 'flex-start',
          gap: 'var(--space-4)',
          padding: 'var(--space-6) var(--console-padding)',
        }}
      >
        <Button variant="ghost" onClick={onBack}>
          Apps
        </Button>
        {listed && <h3 style={{ font: 'var(--type-h3)', margin: 0 }}>{listed.name}</h3>}
        <Banner tone="failed">{messageOf(app.error)}</Banner>
        <DeleteApp appID={appID} appName={listed?.name ?? 'this app'} onDeleted={onBack} />
      </div>
    );
  }

  const tabs = reviewed
    ? [
        { value: 'overview', label: 'Overview' },
        { value: 'logs', label: 'Logs' },
        { value: 'sharing', label: 'Sharing' },
        { value: 'resources', label: 'Settings' },
        { value: 'terminal', label: 'Terminal' },
        { value: 'detection', label: 'Configuration' },
      ]
    : // One name, both states. It used to read "Set up" before a spec was
      // pinned and "Configuration" after, and somebody who finished setup went
      // looking for the tab they had just been using and concluded it had
      // disappeared. The design system's rule — an action keeps its name
      // through the whole flow — applies to the place you do it as well.
      [{ value: 'detection', label: 'Configuration' }];

  return (
    <div>
      <header
        style={{
          display: 'flex',
          flexDirection: 'column',
          alignItems: 'flex-start',
          gap: 'var(--space-3)',
          padding: 'var(--space-6) var(--console-padding) var(--space-4)',
        }}
      >
        <Button variant="ghost" onClick={onBack} style={{ alignSelf: 'flex-start' }}>
          Apps
        </Button>
        <div style={{ display: 'flex', alignItems: 'center', gap: 'var(--space-4)' }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: 'var(--space-3)' }}>
            <h3 style={{ font: 'var(--type-h3)', margin: 0 }}>{app.data.name}</h3>
            <StatusIndicator status={statusSymbol(app.data.state)} label={statusLabel(app.data.state)} />
          </div>
          {/* In the header rather than on Settings: an app whose source could
              not be fetched has no pinned spec and therefore no Settings tab,
              and that is the app most likely to be deleted. */}
          <DeleteApp appID={app.data.id} appName={app.data.name} onDeleted={onBack} />
        </div>
      </header>

      <div style={{ padding: '0 var(--console-padding)' }}>
        <Tabs value={tab} onChange={setTab} items={tabs} />
      </div>

      <div style={{ padding: 'var(--space-5) var(--console-padding) var(--space-7)' }}>
        {tab === 'detection' && <DetectionReview appID={app.data.id} reviewed={reviewed} />}
        {tab === 'sharing' && <Sharing appID={app.data.id} appName={app.data.name} />}
        {tab === 'overview' && <AppOverview app={app.data} onGo={setTab} />}
        {tab === 'logs' && <Logs app={app.data} />}
        {tab === 'resources' && <Resources appID={app.data.id} focus={focus} />}
        {tab === 'terminal' && <Terminal appID={app.data.id} />}
      </div>
    </div>
  );
}

