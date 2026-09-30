// Signing in through an identity provider, from the sign-in page (issue #51).
//
// Pure functions of the address bar, so they can be tested without rendering
// the page. The redirects themselves are the server's: this module only
// builds the link a button follows.

import { returnTo } from './return-to';

/** How people can sign in here, as GET /auth/options answers. */
export interface SignInOptions {
  password_sign_in: boolean;
  providers: { id: string; name: string; kind: string }[] | null;
}

/**
 * Where a provider's button goes: its start address, under the reserved
 * prefix so it works on an app's own hostname too (R-172), carrying where the
 * person was going. `next` is checked the same way the password form checks
 * it, and the server checks it again.
 */
export function providerStart(providerID: string, search: string, here: string): string {
  const next = returnTo(search, here) ?? '/';
  return `/.pando/api/v1/auth/providers/${encodeURIComponent(providerID)}/start?next=${encodeURIComponent(next)}`;
}

/**
 * The failed sign-in to explain, from `?sso_error=`. The server looks it up
 * and answers with its own sentence; nothing in the address bar is shown as
 * text, so a link cannot put words on this page.
 */
export function failedSignIn(search: string): string | null {
  const id = new URLSearchParams(search).get('sso_error');
  return id && /^[A-Za-z0-9_-]{1,64}$/.test(id) ? id : null;
}
