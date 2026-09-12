// Who is signed in, and whether they see the management console.
//
// R-265: "users holding any administrative verb see an Admin entry point from
// the launcher, exposing the console scoped to whatever privileges they hold."
//
// **There is no install-level verb to hold.** R-080's catalog is entirely
// `app.*`, grants are per-app (`grants.app_id` is NOT NULL), users carry no
// admin flag, and the first-run "administrator" is an ordinary local user. Even
// `app.create`, which Sequence A step 1 calls install-level, is not in the
// catalog and is not checked anywhere.
//
// So the gate reads the half of R-265 that *is* implementable, and it is the
// half the existing screens need: a person sees the management console when
// they hold a **control-plane** grant on at least one app. `GET /apps` is
// control-plane scoped — a different list from `GET /me/apps`, which is
// data-plane scoped (R-070, R-071) — so a non-empty result means exactly
// "there is something here you can administer", and the console it opens is
// scoped to those apps and no others. That is "scoped to whatever privileges
// they hold", enforced by the server rather than asserted by the console.
//
// What is still missing is install-level administration: users, hosts, host
// policy, the audit log. Those need verbs that do not exist, and none of those
// screens exist either, so nothing is being hidden. Recorded as O-17.

import { useQuery } from '@tanstack/react-query';
import { api } from '@api/client';

export interface Principal {
  principal_kind: string;
  id: string;
  user_id?: string;
  email?: string;
  display_name?: string;
  groups?: string[] | null;
  must_change_password?: boolean;

  /**
   * Install-level verbs the principal holds.
   *
   * Absent from `GET /me` today — see the note above. Typed as optional so that
   * the console reads it the moment the API supplies it, without a second
   * change here.
   */
  verbs?: string[] | null;
}

export function usePrincipal() {
  return useQuery({
    queryKey: ['me'],
    queryFn: () => api.get<Principal>('/me'),
    retry: false,
  });
}

/**
 * Whether to show the management console (R-265).
 *
 * Asked once, here, so that resolving O-17 changes one function.
 */
export function useAdministrative(): boolean {
  const apps = useQuery({
    queryKey: ['apps'],
    queryFn: () => api.get<{ apps: unknown[] | null }>('/apps'),
    // A 403 means no control-plane access, which is an answer rather than a
    // failure — so it is not retried and not surfaced as an error.
    retry: false,
  });

  return (apps.data?.apps ?? []).length > 0;
}
