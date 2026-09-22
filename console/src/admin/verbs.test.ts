import { describe, expect, it } from 'vitest';
import type { App } from '@api/types.gen';
import { AppVerb, can, changesAnything, keepVerbs } from './verbs';

describe('can', () => {
  it('allows exactly the verbs the server returned', () => {
    const verbs = ['app.view', 'app.deploy'];
    expect(can(verbs, AppVerb.Deploy)).toBe(true);
    expect(can(verbs, AppVerb.SpecEdit)).toBe(false);
  });

  // A record from the list or from a write carries no verbs, and a control
  // shown on a guess is the control that is refused when used.
  it('allows nothing when the verbs are absent', () => {
    expect(can(undefined, AppVerb.View)).toBe(false);
    expect(can(null, AppVerb.View)).toBe(false);
  });
});

describe('changesAnything', () => {
  // install.apps.view gives app.view and app.logs.read on every app, and
  // nothing else: somebody holding it can look but not change.
  it('is false for view and logs alone', () => {
    expect(changesAnything(['app.view', 'app.logs.read'])).toBe(false);
    expect(changesAnything([])).toBe(false);
  });

  it('is true once any other verb is held', () => {
    expect(changesAnything(['app.view', 'app.restart'])).toBe(true);
  });
});

describe('keepVerbs', () => {
  // PATCH /apps/{id} answers with the record alone. Written to the cache as it
  // is, a rename would drop the verbs and hide every control on the screen.
  it('keeps the cached verbs over a write that returns the record alone', () => {
    const updated = { id: 'app_1', name: 'renamed' } as App;
    const next = keepVerbs(updated)({ ...updated, name: 'old', verbs: ['app.spec.edit'] });
    expect(next.name).toBe('renamed');
    expect(next.verbs).toEqual(['app.spec.edit']);
  });
});
