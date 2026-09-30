import { describe, expect, it } from 'vitest';

import { failedSignIn, providerStart } from './sso';

describe('providerStart', () => {
  it('carries where the person was going, under the reserved prefix', () => {
    expect(providerStart('idp_1', '?next=%2Fapps%2Fnotes', 'https://pando.example.com/login')).toBe(
      '/.pando/api/v1/auth/providers/idp_1/start?next=%2Fapps%2Fnotes',
    );
  });

  it('refuses to carry another origin', () => {
    expect(providerStart('idp_1', '?next=https%3A%2F%2Fevil.example', 'https://pando.example.com/login')).toBe(
      '/.pando/api/v1/auth/providers/idp_1/start?next=%2F',
    );
  });
});

describe('failedSignIn', () => {
  it('reads a flow ID or a fixed key', () => {
    expect(failedSignIn('?sso_error=expired')).toBe('expired');
    expect(failedSignIn('?sso_error=Ab_9-x&next=%2F')).toBe('Ab_9-x');
  });

  it('ignores anything that is not an ID', () => {
    expect(failedSignIn('?sso_error=Call%20555-0100')).toBeNull();
    expect(failedSignIn('')).toBeNull();
  });
});
