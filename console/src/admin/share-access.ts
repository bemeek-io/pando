// What the Sharing tab sends, kept apart from the screen so it can be tested
// without rendering one.

import type { GrantRow } from '@api/types.gen';

/** A grant row as GET /apps/{id}/grants returns it. `passcode` is set on the
 *  anonymous grant when it asks for one; the generated type predates it. */
export type Grant = GrantRow & { passcode?: boolean };

/** Somebody an app can be shared with. */
export interface Recipient {
  kind: 'user' | 'group';
  id: string;
}

/** One call against /apps/{id}/grants. */
export type GrantRequest =
  | { method: 'post'; body: { plane: 'control' | 'data'; principal_kind: string; principal_id: string; role_id?: string } }
  | { method: 'patch'; grant: string; body: { role_id: string } };

/**
 * The calls that give `who` what the form asked for, given the grants already
 * on the app.
 *
 * Opening the app is the data plane; a role is the control plane (R-070,
 * R-071). Every choice on the form includes opening the app — "Open it and
 * deploy it" — so a role is always accompanied by a data grant. What is
 * already there is not asked for again: a second data grant would be refused,
 * and a person who already manages the app with one role is moved to the new
 * one rather than given two.
 *
 * @param role a role ID, or null for "open the app" alone
 */
export function shareRequests(existing: Grant[], who: Recipient, role: string | null): GrantRequest[] {
  const theirs = existing.filter((g) => g.principal_kind === who.kind && g.principal_id === who.id);
  const out: GrantRequest[] = [];

  if (role) {
    const control = theirs.find((g) => g.plane === 'control');
    if (!control) {
      out.push({
        method: 'post',
        body: { plane: 'control', principal_kind: who.kind, principal_id: who.id, role_id: role },
      });
    } else if (control.role_id !== role) {
      out.push({ method: 'patch', grant: control.id, body: { role_id: role } });
    }
  }

  if (!theirs.some((g) => g.plane === 'data')) {
    out.push({ method: 'post', body: { plane: 'data', principal_kind: who.kind, principal_id: who.id } });
  }

  return out;
}

/** Whether, and how, an app is open to everyone. */
export type Everyone = 'private' | 'public' | 'passcode';

export function everyone(grants: Grant[]): Everyone {
  const anonymous = grants.find((g) => g.principal_kind === 'anonymous');
  if (!anonymous) return 'private';
  return anonymous.passcode ? 'passcode' : 'public';
}

/** The server's floor for a passcode, checked here only to keep the button
 *  disabled until it could succeed. The server enforces it. */
export const PASSCODE_MIN = 4;

/** The app roles to offer when GET /roles is refused — it is an install-level
 *  read some people who manage an app's sharing do not have. The built-ins
 *  always exist (R-081). */
export const BUILT_IN_APP_ROLES = [
  { id: 'role_viewer', name: 'viewer' },
  { id: 'role_operator', name: 'operator' },
  { id: 'role_owner', name: 'owner' },
];

/** How the form describes each role, as what the person can do rather than the
 *  role's name. A custom role has no description here, so it is named. */
export function roleChoice(role: { id: string; name: string }): string {
  switch (role.id) {
    case 'role_viewer':
      return 'Open it and see its settings';
    case 'role_operator':
      return 'Open it and deploy it';
    case 'role_owner':
      return 'Everything, including sharing';
    default:
      return `Open it, with the ${role.name} role`;
  }
}
