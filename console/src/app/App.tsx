// Two audiences, one app (R-264, R-265).
//
// Root is the launcher. The admin console is not a separate build — a user with
// no administrative verbs simply never reaches those routes, which is what
// makes a non-technical user's first experience a page of tiles rather than a
// dashboard.
//
// Routing is a path switch rather than TanStack Router. Design 08 §1.2 lists
// typed routes as a [P] choice; this overrides it, because the router's value
// is in a large route tree and there are three shapes here — the launcher, a
// section, and an app with a tab. Revisit when there is a tree to type.
//
// It is a real path switch now. It used to be this comment over a useState,
// which is not a path switch at all: a reload dropped whoever was looking at
// the console back on the launcher, the back button did nothing, and there was
// no way to send anybody a link to an app.

import { useAdministrative, usePrincipal } from './principal';
import { useTheme } from '../ui/theme';
import { useRoute } from './route';
import { ChangePassword, Login } from '../auth/Login';
import { Launcher } from '../launcher/Launcher';
import { AdminConsole } from '../admin/AdminConsole';

export function App() {
  // Before anything decides what to render: the sign-in page and the error
  // states are the console too, and they were the screens most likely to be
  // met in the dark.
  useTheme();

  const principal = usePrincipal();
  const isAdmin = useAdministrative();
  const [route, go] = useRoute();

  if (principal.isPending) {
    return <Centered>Loading.</Centered>;
  }

  // Not signed in. The proxy sends an unauthenticated visitor here (R-023), and
  // so does an expired session on an open page. Both land on the same form
  // rather than a link to one: this used to render a link to /login, and /login
  // rendered this component, so the only way into a fresh install was the API.
  if (principal.isError) {
    return <Login />;
  }

  // R-046: the generated first-run credential must be changed before anything
  // else. Placed here rather than inside the launcher so there is no screen
  // that can be reached around it — a "you should change your password" banner
  // is a suggestion, and R-046 says must.
  if (principal.data?.must_change_password) {
    return <ChangePassword username={principal.data.username} />;
  }

  // Someone who lands on /admin without the verbs for it gets the launcher,
  // and the address bar is corrected to say so — replace, not push, so the
  // back button does not bounce them between a page they cannot see and one
  // they can. The server refuses the requests underneath regardless; this is
  // only about not showing a shell that answers 403 to everything (R-265).
  if (route.view === 'admin' && isAdmin) {
    return <AdminConsole route={route} go={go} onLeave={() => go({ view: 'launcher', section: 'apps' })} />;
  }
  if (route.view === 'admin' && !isAdmin && !principal.isPending) {
    go({ view: 'launcher', section: 'apps' }, true);
  }

  return (
    <Launcher
      onAdmin={isAdmin ? () => go({ view: 'admin', section: 'apps' }) : undefined}
    />
  );
}

function Centered({ children }: { children: React.ReactNode }) {
  return (
    <div
      style={{
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        minHeight: '100vh',
        background: 'var(--paper)',
        font: 'var(--type-body-ui)',
        color: 'var(--ink-secondary)',
      }}
    >
      {children}
    </div>
  );
}
