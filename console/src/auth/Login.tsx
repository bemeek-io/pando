// Sign in, and on a new installation, set up.
//
// The console had no login screen at all: `/login` rendered the launcher, which
// rendered a link to `/login`. This is that screen.
//
// A new installation has no account, and nothing to sign in with. It used to
// generate an administrator password and print it to the server log, which
// meant the first person in had to be the person with the log. Now the first
// person to reach this page creates the administrator account (R-046's single
// administrative local user) with a password they chose — and the server
// refuses a second attempt the moment one account exists, so there is exactly
// one first person.
//
// Local accounts only for now. An external identity provider begins with a
// redirect (`IdentityAdapter.Begin`), and when one is configured this page
// grows a button per provider rather than a second page — R-044's providers are
// alternatives to this form, not alternatives to signing in.

import { useEffect, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Banner, Button, Input, Logo, Skeleton } from '@design';

import { api, RequestFailed } from '@api/client';

import { FieldSkeleton, HeadingSkeleton, LineSkeleton, Loading } from '../ui/Loading';
import { TopoMap } from '../ui/TopoBackground';
import { returnTo } from './return-to';

export function Login() {
  // Why the setup form was taken away, when somebody else finished first.
  const [taken, setTaken] = useState<string>();

  const setup = useQuery({
    queryKey: ['setup'],
    queryFn: () => api.get<{ needed: boolean }>('/setup'),
  });

  // Not a form yet: the page does not know which form it is, and showing the
  // sign-in form for a moment on a new installation invites typing into it. So
  // the outline both forms share — a heading, two labeled fields, a button —
  // with nothing in it to type into.
  if (setup.isPending) {
    return (
      <Frame heading={<HeadingSkeleton width="14ch" />}>
        <Loading gap="var(--space-4)">
          {[0, 1].map((n) => (
            <div key={n} style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-2)' }}>
              <LineSkeleton width="10ch" font="var(--type-label)" />
              <FieldSkeleton />
            </div>
          ))}
          <Skeleton height="var(--control-console)" />
        </Loading>
      </Frame>
    );
  }

  // A failure to ask falls back to signing in, which is right on every
  // installation but a new one — and on a new one, signing in says why not.
  if (setup.data?.needed) {
    return (
      <Setup
        onTaken={async (message) => {
          const again = await setup.refetch();
          if (again.data?.needed === false) {
            setTaken(message);
            return true;
          }
          return false;
        }}
      />
    );
  }

  return <SignIn notice={taken} />;
}

