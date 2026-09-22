// The passcode page's address: which app it is for, and the way back to
// signing in.

import { describe, expect, it } from 'vitest';

import { passcodeApp, signInInstead } from './passcode';

describe('passcodeApp', () => {
  it('reads an app ID', () => {
    expect(passcodeApp('?passcode=app_01HQ8ABC&next=%2F')).toBe('app_01HQ8ABC');
  });

  it('refuses anything not shaped like one', () => {
    expect(passcodeApp('')).toBeNull();
    expect(passcodeApp('?passcode=')).toBeNull();
    expect(passcodeApp('?passcode=app_1%2F..%2Fusers')).toBeNull();
    expect(passcodeApp('?passcode=usr_01HQ8')).toBeNull();
  });
});

describe('signInInstead', () => {
  it('drops the passcode and keeps where to go next', () => {
    expect(signInInstead('/.pando/login', '?passcode=app_1&next=%2Fdocs')).toBe('/.pando/login?next=%2Fdocs');
  });

  it('is the bare page when there is nothing else', () => {
    expect(signInInstead('/.pando/login', '?passcode=app_1')).toBe('/.pando/login');
  });
});
