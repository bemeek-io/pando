// The logic behind the onboarding page (AppOnboarding.tsx): which parts of a
// proposal an AI screening put there, and what the variables form sends with
// accept. Plain functions, so the mapping is tested rather than eyeballed.
//
// What the screening changed is read from `screening.applied` — the server's
// record of the amendments that landed (design 10 §5) — never inferred from the
// spec alone. The spec does carry a source on some entries (an env entry or a
// volume marked "screened"), but only the amendment carries the reason and the
// files it rests on, and those are what somebody checking the suggestion needs.

import type { Amendment, AppSpec, Outcome, Proposal, Question, TrialObservation } from '@api/types.gen';
import { looksSensitive } from './sensitive';

/** The warning code an AI screener's notes carry (design 01 §2.8). */
export const SCREENING_ADVISORY = 'WARN_SCREENING_ADVISORY';

/** A usable environment variable name. The same pattern the server checks. */
const ENV_KEY = /^[A-Za-z_][A-Za-z0-9_]*$/;

/** Where each applied amendment lands on the page, keyed by what it touched. */
export interface Suggestions {
  /** By workload name. */
  command: Record<string, Amendment>;
  /** By workload name. */
  port: Record<string, Amendment>;
  /** By `workload/KEY`. */
  env: Record<string, Amendment>;
  health?: Amendment;
  build: { context?: Amendment; dockerfile?: Amendment; static_dir?: Amendment };
  /** By slot key. */
  slots: Record<string, Amendment>;
  /** By mount path. */
  volumes: Record<string, Amendment>;
  /** By warning message. */
  warnings: Record<string, Amendment>;
  /** Questions the screening answered, which detection then stopped asking. */
  answers: Array<{ key: string; value: string; amendment?: Amendment }>;
  /** How many changes landed, for the line under the header. */
  count: number;
}

export function envKey(workload: string, key: string): string {
  return `${workload}/${key}`;
}

/** The workload an amendment with no workload means: the primary (design 10 §3). */
export function primaryWorkload(spec: AppSpec | undefined): string {
  const workloads = spec?.workloads ?? [];
  const primary = workloads.find((w) => w.primary);
  if (primary) return primary.name;
  return workloads.length === 1 ? workloads[0]!.name : '';
}

export function suggestions(outcome: Outcome | undefined, spec: AppSpec | undefined): Suggestions {
  const found: Suggestions = {
    command: {},
    port: {},
    env: {},
    build: {},
    slots: {},
    volumes: {},
    warnings: {},
    answers: [],
    count: 0,
  };
  if (!outcome?.ran) return found;

  const primary = primaryWorkload(spec);
  const where = (a: Amendment) => a.workload?.trim() || primary;

  for (const { amendment: a } of outcome.applied ?? []) {
    found.count++;
    switch (a.kind) {
      case 'set_command':
        found.command[where(a)] = a;
        break;
      case 'set_port':
        found.port[where(a)] = a;
        break;
      case 'set_env':
        found.env[envKey(where(a), (a.key ?? '').trim())] = a;
        break;
      case 'set_health':
        found.health = a;
        break;
      case 'set_build_context':
        found.build.context = a;
        break;
      case 'set_dockerfile':
        found.build.dockerfile = a;
        break;
      case 'set_static_dir':
        found.build.static_dir = a;
        break;
      case 'add_slot':
        found.slots[(a.key ?? '').trim()] = a;
        break;
      case 'add_volume':
        found.volumes[cleanPath(a.path ?? '')] = a;
        break;
      case 'add_warning':
        // The server writes the value as the warning's message, or the reason
        // when there was no value (screening/apply.go addWarning).
        found.warnings[(a.value ?? '').trim() || (a.reason ?? '').trim()] = a;
        break;
      case 'answer_question':
        // Counted here; listed from `outcome.answers` below, which is what
        // detection actually took.
        break;
    }
  }

  const applied = (outcome.applied ?? []).map((c) => c.amendment);
  for (const [key, value] of Object.entries(outcome.answers ?? {})) {
    const amendment = applied.find((a) => a.kind === 'answer_question' && (a.key ?? '').trim() === key);
    found.answers.push({ key, value, amendment });
  }
  return found;
}