function SignIn({ notice }: { notice?: string }) {
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
        {notice && <Banner tone="info">{notice}</Banner>}
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
 * A new installation's first account.
 *
 * `onTaken` is asked whenever the server refuses, and answers whether the
 * refusal was somebody else finishing setup first. A refusal is otherwise
 * about this form — a short password, an unusable username — and stays on it.
 */
function Setup({ onTaken }: { onTaken: (message: string) => Promise<boolean> }) {
  const [username, setUsername] = useState('admin');
  const [displayName, setDisplayName] = useState('');
  const [password, setPassword] = useState('');
  const [confirm, setConfirm] = useState('');
  const queries = useQueryClient();

  const mismatch = confirm !== '' && password !== confirm;

  const create = useMutation({
    mutationFn: () =>
      api.post<unknown>('/setup', { username, display_name: displayName, password }),
    // The server set the session cookie; everything downstream reads GET /me,
    // exactly as after signing in.
    onSuccess: () => void queries.invalidateQueries(),
    onError: (e) => void onTaken(withRemedy(e)),
  });

  const edit = (set: (v: string) => void) => (value: string) => {
    if (create.isError) create.reset();
    set(value);
  };

  return (
    <Frame
      heading="Set up Pando"
      lede="This installation has no accounts yet. The first person here creates the administrator account."
    >
      <form
        onSubmit={(e) => {
          e.preventDefault();
          create.mutate();
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
          label="Name"
          value={displayName}
          autoComplete="name"
          helper="Optional. Shown in the console and in the audit log."
          onChange={(e) => edit(setDisplayName)(e.target.value)}
        />
        <Input
          label="Password"
          type="password"
          value={password}
          autoComplete="new-password"
          helper="At least 10 characters."
          // The server's refusal, on the field it is most often about (see
          // ChangePassword). A username it cannot use says so in its own words.
          error={!mismatch && create.isError ? messageOf(create.error) : undefined}
          onChange={(e) => edit(setPassword)(e.target.value)}
        />
        <Input
          label="Password again"
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
          disabled={create.isPending || username === '' || password === '' || password !== confirm}
        >
          {create.isPending ? 'Setting up' : 'Create account'}
        </Button>
      </form>
    </Frame>
  );
}

/**
 * The forced password change (R-046).
 *
 * An account arrives here when somebody else chose its password: an
 * administrator who added it or reset it and handed the password over, or
 * whoever set PANDO_ADMIN_PASSWORD for an installation's first account. A
 * password known to two people is a handover token rather than a password —
 * and until this screen existed, the flag saying it had to be changed was
 * something nothing could clear.
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
      // Said plainly, and only once. The person is holding a string somebody
      // gave them; they do not need to be told that this is for security.
      lede="You signed in with a password somebody else set. Choose your own."
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

/**
 * The shared card. Left-aligned, one column.
 *
 * This used to carry a note saying there was no contour illustration here
 * because the logo was already doing that work. That stopped being true when
 * the real logo arrived: the mark is the "pando." wordmark with a marker-red
 * full stop, not the three nested contours the spec had described, and the
 * design system's own readme records that the logo is no longer one of the
 * places the contour figure appears.
 *
 * So the one screen every person sees before anything else carried no trace of
 * the brand's single bold idea. It gets the map as a picture, in colour. On a
 * wide window the land rises on the right and falls away before it reaches
 * the form on the left; on a narrow one it rises at the top and falls away
 * above the form. Either way the map ends where its lowest contour does — no
 * panel edge — and the form sits on plain paper, never over a line.
 */
function Frame({
  heading = 'Sign in to Pando',
  lede,
  children,
}: {
  /** A string is the page's h1; anything else — a loading outline — stands in
   *  its place without being announced as a heading. */
  heading?: React.ReactNode;
  lede?: string;
  children: React.ReactNode;
}) {
  const wide = useWide();

  return (
    <div
      style={{
        minHeight: '100vh',
        background: 'var(--paper)',
        position: 'relative',
        isolation: 'isolate',
        overflow: 'hidden',
        display: 'flex',
        alignItems: wide ? 'center' : 'flex-start',
      }}
    >
      <TopoMap seed="sign-in" recede={wide ? 'left' : 'down'} />
      <div
        style={{
          width: wide ? '44%' : '100%',
          display: 'flex',
          justifyContent: 'center',
          padding: wide ? 'var(--space-8)' : '42vh var(--space-5) var(--space-6)',
        }}
      >
        <div style={{ width: '100%', maxWidth: '36ch' }}>
          <div style={{ marginBottom: 'var(--space-6)' }}>
            <Logo size={24} />
          </div>
          {typeof heading === 'string' ? (
            heading && <h1 style={{ font: 'var(--type-h3)', color: 'var(--ink)', margin: 0 }}>{heading}</h1>
          ) : (
            heading
          )}
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
    </div>
  );
}

/** Wide enough for the form and the map side by side. In em, so it follows the
 *  reader's text size rather than a device's pixel count. */
function useWide(): boolean {
  const query = '(min-width: 60em)';
  const [wide, setWide] = useState(() => window.matchMedia(query).matches);
  useEffect(() => {
    const m = window.matchMedia(query);
    const on = () => setWide(m.matches);
    m.addEventListener('change', on);
    return () => m.removeEventListener('change', on);
  }, []);
  return wide;
}

function messageOf(error: unknown): string {
  if (error instanceof RequestFailed) return error.message;
  return 'Pando could not reach the server. Check that it is running and try again.';
}

/** The message and, when the server gave one, what to do about it. */
function withRemedy(error: unknown): string {
  const message = messageOf(error);
  const remedy = error instanceof RequestFailed ? error.remedy : undefined;
  return remedy ? `${message} ${remedy}` : message;
}
