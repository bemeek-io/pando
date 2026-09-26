import { describe, expect, it } from 'vitest';

import { NO_FILTERS, auditQuery, filtersFrom, filtersFromSearch, linkQuery } from './audit';

describe('audit filters', () => {
  it('carry an account page link into the Audit log unchanged', () => {
    const f = { ...NO_FILTERS, involving: 'usr_01', when: '7d' };
    expect(linkQuery(f)).toBe('involving=usr_01&when=7d');
    expect(filtersFrom(linkQuery(f))).toEqual(f);
  });

  it('ask the API for either side of one account', () => {
    const now = Date.parse('2026-09-21T12:00:00Z');
    const q = new URLSearchParams(auditQuery({ ...NO_FILTERS, involving: 'usr_01', when: '24h' }, undefined, now));
    expect(q.get('involving')).toBe('usr_01');
    expect(q.get('since')).toBe('2026-09-20T12:00:00.000Z');
    expect(q.get('principal_id')).toBeNull();
  });

  it('ignore a range a link cannot mean', () => {
    expect(filtersFrom('when=forever&since=2026-01-01T00:00').when).toBe('');
    expect(filtersFrom('when=7d&since=2026-01-01T00:00').since).toBe('');
  });

  it('turn a kind of actor into principal_kind', () => {
    expect(auditQuery({ ...NO_FILTERS, actor: 'kind:system' })).toBe('?principal_kind=system');
  });

  it('send several actions as one parameter each, any of which matches', () => {
    const q = new URLSearchParams(auditQuery({ ...NO_FILTERS, action: 'app.create, app.delete,', app: 'app_01' }));
    expect(q.getAll('action')).toEqual(['app.create', 'app.delete']);
    expect(q.get('app_id')).toBe('app_01');
  });

  it('show an AI search as the fields a person would have filled (R-345)', () => {
    const f = filtersFromSearch({
      actions: ['app.create', 'app.delete'],
      principal_id: 'usr_01',
      since: '2026-08-26T00:00:00Z',
    });
    expect(f.action).toBe('app.create, app.delete');
    expect(f.actor).toBe('usr_01');
    expect(f.when).toBe('custom');
    expect(new Date(f.since).toISOString()).toBe('2026-08-26T00:00:00.000Z');
    expect(filtersFromSearch({ principal_kind: 'system' }).actor).toBe('kind:system');
  });
});
