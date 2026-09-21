// Sign out.
//
// DELETE /sessions revokes the session server-side and clears the cookie; the
// console only asks for it. Then every cached query is reset rather than
// invalidated: invalidating would refetch /me, get a 401, and show the sign-in
// form — but the previous person's apps and accounts would still be sitting in
// the cache for whoever signs in next on this browser.
//
// A ghost button, like the theme toggle beside it. This is chrome.

import { useMutation, useQueryClient } from '@tanstack/react-query';
import { Button } from '@design';

import { api } from '@api/client';

export function SignOut({ onSignedOut }: { onSignedOut?: () => void }) {
  const queries = useQueryClient();

  const signOut = useMutation({
    mutationFn: () => api.del<void>('/sessions'),
    onSuccess: () => {
      onSignedOut?.();
      void queries.resetQueries();
    },
  });

  return (
    <Button
      variant="ghost"
      onClick={() => signOut.mutate()}
      disabled={signOut.isPending}
      // The one way this fails is the server being unreachable, and a button
      // that silently does nothing is worse than one that says so.
      title={signOut.isError ? 'Pando couldn’t sign you out. Try again.' : undefined}
    >
      {signOut.isPending ? 'Signing out' : 'Sign out'}
    </Button>
  );
}
