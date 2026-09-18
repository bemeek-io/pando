import { describe, expect, it } from 'vitest';

import { looksSensitive } from './sensitive';

describe('looksSensitive', () => {
  it('defaults a credential to the secret store', () => {
    for (const key of [
      'ANTHROPIC_API_KEY',
      'POSTGRES_PASSWORD',
      'VAPID_PRIVATE_KEY',
      'CREW_TOKEN_ENC_KEY',
      'SESSION_SECRET',
      'COOKIE_SIGNING_KEY',
    ]) {
      expect(looksSensitive(key), key).toBe(true);
    }
  });

  it('leaves an ordinary setting ordinary', () => {
    for (const key of ['APP_BASE_URL', 'APP_DOMAIN', 'LOG_LEVEL', 'VAPID_SUBJECT', 'NODE_ENV']) {
      expect(looksSensitive(key), key).toBe(false);
    }
  });

  // A VAPID public key is handed to every browser that subscribes. Storing it
  // as a secret is not harmful, but it is wrong about what the thing is, and
  // the two VAPID keys sitting in different places is confusing in a way that
  // outlasts the form.
  it('reads PUBLIC as public, whatever else the name says', () => {
    expect(looksSensitive('VAPID_PUBLIC_KEY')).toBe(false);
    expect(looksSensitive('PUBLIC_TOKEN')).toBe(false);
  });
});
