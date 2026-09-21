import { describe, expect, it } from 'vitest';

import { matches } from './search';

describe('matches', () => {
  it('matches everything when the query is empty', () => {
    expect(matches('', 'Grafana')).toBe(true);
    expect(matches('   ', 'Grafana')).toBe(true);
  });

  it('ignores case', () => {
    expect(matches('GRAF', 'Grafana')).toBe(true);
  });

  it('needs every word, in any field and any order', () => {
    expect(matches('running grafana', 'Grafana', 'Running')).toBe(true);
    expect(matches('grafana stopped', 'Grafana', 'Running')).toBe(false);
  });

  it('skips missing fields', () => {
    expect(matches('dana', undefined, null, 'Dana')).toBe(true);
    expect(matches('dana', undefined, null)).toBe(false);
  });
});
