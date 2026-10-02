import { describe, expect, it, vi } from 'vitest';

// principal.ts brings in the API client, which reads the page's address when
// it loads. There is no page here: give it the least it needs.
vi.hoisted(() => {
  (globalThis as { window?: unknown }).window ??= { location: { pathname: '/' } };
});

import { AppVerb } from '../admin/verbs';
import { InstallVerb } from '../app/principal';
import { VERB_NOTES } from './verbNotes';

describe('every verb the console knows says what it lets somebody do', () => {
  it.each([...Object.values(AppVerb), ...Object.values(InstallVerb)])('%s has a note', (verb) => {
    expect(VERB_NOTES[verb]).toBeTruthy();
  });

  // Issue #79 renamed app.egress.override; a note under the old name would
  // describe a verb nobody can be granted.
  it('has no note for a verb that no longer exists', () => {
    expect(VERB_NOTES['app.egress.override']).toBeUndefined();
  });

  // Issue #81 replaced install.apps.manage with the App manager role.
  it('has no note for the retired install.apps.manage bundle', () => {
    expect(VERB_NOTES['install.apps.manage']).toBeUndefined();
  });
});
