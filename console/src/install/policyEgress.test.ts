import { describe, expect, it } from 'vitest';

import { approvalsNeeded, egressPatch, installEgress, lines, looseningRule } from './policyEgress';

describe("the installation's egress rules as the Policy screen reads them (R-181)", () => {
  it('reads an unset policy as anywhere', () => {
    expect(installEgress({})).toEqual({ mode: 'allow_all', list: [] });
  });

  it('reads the field from before issue #79 as an allowlist when no mode is set', () => {
    expect(installEgress({ egress_allowlist: ['api.example.com'] })).toEqual({
      mode: 'allowlist',
      list: ['api.example.com'],
    });
  });

  it('prefers the mode when both are set', () => {
    expect(
      installEgress({ egress_mode: 'denylist', egress_list: ['evil.example'], egress_allowlist: ['api.example.com'] }),
    ).toEqual({ mode: 'denylist', list: ['evil.example'] });
    expect(installEgress({ egress_mode: 'allow_all', egress_allowlist: ['api.example.com'] })).toEqual({
      mode: 'allow_all',
      list: [],
    });
  });

  it('writes in the new fields and clears the old one', () => {
    expect(egressPatch('allowlist', ['a.example'])).toEqual({
      egress_mode: 'allowlist',
      egress_list: ['a.example'],
      egress_allowlist: undefined,
    });
  });

  it('applies the defaults the server applies', () => {
    expect(looseningRule({})).toBe('verb');
    expect(looseningRule({ egress_loosening: 'forbidden' })).toBe('forbidden');
    expect(approvalsNeeded(0)).toBe(1);
    expect(approvalsNeeded(undefined)).toBe(1);
    expect(approvalsNeeded(3)).toBe(3);
  });

  it('reads one entry per line', () => {
    expect(lines(' a.example \n\n*.b.example:443\n')).toEqual(['a.example', '*.b.example:443']);
  });
});
