// A proposed policy change as the Policy screen would put it (R-344).

const show = (v: unknown) => (v === null || v === undefined ? 'unset' : JSON.stringify(v));

// Each setting by the name the Policy screen gives it, so a proposal reads as
// the switch it would flip rather than as a field name.
const LABELS: Record<string, string> = {
  source_allowlist: 'Where apps may be created from',
  disabled_verbs: 'Permissions nobody may use',
  agent_disabled_verbs: "Verbs an agent's token may not use",
  allow_anonymous_grants: 'Sharing apps with everyone',
  public_sharing: 'Sharing apps with everyone',
  min_build_isolation: 'Minimum isolation for builds',
  min_runtime_isolation: 'Minimum isolation for running apps',
  egress_allowlist: 'Where apps may connect out to',
  require_backup_before_destroy: 'Require a backup before anything is destroyed',
  max_token_lifetime_days: 'Longest a token may live, in days',
  max_log_disk_bytes: 'Total disk for app logs',
  disable_ai_screening: 'Turn off AI screening of deployment plans',
  disable_anonymous_use_audit: "Don't record visits from people who aren't signed in",
  min_security_score: 'Minimum security score',
  insecure_action: 'Stop apps that fall below it while running',
  insecure_grace_hours: 'Grace period, in hours',
  ignore_unfixable_findings: 'Ignore findings with no fix available',
};

const SHARING: Record<string, string> = { allowed: 'Allowed', passcode_only: 'Only with a passcode', none: 'Not allowed' };

const asList = (v: unknown): string[] => (Array.isArray(v) ? (v as string[]) : []);

/**
 * One change as the Policy screen would put it: a title, and what it does.
 * Terminal access is its own switch on that screen, though it is stored as
 * app.exec in the permissions nobody may use, so it reads as that switch.
 */
export function describeChange(c: { key: string; from: unknown; to: unknown }): { title: string; detail: string }[] {
  if (c.key === 'disabled_verbs' || c.key === 'agent_disabled_verbs') {
    const before = asList(c.from);
    const after = asList(c.to);
    const out: { title: string; detail: string }[] = [];
    const agent = c.key === 'agent_disabled_verbs';
    for (const v of after.filter((x) => !before.includes(x))) {
      out.push(
        !agent && v === 'app.exec'
          ? { title: 'Turn off terminal access for the whole installation', detail: 'Off to on. Nobody gets a terminal, including the person who owns the app.' }
          : { title: LABELS[c.key]!, detail: `Adds ${v}.` },
      );
    }
    for (const v of before.filter((x) => !after.includes(x))) {
      out.push(
        !agent && v === 'app.exec'
          ? { title: 'Turn off terminal access for the whole installation', detail: 'On to off. Terminals are allowed again, to whoever holds app.exec.' }
          : { title: LABELS[c.key]!, detail: `Removes ${v}.` },
      );
    }
    return out;
  }
  if (c.key === 'public_sharing') {
    return [{ title: LABELS.public_sharing!, detail: `${SHARING[String(c.from)] ?? 'Allowed'} to ${SHARING[String(c.to)] ?? show(c.to)}.` }];
  }
  if (typeof c.to === 'boolean' || typeof c.from === 'boolean') {
    return [{ title: LABELS[c.key] ?? c.key, detail: c.to ? 'Off to on.' : 'On to off.' }];
  }
  return [{ title: LABELS[c.key] ?? c.key, detail: `${show(c.from)} to ${show(c.to)}.` }];
}
