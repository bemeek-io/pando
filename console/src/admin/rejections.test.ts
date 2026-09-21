import { describe, expect, it } from 'vitest';

import { rejectedEntries } from './rejections';

describe('rejectedEntries', () => {
  it('reads the entries the summary is summarizing', () => {
    const entries = rejectedEntries({
      rejected: [
        {
          service: 'proxy',
          construct: 'volume ./Caddyfile:/etc/caddy/Caddyfile:ro',
          reason: 'This mounts the single file ./Caddyfile into the container.',
        },
      ],
    });

    expect(entries).toHaveLength(1);
    expect(entries[0]?.service).toBe('proxy');
    expect(entries[0]?.reason).toContain('./Caddyfile');
  });

  // Details are shaped by the server and typed by nobody. A malformed entry is
  // dropped rather than rendered as [object Object] under a heading that says
  // this is why Pando refused.
  it('drops anything that is not three strings', () => {
    expect(rejectedEntries(undefined)).toEqual([]);
    expect(rejectedEntries(null)).toEqual([]);
    expect(rejectedEntries({})).toEqual([]);
    expect(rejectedEntries({ rejected: 'privileged: true' })).toEqual([]);
    expect(rejectedEntries({ rejected: [null, 3, { service: 'db' }] })).toEqual([]);
  });
});
