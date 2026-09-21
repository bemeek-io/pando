import { describe, expect, it } from 'vitest';

import { deletePath } from './delete-app';

describe('R-204 the backup decision is carried, never assumed', () => {
  it('asks for a final backup when the person keeps the storage', () => {
    expect(deletePath('app_01HQ8', 'backup')).toBe('/apps/app_01HQ8?backup=true');
  });

  it('forces only when the person chose to discard the storage', () => {
    expect(deletePath('app_01HQ8', 'discard')).toBe('/apps/app_01HQ8?force=true');
  });

  // An app with no storage has no decision to make, and `force=true` here would
  // record a deletion as forced when nothing was discarded.
  it('sends no decision for an app that keeps nothing', () => {
    expect(deletePath('app_01HQ8', 'none')).toBe('/apps/app_01HQ8');
  });
});
