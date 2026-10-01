// Deploy approval, as the console reads it (R-154 – R-159).
//
// The fields below arrive on a deployment from `GET /apps/{id}/deployments`
// and `GET /approvals`. They are written here by hand, beside the generated
// Deployment, because only a deployment waiting for approval carries them.

import type { Deployment } from '@api/types.gen';

export interface ApprovalReason {
  /** install | app_policy | app_spec | egress_loosening */
  reason: string;
  /** Written for the person deciding (R-105), shown as it is. */
  message: string;
}

export interface ApprovalDecision {
  principal_id: string;
  principal_name?: string;
  decision: 'approve' | 'reject';
  comment?: string;
  decided_at: string;
}

export type ApprovalDeployment = Deployment & {
  approvals_required?: number;
  approval_expires_at?: string;
  approval_reasons?: ApprovalReason[] | null;
  approvals?: ApprovalDecision[] | null;
  /** Whether the caller may approve or reject it. Only on waiting ones. */
  can_decide?: boolean;
};

/** One row of `GET /approvals`: a waiting deployment and the app it is for. */
export type ApprovalRow = ApprovalDeployment & { app_name: string; app_slug?: string };

export function isAwaiting(d: { status: string } | undefined): boolean {
  return d?.status === 'awaiting_approval';
}

/** How far a request has got, as a sentence. */
export function progress(d: ApprovalDeployment): string {
  const required = d.approvals_required && d.approvals_required > 0 ? d.approvals_required : 1;
  const yes = (d.approvals ?? []).filter((a) => a.decision === 'approve').length;
  if (required === 1) return yes === 0 ? 'Needs one approval.' : 'Approved.';
  if (yes === 0) return `Needs ${required} approvals. None yet.`;
  return `${yes} of ${required} approvals.`;
}

/**
 * When a request stops waiting, as a sentence. No expiry is a request that
 * waits until somebody answers (policy expiry zero, R-156).
 */
export function expiry(iso: string | undefined, now: number = Date.now()): string {
  if (!iso) return 'Waits until somebody answers.';
  const at = new Date(iso).getTime();
  if (Number.isNaN(at)) return `Expires at ${iso}.`;
  const minutes = Math.round((at - now) / 60_000);
  if (minutes <= 0) return 'Expired.';
  if (minutes < 60) return minutes === 1 ? 'Expires in 1 minute.' : `Expires in ${minutes} minutes.`;
  const hours = Math.round(minutes / 60);
  if (hours < 48) return hours === 1 ? 'Expires in 1 hour.' : `Expires in ${hours} hours.`;
  return `Expires in ${Math.round(hours / 24)} days.`;
}

/** Who decided, by name where the server gave one. */
export function decider(a: ApprovalDecision): string {
  return a.principal_name || a.principal_id;
}