/** A mount path as the server compares them: no trailing slash. */
function cleanPath(p: string): string {
  const trimmed = p.trim();
  return trimmed.length > 1 ? trimmed.replace(/\/+$/, '') : trimmed;
}

export function volumePath(p: string): string {
  return cleanPath(p);
}

/**
 * The prompt a question was asked with, for one the screening answered.
 *
 * Detection drops an answered question from `questions`, so its wording is
 * looked for among the candidates' own questions. The key is the fallback: it
 * is what the screening answered, and a made-up sentence would not be.
 */
export function answeredPrompt(proposal: Proposal, key: string): Question | undefined {
  const candidates = [proposal.winning_bid, ...(proposal.runners_up ?? [])];
  for (const candidate of candidates) {
    const q = (candidate?.questions ?? []).find((question) => question.key === key);
    if (q) return q;
  }
  return undefined;
}

/** One row of the variables form. */
export interface VariableRow {
  /** Stable across edits, for React. */
  id: string;
  /** The workload it belongs to. Empty for a row added during review, which
   *  the server puts on the primary workload. */
  workload: string;
  key: string;
  value: string;
  secret: boolean;
  /** What detection put there. Absent for a row added during review. */
  original?: string;
}

/**
 * The rows the form starts with: every variable the proposal declares that
 * Pando fills with a plain value or nothing.
 *
 * A variable filled from a dependency (`slot_ref`) belongs to that dependency
 * and is shown there; one already filled from a secret is not something
 * detection produces. A name that reads like a credential starts as a secret
 * when it has no value yet — the same default the Environment tab takes.
 */
export function variableRows(spec: AppSpec | undefined): VariableRow[] {
  return (spec?.workloads ?? []).flatMap((w) =>
    (w.env ?? [])
      .filter((e) => !e.slot_ref && !e.secret_ref)
      .map((e) => {
        const value = e.value ?? '';
        return {
          id: envKey(w.name, e.key),
          workload: w.name,
          key: e.key,
          value,
          secret: value === '' && looksSensitive(e.key),
          original: value,
        };
      }),
  );
}

/** What the person changed on a detected row, kept apart from the rows. */
export interface RowEdit {
  value?: string;
  secret?: boolean;
}

/**
 * The form's rows: what detection has found so far, with the person's edits
 * laid over it, then the rows they added.
 *
 * Edits are held by row id (workload and name) rather than as a copy of the
 * rows, because the proposal keeps arriving while somebody types: the trial run
 * and the screening both finish after the variables first appear. A copy taken
 * at the first poll would drop whatever the screening added; edits keyed by
 * name survive every later poll, and a value somebody typed wins over one the
 * screening proposed for the same variable.
 */
export function mergeRows(detected: VariableRow[], edits: Record<string, RowEdit>, added: VariableRow[]): VariableRow[] {
  return [
    ...detected.map((row) => {
      const edit = edits[row.id];
      if (!edit) return row;
      return {
        ...row,
        value: edit.value ?? row.value,
        secret: edit.secret ?? row.secret,
      };
    }),
    ...added,
  ];
}

/** Where detection is while it runs. `stage` is set only on a running one. */
export type Stage = 'fetching' | 'detecting' | 'trying' | 'screening';

export type StepState = 'done' | 'active' | 'pending';

export interface Step {
  label: string;
  state: StepState;
}

const ORDER: Stage[] = ['fetching', 'detecting', 'trying', 'screening'];

/**
 * The steps at the top of a running detection, each ticking over as the stage
 * advances.
 *
 * The AI step is listed only once Pando is known to be taking it: a stage of
 * `screening`, or a finished screening that ran. An install without an AI
 * adapter is not a degraded one (R-335), so it is not shown a step it will
 * never reach.
 */
