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
import { DeployButton } from './DeployButton';
import { Lifecycle } from './Lifecycle';
import type { Route, Section } from '../app/route';
import { ThemeToggle } from '../ui/ThemeToggle';
import { MEASURE } from '../ui/layout';
import { relative } from '../ui/time';
import { ScoreBadge } from '../ui/ScoreBadge';
import { Terminal } from './Terminal';
import { Sheet } from '../ui/Sheet';
import { TopoBackground } from '../ui/TopoBackground';

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

  // The same query key the accounts screen uses, so the sidebar's count and
  // that screen's table are one request and cannot disagree. Only asked for by
  // somebody who may read it — the endpoint refuses the rest, and a sidebar
  // that fires a 403 on every load is a sidebar that fills the log.
  const accounts = useQuery({
    queryKey: ['users'],
    queryFn: () => api.get<{ users: unknown[] | null }>('/users'),
    enabled: canView || canManageUsers,
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
  if (canView || canManageUsers) {
    items.push({
      value: 'accounts',
      label: 'Accounts',
      trailing: <Badge count={accounts.data?.users?.length ?? 0} />,
    });
  }
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
    // isolation makes this the stacking context, so the terrain's negative
    // z-index puts it above the paper and below everything else.
    <div
      style={{
        display: 'flex',
        minHeight: '100vh',
        background: 'var(--paper)',
        position: 'relative',
        isolation: 'isolate',
      }}
    >
      {/* A different map per screen; the tabs of one app share its map. */}
      <TopoBackground seed={`${section}/${selectedID ?? ''}`} />
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
  // offers. Sheet does both, and frames the page as a survey sheet.
  return (
    <>
      <Sheet
        heading="Apps"
        action={
          <Button variant="primary" onClick={() => setAdding(true)}>
            Add app
          </Button>
        }
      >
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
      </Sheet>

      {adding && (
        <AddApp
          onClose={() => setAdding(false)}
          onAdded={(app) => {
            setAdding(false);
            onAdded(app);
          }}
        />
      )}
    </>
  );
}

/**
 * Where each plan-time refusal is fixed.
 *
 * The same idea as the warnings on the overview: an error that says "choose how
 * to fill the DB_URL slot" on a screen with no slots on it is an error somebody
 * has to go looking for the answer to. A code with no entry keeps the plain
 * dismissal — nothing here claims to be actionable when it is not.
 */
const REFUSALS: Record<string, { label: string; tab: string; focus?: string }> = {
  PLAN_SLOT_UNFILLED: { label: 'Fill it in', tab: 'resources', focus: 'dependencies' },
  PLAN_CAPABILITY_UNSUPPORTED: { label: 'Open settings', tab: 'resources' },
  PLAN_SECURITY_BELOW_THRESHOLD: { label: 'See the findings', tab: 'overview' },
  VALID_PRIMARY_WORKLOAD: { label: 'Open configuration', tab: 'detection' },
  VALID_DANGLING_MOUNT: { label: 'Open storage', tab: 'resources', focus: 'storage' },
  VALID_DANGLING_SLOT_REF: { label: 'Open dependencies', tab: 'resources', focus: 'dependencies' },
};

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

  // A refused deploy, rendered under the header rather than inside it: the
  // message is a sentence or three, and a paragraph in a row of buttons moves
  // the buttons.
  const [refusal, setRefusal] = useState<{ message: string; remedy?: string; code?: string } | null>(
    null,
  );
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
    //
    // Not the 404 contour figure. That figure means "the thing you came for is
    // not here", and this app is in the list — its record failed to load, and
    // the server's own message says why (R-105). A guessed "may have been
    // deleted" would be wrong about the one app it is shown for.
    return (
      <Sheet>
        <div
          style={{
            display: 'flex',
            flexDirection: 'column',
            alignItems: 'flex-start',
            gap: 'var(--space-4)',
          }}
        >
          <Button variant="ghost" onClick={onBack}>
            Apps
          </Button>
          {listed && <h3 style={{ font: 'var(--type-h3)', margin: 0 }}>{listed.name}</h3>}
          <Banner tone="failed">{messageOf(app.error)}</Banner>
          <DeleteApp appID={appID} appName={listed?.name ?? 'this app'} onDeleted={onBack} />
        </div>
      </Sheet>
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
    <Sheet
      heading={
        <div
          style={{
            display: 'flex',
            flexDirection: 'column',
            alignItems: 'flex-start',
            gap: 'var(--space-3)',
          }}
        >
          <Button variant="ghost" onClick={onBack} style={{ alignSelf: 'flex-start' }}>
            Apps
          </Button>
          <div style={{ display: 'flex', alignItems: 'center', gap: 'var(--space-4)' }}>
            <div style={{ display: 'flex', alignItems: 'center', gap: 'var(--space-3)' }}>
              <h3 style={{ font: 'var(--type-h3)', margin: 0 }}>{app.data.name}</h3>
              <StatusIndicator
                status={statusSymbol(app.data.state)}
                label={statusLabel(app.data.state)}
              />
            </div>
            <div style={{ display: 'flex', alignItems: 'flex-start', gap: 'var(--space-3)' }}>
              {/* Deploy where somebody looking at an app can reach it, from any
                  tab, rather than under everything on the overview. Only for an
                  app that has a configuration to deploy: before that, the thing
                  to do is accept one. */}
              {app.data.pinned_spec_id && (
                <DeployButton
                  app={app.data}
                  onRefused={(message, remedy, code) =>
                    setRefusal(message ? { message, remedy, code } : null)
                  }
                />
              )}

              {/* Stopping is the thing somebody reaches for when they would
                  otherwise delete: it keeps the storage, the configuration and
                  the address, and starting brings back what was running. */}
              <Lifecycle app={app.data} />

              {/* In the header rather than on Settings: an app whose source could
                  not be fetched has no pinned spec and therefore no Settings tab,
                  and that is the app most likely to be deleted. */}
              <DeleteApp appID={app.data.id} appName={app.data.name} onDeleted={onBack} />
            </div>
          </div>
        </div>
      }
      // The sheet's marginal data. An app's id is what the CLI and the API want
      // and the console had nowhere to show it, so it was a value you could
      // only get by reading the address bar.
      note={app.data.id}
    >
      {/* Sheet provides the page inset, so this carries only the measure and
          the gap above the tabs. */}
      {refusal && (
        <div style={{ paddingBottom: 'var(--space-4)', maxWidth: MEASURE }}>
          {/* The server's words, which are written to be acted on (R-105) —
              and, where the console has the screen that acts on them, the way
              there. A refusal that names a remedy on a page with no control
              for it is a remedy nobody can take. */}
          <Banner
            tone="failed"
            action={
              REFUSALS[refusal.code ?? ''] ? (
                <Button
                  variant="secondary"
                  onClick={() => {
                    const fix = REFUSALS[refusal.code ?? ''];
                    if (fix) setTab(fix.tab, fix.focus);
                    setRefusal(null);
                  }}
                >
                  {REFUSALS[refusal.code ?? '']?.label}
                </Button>
              ) : (
                <Button variant="ghost" onClick={() => setRefusal(null)}>
                  Dismiss
                </Button>
              )
            }
          >
            {refusal.message}
            {refusal.remedy ? ` ${refusal.remedy}` : ''}
          </Banner>
        </div>
      )}

      <Tabs value={tab} onChange={setTab} items={tabs} />

      <div style={{ paddingTop: 'var(--space-5)' }}>
        {tab === 'detection' && <DetectionReview appID={app.data.id} reviewed={reviewed} />}
        {tab === 'sharing' && <Sharing appID={app.data.id} appName={app.data.name} />}
        {tab === 'overview' && <AppOverview app={app.data} onGo={setTab} />}
        {tab === 'logs' && <Logs app={app.data} workload={focus} />}
        {tab === 'resources' && <Resources appID={app.data.id} focus={focus} />}
        {tab === 'terminal' && <Terminal appID={app.data.id} />}
      </div>
    </Sheet>
  );
}

