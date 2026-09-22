// What the caller may do on one app.
//
// `GET /apps/{id}` answers with the app and the app-scoped verbs the caller
// holds on it, computed by the authorizer itself — host policy included — so
// the console's answer and the API's checks cannot disagree (R-261). An app's
// screen reads them once and every control on it asks here, rather than each
// control asking the server, or guessing from a role name.
//
// There is no implication graph between verbs (R-082): app.spec.edit says
// nothing about app.deploy, so each control asks for exactly the verb its
// endpoint checks, and no other.
//
// This is an affordance, not the enforcement. Every endpoint checks its own
// verb; hiding a button only spares somebody a control that would be refused.

import { createContext, useContext } from 'react';
import type { App } from '@api/types.gen';

/** App-scoped verbs (design 06 §5), as the server names them. */
export const AppVerb = {
  View: 'app.view',
  LogsRead: 'app.logs.read',
  Deploy: 'app.deploy',
  Restart: 'app.restart',
  SpecEdit: 'app.spec.edit',
  SecretsWrite: 'app.secrets.write',
  SecretsRead: 'app.secrets.read',
  Exec: 'app.exec',
  GrantsManage: 'app.grants.manage',
  RoutingOverride: 'app.routing.override',
  ResourcesOverride: 'app.resources.override',
  EgressOverride: 'app.egress.override',
  Delete: 'app.delete',
} as const;

export type AppVerb = (typeof AppVerb)[keyof typeof AppVerb];

/** The app as `GET /apps/{id}` returns it: the record, plus the caller's verbs
 *  on it. The generated type is the record alone, because the handler adds
 *  the verbs beside it. */
export type AppWithVerbs = App & { verbs?: string[] | null };

/**
 * Whether the verbs allow one.
 *
 * Absent verbs allow nothing. A record without them came from somewhere other
 * than `GET /apps/{id}` — the list, or a write's response — and saying yes to
 * a control on that would be the guess this file exists to avoid.
 */
export function can(verbs: readonly string[] | null | undefined, verb: AppVerb): boolean {
  return (verbs ?? []).includes(verb);
}

/** Whether the caller holds any verb that changes the app. False for somebody
 *  who may only look at it and read its logs. */
export function changesAnything(verbs: readonly string[] | null | undefined): boolean {
  return (verbs ?? []).some((v) => v !== AppVerb.View && v !== AppVerb.LogsRead);
}

/**
 * The open app's verbs, for the controls under its screen.
 *
 * A context rather than a query per control: the tabs sit several components
 * deep, and each one mounting its own observer of `['apps', appID]` after the
 * cache went stale would be a request per control.
 */
export const AppVerbs = createContext<readonly string[]>([]);

/** Whether the caller may use this verb on the app whose screen this is. */
export function useCan(verb: AppVerb): boolean {
  return can(useContext(AppVerbs), verb);
}

/**
 * Keep the verbs when a write hands back the record.
 *
 * `PATCH /apps/{id}` and the icon endpoints answer with the app alone. Put in
 * the cache as it is, that would drop the verbs, and every control on the
 * screen would disappear on a rename.
 */
export function keepVerbs(updated: App) {
  return (old: AppWithVerbs | undefined): AppWithVerbs => ({ ...updated, verbs: old?.verbs });
}