export function detectionSteps(status: string, stage: string | undefined, screened: boolean): Step[] {
  const running = status === 'running';
  // A stage this console does not know is read as the first: under-claiming
  // progress is harmless, claiming a step finished that is not is not.
  const at = running ? Math.max(0, ORDER.indexOf((stage ?? 'fetching') as Stage)) : ORDER.length;
  const withAI = stage === 'screening' || screened;
  const labels: Array<[Stage, string]> = [
    ['fetching', 'Reading the repository'],
    ['detecting', 'Working out what it is'],
    ['trying', 'Trying a run'],
  ];
  if (withAI) labels.push(['screening', 'Checking with AI']);
  return labels.map(([name, label]) => {
    const index = ORDER.indexOf(name);
    const state: StepState = index < at ? 'done' : index === at ? 'active' : 'pending';
    return { label, state };
  });
}

/**
 * How many rings the header's contour map shows. The map builds as detection
 * advances: two while fetching, and two more for each stage after. `finished`
 * is the full figure; `reached` is the last stage seen, so a detection that
 * failed stops at the rings it had drawn rather than growing or emptying.
 */
export function ringsFor(reached: string | undefined, finished: boolean): number {
  if (finished) return 8;
  const index = Math.max(0, ORDER.indexOf((reached ?? 'fetching') as Stage));
  return 2 + index * 2;
}

/** What the trial run showed, in one sentence, or nothing if there was none. */
export function trialSentence(trial: TrialObservation | undefined): string | undefined {
  if (!trial?.ran) return undefined;
  if (trial.crashed) return 'Pando tried a run, and the app exited on its own.';
  if (!trial.started) return 'Pando tried a run, and the app did not start.';
  const ports = trial.observed_ports ?? [];
  if (ports.length === 0) return 'Pando tried a run: the app started and opened no ports.';
  const list = ports.join(', ');
  return ports.length === 1
    ? `Pando tried a run: the app started and opened port ${list}.`
    : `Pando tried a run: the app started and opened ports ${list}.`;
}

/** What accept sends for each variable (POST /apps/{id}/detection/accept). */
export interface ValueInput {
  key: string;
  value: string;
  secret?: boolean;
  workload?: string;
}

/**
 * The `values` accept carries: only what the person changed.
 *
 * A row left as detection found it is not sent, so accepting without touching
 * the form pins exactly the proposal. An empty secret is not sent either —
 * storing nothing as a secret would put a reference in the spec that resolves
 * to an empty string, and read as though the value had been set.
 */
export function valuesPayload(rows: VariableRow[]): ValueInput[] {
  const out: ValueInput[] = [];
  for (const row of rows) {
    const key = row.key.trim();
    if (!key) continue;
    if (row.original === undefined) {
      if (row.value === '') continue;
    } else if (row.value === row.original && !(row.secret && row.value !== '')) {
      continue;
    }
    if (row.secret && row.value === '') continue;

    const input: ValueInput = { key, value: row.value };
    if (row.secret) input.secret = true;
    if (row.workload) input.workload = row.workload;
    out.push(input);
  }
  return out;
}

/**
 * Why a row cannot be sent, by row id. Only rows added during review are
 * checked: a detected row's name came from the repository and is not editable.
 */
export function rowProblems(rows: VariableRow[]): Record<string, string> {
  const problems: Record<string, string> = {};
  const detected = new Set(rows.filter((r) => r.original !== undefined).map((r) => r.key));
  const seen = new Set<string>();
  for (const row of rows) {
    if (row.original !== undefined) continue;
    const key = row.key.trim();
    if (!key) {
      if (row.value !== '') problems[row.id] = 'Give this variable a name.';
      continue;
    }
    if (!ENV_KEY.test(key)) {
      problems[row.id] = 'A name uses letters, digits and underscores, and does not start with a digit.';
    } else if (detected.has(key)) {
      problems[row.id] = `${key} is already in the list above. Set its value there.`;
    } else if (seen.has(key)) {
      problems[row.id] = `${key} is in the list twice.`;
    }
    seen.add(key);
  }
  return problems;
}
