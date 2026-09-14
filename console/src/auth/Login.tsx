// Sign in.
//
// The console had no login screen at all: `/login` rendered the launcher, which
// rendered a link to `/login`. A fresh install printed a password to the server
// log and gave nobody anywhere to type it. This is that screen.
//
// Local accounts only for now. An external identity provider begins with a
// redirect (`IdentityAdapter.Begin`), and when one is configured this page
// grows a button per provider rather than a second page — R-044's providers are
// alternatives to this form, not alternatives to signing in.

import { useState } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { Button, Input, Logo } from '@design';

import { api, RequestFailed } from '@api/client';

import { returnTo } from './return-to';

export function Login() {
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const queries = useQueryClient();

  const signIn = useMutation({
    mutationFn: () => api.post<unknown>('/sessions', { username, password }),
    onSuccess: () => {
      // Somewhere to go back to, when the proxy sent them here (R-023). This
      // page is reachable on an app's own hostname at /.pando/login, and
      // without this the person who was trying to open an app signs in and
      // lands on a page of tiles, one click from where they already were.
      const next = returnTo(window.location.search, window.location.href);
      if (next) {
        window.location.assign(next);
        return;
      }

      // Otherwise the cookie is set and everything downstream reads GET /me.
      // Invalidating rather than navigating keeps this a single-page flow and
      // means the first thing the person sees is their own apps.
      void queries.invalidateQueries();
    },
  });

  // A rejection belongs to the value that caused it, and stops applying the
  // moment that value changes.
  const edit = (set: (v: string) => void) => (value: string) => {
    if (signIn.isError) signIn.reset();
    set(value);
  };

  return (
    <Frame>
      <form
        onSubmit={(e) => {
          e.preventDefault();
          signIn.mutate();
        }}
        style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-4)' }}
      >
        <Input
          label="Username"
          value={username}
          autoComplete="username"
          autoFocus
          onChange={(e) => edit(setUsername)(e.target.value)}
        />
        <Input
          label="Password"
          type="password"
          value={password}
          autoComplete="current-password"
          onChange={(e) => edit(setPassword)(e.target.value)}
          // The server's message, shown as written. It is held to the R-105
          // standard, and paraphrasing it here would undo that in the UI layer.
          error={signIn.isError ? messageOf(signIn.error) : undefined}
        />
        <Button
          type="submit"
          variant="primary"
          fullWidth
          disabled={signIn.isPending || username === '' || password === ''}
        >
          {signIn.isPending ? 'Signing in' : 'Sign in'}
        </Button>
      </form>
    </Frame>
  );
}

/**
 * The first-run password change (R-046).
 *
 * The initial credential is generated, printed once to the server log and never
 * stored in the clear, so it is a handover token rather than a password — and
 * until this screen existed, the flag saying it had to be changed was something
 * nothing could clear.
 */
export function ChangePassword({ username }: { username?: string }) {
  const [current, setCurrent] = useState('');
  const [next, setNext] = useState('');
  const [confirm, setConfirm] = useState('');
  const queries = useQueryClient();

  const mismatch = confirm !== '' && next !== confirm;

  const change = useMutation({
    mutationFn: () =>
      api.post<void>('/me/password', { current_password: current, new_password: next }),
    onSuccess: () => void queries.invalidateQueries(),
  });

  // Editing any field clears the last failure.
  //
  // Without this, a rejection outlives the value that caused it. Type something
  // too short, get "A password needs at least 10 characters", then type
  // something longer — and the message is still there, because it belongs to
  // the mutation and the mutation has not run again. The form now says the new
  // password is too short when it is not, and the only way to find out
  // otherwise is to submit anyway and disbelieve the screen.
  const edit = (set: (v: string) => void) => (value: string) => {
    if (change.isError) change.reset();
    set(value);
  };

  return (
    <Frame
      heading="Choose a password"
      // Said plainly, and only once. The person is holding a string out of a
      // log file; they do not need to be told that this is for security.
      lede="The password Pando generated for this account was shown once in the server log. Replace it with one you'll remember."
    >
      <form
        onSubmit={(e) => {
          e.preventDefault();
          change.mutate();
        }}
        style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-4)' }}
      >
        {username !== undefined && (
          <Input label="Username" value={username} readOnly autoComplete="username" />
        )}
        <Input
          label="Current password"
          type="password"
          value={current}
          autoComplete="current-password"
          autoFocus
          onChange={(e) => edit(setCurrent)(e.target.value)}
        />
        <Input
          label="New password"
          type="password"
          value={next}
          autoComplete="new-password"
          helper="At least 10 characters. A short phrase you'll remember works well."
          // The server's refusal belongs here, on the field it is about. It
          // used to render under "New password again", so a message about the
          // new password's length appeared beneath a field whose only job is
          // to match — two fields' worth of confusion from one error.
          error={!mismatch && change.isError ? messageOf(change.error) : undefined}
          onChange={(e) => edit(setNext)(e.target.value)}
        />
        <Input
          label="New password again"
          type="password"
          value={confirm}
          autoComplete="new-password"
          onChange={(e) => edit(setConfirm)(e.target.value)}
          error={mismatch ? 'These two passwords are different.' : undefined}
        />
        <Button
          type="submit"
          variant="primary"
          fullWidth
          disabled={change.isPending || current === '' || next === '' || next !== confirm}
        >
          {change.isPending ? 'Saving' : 'Save password'}
        </Button>
      </form>
    </Frame>
  );
}

/** The shared card. Left-aligned, one column, no decoration — the brand's rule
 *  is that a quiet screen stays quiet, and there is no contour illustration
 *  here because the logo is already doing that work. */
function Frame({
  heading = 'Sign in to Pando',
  lede,
  children,
}: {
  heading?: string;
  lede?: string;
  children: React.ReactNode;
}) {
  return (
    <div
      style={{
        minHeight: '100vh',
        background: 'var(--paper)',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        padding: 'var(--space-5)',
      }}
    >
      <div style={{ width: '100%', maxWidth: '36ch' }}>
        <div style={{ marginBottom: 'var(--space-6)' }}>
          <Logo size={24} />
        </div>
        <h1 style={{ font: 'var(--type-h3)', color: 'var(--ink)', margin: 0 }}>{heading}</h1>
        {lede && (
          <p
            style={{
              font: 'var(--type-body-ui)',
              color: 'var(--ink-secondary)',
              margin: 'var(--space-3) 0 0',
            }}
          >
            {lede}
          </p>
        )}
        <div style={{ marginTop: 'var(--space-6)' }}>{children}</div>
      </div>
    </div>
  );
}

function messageOf(error: unknown): string {
  if (error instanceof RequestFailed) return error.message;
  return 'Pando could not reach the server. Check that it is running and try again.';
}
