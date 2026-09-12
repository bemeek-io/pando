// Two audiences, one app (R-264, R-265).
//
// Root is the launcher. The admin console is not a separate build — a user with
// no administrative verbs simply never reaches those routes, which is what
// makes a non-technical user's first experience a page of tiles rather than a
// dashboard.
//
// Routing is a path switch rather than TanStack Router for now. Design 08 §1.2
// lists typed routes as a [P] choice, and the console has five screens: the
// router's value is in a large route tree, and adding one before there is a
// tree to type is configuration for its own sake. Noted rather than silently
// skipped — revisit when the admin console grows past a handful of screens.

import { useState } from 'react';

import { useAdministrative, usePrincipal } from './principal';
import { ChangePassword, Login } from '../auth/Login';
import { Launcher } from '../launcher/Launcher';
import { AdminConsole } from '../admin/AdminConsole';

type View = 'launcher' | 'admin';

export function App() {
  const principal = usePrincipal();
  const isAdmin = useAdministrative();
  const [view, setView] = useState<View>('launcher');

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

  if (view === 'admin' && isAdmin) {
    return <AdminConsole onLeave={() => setView('launcher')} />;
  }

  return <Launcher onAdmin={isAdmin ? () => setView('admin') : undefined} />;
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
