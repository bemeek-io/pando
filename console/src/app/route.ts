// Where you are, in the address bar rather than in a variable.
//
// The console kept its position in component state, so a reload dropped
// whoever was looking at it back on the launcher — and the back button did
// nothing, and there was no way to send somebody a link to an app.
//
// The paths here are exactly the ones the server reserves for the console
// (`/`, `/login`, `/admin`, `/admin/*` — see consoleRoutes in httpapi). It
// serves index.html for any of them, which is what makes a reload on a deep
// link work, and nothing else may be added here without reserving it there:
// every console route is a slug an app cannot have, and that is what keeps the
// two namespaces apart (R-023).

import { useEffect, useState } from 'react';

export type Section =
  | 'apps'
  | 'api'
  | 'accounts'
  | 'identity'
  | 'installation'
  | 'policy'
  | 'backups'
  | 'audit';

export interface Route {
  /** Settings is its own page, not a section of the admin console: it is
   *  about the person, and everyone reaches it. It still lives under /admin,
   *  because that prefix is already reserved against app slugs (R-023). */
  view: 'launcher' | 'admin' | 'settings';
  section: Section;
  appID?: string;
  tab?: string;
}

const SECTIONS: Section[] = [
  'apps',
  'api',
  'accounts',
  'identity',
  'installation',
  'policy',
  'backups',
  'audit',
];

/** Reads a route out of a path. Anything unrecognized is the launcher. */
export function parse(pathname: string): Route {
  const parts = pathname.split('/').filter(Boolean);

  if (parts[0] !== 'admin') return { view: 'launcher', section: 'apps' };
  if (parts[1] === 'settings') return { view: 'settings', section: 'apps' };

  // /admin/apps/{id}[/{tab}]
  if (parts[1] === 'apps' && parts[2]) {
    return { view: 'admin', section: 'apps', appID: parts[2], tab: parts[3] };
  }

  const section = SECTIONS.find((s) => s === parts[1]);
  return { view: 'admin', section: section ?? 'apps' };
}

/** The path for a route. The inverse of parse, and tested as such. */
export function format(route: Route): string {
  if (route.view === 'launcher') return '/';
  if (route.view === 'settings') return '/admin/settings';
  if (route.section === 'apps' && route.appID) {
    return `/admin/apps/${route.appID}${route.tab ? `/${route.tab}` : ''}`;
  }
  return route.section === 'apps' ? '/admin' : `/admin/${route.section}`;
}

/**
 * The current route, and a way to change it.
 *
 * popstate is what makes the browser's own back and forward buttons work. It
 * is easy to leave out and easy not to notice: without it the address bar
 * changes and the page does not, which is worse than having no routing at all.
 */
export function useRoute(): [Route, (next: Route, replace?: boolean) => void] {
  const [route, setRoute] = useState<Route>(() => parse(window.location.pathname));

  useEffect(() => {
    const onPop = () => setRoute(parse(window.location.pathname));
    window.addEventListener('popstate', onPop);
    return () => window.removeEventListener('popstate', onPop);
  }, []);

  const go = (next: Route, replace = false) => {
    const path = format(next);
    if (path !== window.location.pathname) {
      window.history[replace ? 'replaceState' : 'pushState']({}, '', path);
    }
    setRoute(next);
  };

  return [route, go];
}
