import { describe, expect, it } from 'vitest';

import { describeChange } from './policyChange';

describe('a proposed policy change reads as the Policy screen puts it (R-344)', () => {
  it('names terminal access as its switch, not as disabled_verbs', () => {
    expect(describeChange({ key: 'disabled_verbs', from: null, to: ['app.exec'] })).toEqual([
      {
        title: 'Turn off terminal access for the whole installation',
        detail: 'Off to on. Nobody gets a terminal, including the person who owns the app.',
      },
    ]);
  });

  it('says which other permissions are added or removed', () => {
    expect(describeChange({ key: 'disabled_verbs', from: ['app.delete'], to: ['app.exec'] }).map((d) => d.detail)).toEqual([
      'Off to on. Nobody gets a terminal, including the person who owns the app.',
      'Removes app.delete.',
    ]);
  });

  it('names sharing and switches in words', () => {
    expect(describeChange({ key: 'public_sharing', from: 'allowed', to: 'none' })[0]!.detail).toBe('Allowed to Not allowed.');
    expect(describeChange({ key: 'disable_ai_screening', from: null, to: true })[0]).toEqual({
      title: 'Turn off AI screening of deployment plans',
      detail: 'Off to on.',
    });
  });
});
