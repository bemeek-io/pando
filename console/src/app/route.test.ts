import { describe, expect, it } from 'vitest';

import { format, parse } from './route';
import type { Route } from './route';

describe('route', () => {
  const routes: Route[] = [
    { view: 'launcher', section: 'apps' },
    { view: 'settings', section: 'apps' },
    { view: 'admin', section: 'apps' },
    { view: 'admin', section: 'api' },
    { view: 'admin', section: 'apps', appID: 'app_01', tab: 'logs' },
  ];

  it.each(routes)('parse is the inverse of format for %o', (route) => {
    expect(parse(format(route))).toEqual(route);
  });

  it('keeps settings under the reserved /admin prefix', () => {
    // A top-level /settings would be a slug no app could have (R-023).
    expect(format({ view: 'settings', section: 'apps' })).toBe('/admin/settings');
  });
});
