// Reading the passcode page's address. Its own module for the same reason as
// return-to.ts: a pure function of a string is tested without rendering.
//
// The proxy sends a visitor without the passcode for an app that asks for one
// to `/.pando/login?passcode=<appID>&next=<path>` — the sign-in page's own
// address, with one more parameter saying which form to show.

/** The app ID in `search`'s `passcode` parameter, or null.
 *
 *  Only something shaped like an app ID: it goes into an API path, and a value
 *  from the address bar is not trusted to be one just because it is there. */
export function passcodeApp(search: string): string | null {
  const id = new URLSearchParams(search).get('passcode');
  return id && /^app_[0-9A-Za-z]+$/.test(id) ? id : null;
}

/** The same address without `passcode` — the sign-in form, keeping `next`, for
 *  someone who has an account and may have access to the app directly. */
export function signInInstead(pathname: string, search: string): string {
  const params = new URLSearchParams(search);
  params.delete('passcode');
  const rest = params.toString();
  return rest ? `${pathname}?${rest}` : pathname;
}
