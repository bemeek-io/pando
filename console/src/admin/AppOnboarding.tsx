// Onboarding a new app: Pando works out how to run it, live, and the page then
// turns into the review of that plan (R-102, R-103, R-105; design 08 §1.3).
//
// One page in three phases, and the layout morphs between them rather than
// cutting. While detection runs ("discovering") a terrain profile draws across
// the top, the headline says what Pando is doing, and each step lands in a list
// with what it found. When it finishes the headline becomes "Plan ready" and
// the summit mark lands ("ready"); a moment later the column widens, the steps
// fold behind a toggle, and the review rises below with a sticky bar to reject,
// accept, or accept and deploy ("done"). Someone opening an app whose detection
// already finished lands on "done" with nothing animating.
//
// AI appears only where it did something (R-336): an AI step in the list when
// an adapter was called, questions it answered grouped under "Check what AI
// filled in" with its reason and a way back to the suggestion, and a star on
// whatever else it changed. Everything it changed is shown with its reason, and
// what Pando refused is shown too (R-331, R-334). A failed trial run's output
// stays on the page whatever a repair did (R-107).
//
// Reading is app.view. Answering and accepting are app.spec.edit; deploying is
// app.deploy; rejecting is app.delete; a secret value is app.secrets.write.
// Without a verb its control is absent or read-only (R-261).

import './onboarding.css';

import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  Badge,
  Button,
  Card,
  Checkbox,
  CodeBlock,
  Dialog,
  Icon,
  IconButton,
  Input,
  Radio,
  Skeleton,
  SkeletonText,
  StatusIndicator,
  StatusSymbol,
  Tag,
} from '@design';

import { api, RequestFailed } from '@api/client';
import type { Amendment, AppSpec, Question, Source } from '@api/types.gen';
import { Loading } from '../ui/Loading';
import { deletePath } from './delete-app';
import { Blocked, DetectionFailed, Failure, fills, type DetectionResponse, type Slot } from './DetectionReview';
import {
  aiFrom,
  answerFor,
  answerState,
  askedQuestions,
  currentStep,
  discoverySteps,
  groupOf,
  listNames,
  planSpec,
  progress,
  questionName,
  slotLabel,
  stillNeeded,
  stops,
  strategyLabel,
  type DiscoveryStep,
  type Finding,
} from './discovery';
import {
  SCREENING_ADVISORY,
  envKey,
  mergeRows,
  primaryWorkload,
  rowProblems,
  suggestions,
  valuesPayload,
  variableRows,
  type RowEdit,
  type Suggestions,
  type VariableRow,
} from './onboarding';
import { PHRASE_MS, phrasesFor } from './onboardingPhrases';
import { describeAmendment } from './screeningText';
import { Terrain } from './Terrain';
import { AppVerb, can, type AppWithVerbs } from './verbs';

export interface DeployRefusal {
  message: string;
  remedy?: string;
  code?: string;
}

type Phase = 'discovering' | 'ready' | 'done';

/** How long "Plan ready" holds before the review rises (design handoff). */
const READY_HOLD_MS = 2_400;

export function AppOnboarding({
  app,
  onBack,
  onDeployRefused,
}: {
  app: AppWithVerbs;
  /** Back to the list: the back button, and where a rejected app leaves to. */
  onBack: () => void;
  /**
   * A deploy refused after the accept went through. The app is configured by
   * then and this page gives way to the ordinary app screen, so the refusal is
   * handed to that screen to show under its header, with the way to the fix.
   */
  onDeployRefused: (refusal: DeployRefusal | null) => void;
}) {
  const appID = app.id;
  const queries = useQueryClient();
  const verbs = app.verbs ?? [];
  const canEdit = can(verbs, AppVerb.SpecEdit);
  const canDeploy = can(verbs, AppVerb.Deploy);
  const canDelete = can(verbs, AppVerb.Delete);
  const canSecrets = can(verbs, AppVerb.SecretsWrite);
  const reduced = useReducedMotion();

  // The same key DetectionReview uses, so the two never disagree about one app.
  const detection = useQuery({
    queryKey: ['apps', appID, 'detection'],
    queryFn: () => api.get<DetectionResponse>(`/apps/${appID}/detection`),
    // Polled while running: the page builds from whatever has been found so far.
    refetchInterval: (query) => (query.state.data?.status === 'running' ? 2_000 : false),
  });

  // The variables form. Edits are held by variable, not as a copy of the rows,
  // so a poll that brings in more of the proposal never drops what somebody
  // typed (onboarding.ts mergeRows).
  const [edits, setEdits] = useState<Record<string, RowEdit>>({});
  const [added, setAdded] = useState<VariableRow[]>([]);
  const nextRow = useRef(0);

  // Answers typed but not yet saved, by question key.
  const [drafts, setDrafts] = useState<Record<string, string>>({});

  const answer = useMutation({
    mutationFn: (answers: Record<string, string>) => api.post(`/apps/${appID}/detection/answers`, { answers }),
    onSuccess: (_, answers) => {
      setDrafts((all) => {
        const next = { ...all };
        for (const key of Object.keys(answers)) delete next[key];
        return next;
      });
      return queries.invalidateQueries({ queryKey: ['apps', appID, 'detection'] });
    },
  });

  // R-022: looking again is explicit. Offered on a failure, which is when
  // somebody has just fixed what caused it.
  const rerun = useMutation({
    mutationFn: () => api.post(`/apps/${appID}/detection/rerun`),
    onSuccess: () => queries.invalidateQueries({ queryKey: ['apps', appID, 'detection'] }),
  });

  const [rejecting, setRejecting] = useState(false);
  const reject = useMutation({
    mutationFn: async () => {
      try {
        await api.del(deletePath(appID, 'none'));
      } catch (error) {
        // A just-added app has deployed nothing, so no storage of it holds any
        // data. If the server still asks for R-204's backup decision — storage
        // was declared, so it cannot tell — the answer is to discard, because
        // there is nothing to back up. Asked only when the server requires it,
        // so an app with no storage is not recorded as a forced delete.
        if (error instanceof RequestFailed && error.code === 'STATE_BACKUP_DECISION_REQUIRED') {
          await api.del(deletePath(appID, 'discard'));
          return;
        }
        throw error;
      }
    },
    onSuccess: () => {
      // As DeleteApp does: exact, then remove, so the list refetches and the
      // deleted app's own record is not asked for on the way out.
      void queries.invalidateQueries({ queryKey: ['apps'], exact: true });
      queries.removeQueries({ queryKey: ['apps', appID] });
      onBack();
    },
  });

  const data = detection.data;
  // While fetching, the body may carry nothing but the stage — or, from a
  // server that has not written one yet, nothing at all. Both are an empty
  // proposal still being found, not a failure.
  const proposal = data ? (data.detection ?? ({} as DetectionResponse['detection'])) : undefined;
  const status = data?.status ?? 'running';
  const running = status === 'running';
  const failed = status === 'failed';
  const blocked = status === 'blocked';
  const saved = data?.answers ?? null;

  // --- Phase ---------------------------------------------------------------
  // "Ready" is held only when this page watched detection finish. Opening an
  // app that finished earlier goes straight to the review.
  const [holding, setHolding] = useState(false);
  const wasRunning = useRef(false);
  useEffect(() => {
    if (!data) return;
    if (running) {
      wasRunning.current = true;
      return;
    }
    if (wasRunning.current) {
      wasRunning.current = false;
      if (reduced || failed || blocked) return;
      setHolding(true);
      const timer = window.setTimeout(() => setHolding(false), READY_HOLD_MS);
      return () => window.clearTimeout(timer);
    }
  }, [data, running, failed, blocked, reduced]);
  const phase: Phase = !data || running ? 'discovering' : holding ? 'ready' : 'done';

  // --- Steps -----------------------------------------------------------------
  const steps = useMemo(
    () =>
      proposal
        ? discoverySteps({ status, stage: proposal.stage, proposal, source: app.source, commit: data?.commit })
        : [],
    [proposal, status, app.source, data?.commit],
  );
  const current = currentStep(steps);

  // When the current step was first seen, for easing the terrain forward.
  const since = useRef<{ id: string; at: number }>({ id: '', at: Date.now() });
  if (current && since.current.id !== current.id) since.current = { id: current.id, at: Date.now() };
  const fraction = useCallback(
    () => (running ? progress(steps, Date.now() - since.current.at) : failed ? progress(steps, 0) : 1),
    [running, failed, steps],
  );

  const edit = (row: VariableRow, patch: RowEdit & { key?: string }) => {
    if (row.original === undefined) {
      setAdded((list) => list.map((r) => (r.id === row.id ? { ...r, ...patch } : r)));
    } else {
      setEdits((all) => ({ ...all, [row.id]: { ...all[row.id], ...patch } }));
    }
  };

  const spec = proposal?.draft_spec as AppSpec | undefined;
  const detected = variableRows(spec).map((row) => (canSecrets ? row : { ...row, secret: false }));
  const rows = mergeRows(detected, edits, added);
  const problems = rowProblems(rows);
  const asked = proposal ? askedQuestions(proposal) : [];
  const missing = stillNeeded(asked, saved, drafts);

  // Answers typed and not yet saved go with the accept, so nobody has to know
  // that a text answer saves when the field loses focus.
  const unsaved = () => {
    const out: Record<string, string> = {};
    for (const q of asked) {
      const typed = drafts[q.key];
      if (typed !== undefined && typed.trim() && typed !== answerFor(q, saved)) out[q.key] = typed.trim();
    }
    return out;
  };

  const accept = useMutation({
    mutationFn: async (andDeploy: boolean) => {
      const pending = unsaved();
      if (Object.keys(pending).length > 0) {
        await api.post(`/apps/${appID}/detection/answers`, { answers: pending });
      }
      await api.post(`/apps/${appID}/detection/accept`, { values: valuesPayload(rows) });
      if (!andDeploy) return;
      // Accepting pins a spec and does not deploy it (Sequence A). The deploy is
      // its own request, made only once the accept has gone through, so a
      // refused deploy leaves a configured app rather than nothing.
      try {
        await api.post(`/apps/${appID}/deployments`, {});
        onDeployRefused(null);
      } catch (error) {
        const refused = error instanceof RequestFailed ? error : null;
        onDeployRefused({
          message:
            refused?.message ??
            'Pando saved the configuration but couldn’t start a deploy. Deploy it from the app’s overview.',
          remedy: refused?.remedy,
          code: refused?.code,
        });
      }
    },
    // ['apps']: the app leaves draft, which changes both this screen — it gives
    // way to the ordinary one, with tabs — and the app's row in the list.
    onSuccess: () => queries.invalidateQueries({ queryKey: ['apps'] }),
  });

  const rejectDialog = (
    <Dialog
      open={rejecting}
      title={`Reject ${app.name}?`}
      description="Pando removes the app and everything it found. Nothing was deployed."
      onClose={() => setRejecting(false)}
      footer={
        <>
          <Button variant="ghost" onClick={() => setRejecting(false)}>
            Cancel
          </Button>
          <Button variant="destructive" disabled={reject.isPending} onClick={() => reject.mutate()}>
            {reject.isPending ? 'Rejecting' : 'Reject plan'}
          </Button>
        </>
      }
    >
      {reject.isError && <Failure error={reject.error} />}
    </Dialog>
  );

  const back = (
    <div style={{ display: 'flex', alignItems: 'center', gap: 'var(--space-3)' }}>
      <Button variant="ghost" icon={<Icon name="arrow-left" />} onClick={onBack}>
        Apps
      </Button>
      <span style={{ font: 'var(--type-label)', color: 'var(--ink)' }}>{app.name}</span>
    </div>
  );

  // Before the first answer: the terrain's place and the column's shape.
  if (detection.isPending) {
    return (
      <div>
        <div className="pando-terrain" aria-hidden="true" />
        <div className="pando-onboard-column">
          {back}
          <Loading gap="var(--space-5)">
            <Skeleton width="16rem" height="2rem" radius="sm" />
            <SkeletonText lines={4} />
          </Loading>
        </div>
      </div>
    );
  }

  if (detection.isError || !data || !proposal) {
    return (
      <div>
        <div className="pando-onboard-column">
          {back}
          <Failure error={detection.error} />
        </div>
        {canDelete && (
          <ActionBar status="failed" label="Pando couldn’t load this app" detail="Reject it, or reload the page to try again.">
            <Button variant="ghost" onClick={() => setRejecting(true)}>
              Reject plan
            </Button>
          </ActionBar>
        )}
        {rejectDialog}
      </div>
    );
  }

  const outcome = proposal.screening;
  const marks = suggestions(outcome, spec);
  const done = phase === 'done';
  const aiSteps = steps.filter((s) => s.ai).length;
  const seed = `${app.source?.url ?? app.source?.image ?? app.id}@${data.commit || proposal.commit || ''}`;

  const headline = failed
    ? 'Pando stopped'
    : blocked
      ? 'This can’t run as written'
      : phase === 'discovering'
        ? (current?.active ?? 'Reading the repo')
        : 'Plan ready';

  const effective: Record<string, string> = {};
  for (const q of asked) effective[q.key] = answerFor(q, saved, drafts[q.key]);
  const plan = planSpec(proposal, effective) ?? spec;

  return (
    <div>
      <Terrain
        seed={seed}
        fraction={fraction}
        aiFrom={aiFrom(steps)}
        stops={stops(steps)}
        summit={!running && !failed && !blocked}
        moving={running}
      />

      <div className="pando-onboard-column" data-phase={done ? 'done' : 'working'}>
        <div style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-2)', minWidth: 0 }}>
          {back}
          <SourceTags source={app.source} commit={data.commit || proposal.commit} />
          <div style={{ display: 'flex', alignItems: 'center', gap: 'var(--space-2)' }}>
            {running && current?.ai && <AiStar size={18} />}
            <h2 className="pando-onboard-headline" data-big={!running && !failed && !blocked}>
              {headline}
            </h2>
          </div>
          {running && <WorkingLine step={current} startedAt={data.started_at} reduced={reduced} />}
          {!running && !failed && !blocked && (
            <p
              className="pando-onboard-enter"
              style={{ margin: 0, font: 'var(--type-body)', color: 'var(--ink-secondary)', maxWidth: '60ch' }}
            >
              {subline(outcome, asked, missing.length, marks)}
            </p>
          )}
        </div>

        <Segments steps={steps} hidden={done} failedFixed={Boolean(outcome?.ran && outcome.function === 'repair_plan')} />

        <Tallies spec={spec} running={running} steps={steps} open={missing.length} reached={Boolean(spec)} />

        <section style={{ display: 'flex', flexDirection: 'column' }}>
          {done ? (
            <StepLog steps={steps} aiSteps={aiSteps} />
          ) : (
            <StepList steps={steps.filter((s) => s.state !== 'pending')} />
          )}
        </section>

        {failed && (
          <div className="pando-onboard-enter">
            <DetectionFailed
              error={proposal.error}
              onRetry={canEdit ? () => rerun.mutate() : undefined}
              retrying={rerun.isPending}
            />
          </div>
        )}
        {blocked && (
          <div className="pando-onboard-enter">
            <Blocked proposal={proposal} />
          </div>
        )}

        {done && !failed && !blocked && (
          <div
            className="pando-onboard-enter"
            style={{ '--stagger': 1, display: 'flex', flexDirection: 'column', gap: 'var(--space-7)' } as React.CSSProperties}
          >
            <Notices proposal={proposal} spec={spec} marks={marks} />

            {(['needs', 'ai'] as const).map((group) => {
              const items = asked.filter((q) => groupOf(q) === group);
              if (items.length === 0) return null;
              return (
                <section key={group} style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-4)' }}>
                  <GroupHeading
                    title={group === 'ai' ? 'Check what AI filled in' : 'Needs your answer'}
                    count={items.length}
                    ai={group === 'ai'}
                  >
                    {group === 'ai'
                      ? 'AI answered these from the code. Change anything that looks wrong.'
                      : outcome?.ran
                        ? 'Pando and AI couldn’t work these out from the code. Copy a question into the tool that wrote this app and paste its answer back.'
                        : 'Pando couldn’t work these out from the code. Copy a question into the tool that wrote this app and paste its answer back.'}
                  </GroupHeading>
                  {items.map((q) => (
                    <QuestionCard
                      key={q.key}
                      question={q}
                      value={answerFor(q, saved, drafts[q.key])}
                      saving={answer.isPending && Boolean(answer.variables?.[q.key] !== undefined)}
                      error={answer.isError && answer.variables?.[q.key] !== undefined ? answer.error : undefined}
                      onType={canEdit ? (value) => setDrafts((all) => ({ ...all, [q.key]: value })) : undefined}
                      onSave={
                        canEdit
                          ? (value) => {
                              if (value.trim() && value !== ((saved ?? {})[q.key] ?? q.suggested?.value)) {
                                answer.mutate({ [q.key]: value.trim() });
                              }
                            }
                          : undefined
                      }
                    />
                  ))}
                </section>
              );
            })}

            {spec && (
              <Variables
                rows={rows}
                spec={spec}
                problems={problems}
                marks={marks}
                canEdit={canEdit}
                canSecrets={canSecrets}
                onEdit={edit}
                onAdd={() => {
                  nextRow.current += 1;
                  setAdded((list) => [
                    ...list,
                    { id: `added-${nextRow.current}`, workload: '', key: '', value: '', secret: false },
                  ]);
                }}
                onRemove={(row) => setAdded((list) => list.filter((r) => r.id !== row.id))}
              />
            )}

            {outcome?.ran && <AiNotes notes={outcome.notes ?? []} />}

            {plan && <ThePlan spec={plan} marks={marks} answers={effective} source={app.source} commit={data.commit} />}
          </div>
        )}
      </div>

      {done && !failed && !blocked && (canEdit || canDelete) && (
        <ActionBar {...barStatus(missing, Object.keys(problems).length, canDeploy, canEdit)} error={accept.isError ? accept.error : undefined}>
          {canDelete && (
            <Button variant="ghost" onClick={() => setRejecting(true)}>
              Reject plan
            </Button>
          )}
          {canEdit && (
            <Button
              variant={canDeploy ? 'secondary' : 'primary'}
              disabled={missing.length > 0 || Object.keys(problems).length > 0 || accept.isPending}
              onClick={() => accept.mutate(false)}
            >
              {accept.isPending && accept.variables === false ? 'Accepting' : 'Accept plan'}
            </Button>
          )}
          {canEdit && canDeploy && (
            <Button
              variant="primary"
              disabled={missing.length > 0 || Object.keys(problems).length > 0 || accept.isPending}
              onClick={() => accept.mutate(true)}
            >
              {accept.isPending && accept.variables === true ? 'Accepting' : 'Accept and deploy'}
            </Button>
          )}
        </ActionBar>
      )}

      {(failed || blocked) && canDelete && (
        <ActionBar
          status="failed"
          label={failed ? 'Detection stopped' : 'Pando can’t run this app'}
          detail={failed ? 'Fix what stopped it and try again, or reject the app.' : 'Change what’s described above, or reject the app.'}
        >
          <Button variant="ghost" onClick={() => setRejecting(true)}>
            Reject plan
          </Button>
        </ActionBar>
      )}

      {rejectDialog}
    </div>
  );
}

// --- Header ------------------------------------------------------------------

function SourceTags({ source, commit }: { source: Source | undefined; commit?: string }) {
  if (!source) return null;
  const short = commit ? commit.slice(0, 7) : undefined;
  const repo = source.url ? repoName(source.url) : undefined;
  return (
    <div style={{ display: 'flex', flexWrap: 'wrap', gap: 'var(--space-2)' }}>
      {source.type === 'image' && source.image && <Tag mono>{source.image}</Tag>}
      {source.type === 'upload' && <Tag>An uploaded archive</Tag>}
      {repo && (
        <Tag mono icon={<Icon name="git-branch" size={12} />} title={source.url}>
          {repo}
        </Tag>
      )}
      {source.ref && <Tag mono>{source.ref}</Tag>}
      {source.subdir && <Tag mono>{source.subdir}</Tag>}
      {short && <Tag mono>{short}</Tag>}
    </div>
  );
}

/** `owner/repo` from a clone URL, which is how people name a repository. */
function repoName(url: string): string {
  const cleaned = url.replace(/\.git$/, '').replace(/\/+$/, '');
  const parts = cleaned.split(/[/:]/).filter(Boolean);
  return parts.length >= 2 ? `${parts[parts.length - 2]}/${parts[parts.length - 1]}` : cleaned;
}

/**
 * What Pando is doing, live: a ripple, a phrase that turns over, and how long
 * it has been at it. Water blue while AI works. Announced politely; under
 * reduced motion the phrase holds still.
 */
function WorkingLine({
  step,
  startedAt,
  reduced,
}: {
  step: DiscoveryStep | undefined;
  startedAt?: string;
  reduced: boolean;
}) {
  const [now, setNow] = useState(() => Date.now());
  const [tick, setTick] = useState(0);
  const id = step?.ai ? (step.name.startsWith('Review') ? 'repair' : 'answer') : step?.id;
  const phrases = phrasesFor(id);

  useEffect(() => {
    const clock = window.setInterval(() => setNow(Date.now()), 1_000);
    if (reduced) return () => window.clearInterval(clock);
    const phrase = window.setInterval(() => setTick((n) => n + 1), PHRASE_MS);
    return () => {
      window.clearInterval(clock);
      window.clearInterval(phrase);
    };
  }, [reduced]);

  // Each step starts from its first, literal phrase.
  useEffect(() => setTick(0), [id]);

  const began = startedAt ? Date.parse(startedAt) : NaN;
  const seconds = Number.isNaN(began) ? undefined : Math.max(0, Math.floor((now - began) / 1_000));
  const phrase = phrases[(reduced ? 0 : tick) % phrases.length];
  const color = step?.ai ? 'var(--water)' : 'var(--ink-secondary)';

  return (
    <div role="status" aria-live="polite" style={{ display: 'flex', alignItems: 'center', gap: 'var(--space-3)' }}>
      <span className="pando-ripple" data-ai={Boolean(step?.ai)} aria-hidden="true">
        <span />
        <span />
        <span />
        <i />
      </span>
      <span key={phrase} className="pando-onboard-enter" style={{ font: 'var(--type-body-ui)', color }}>
        {phrase}
        <span className="pando-dots" aria-hidden="true">
          <span>.</span>
          <span>.</span>
          <span>.</span>
        </span>
      </span>
      {seconds !== undefined && (
        <span style={{ font: 'var(--type-code-sm)', color: 'var(--ink-muted)' }}>{seconds}s</span>
      )}
    </div>
  );
}

function subline(
  outcome: DetectionResponse['detection']['screening'],
  asked: Question[],
  missing: number,
  marks: Suggestions,
): string {
  const parts: string[] = [];
  if (outcome?.ran) {
    const answered = asked.filter((q) => q.suggested).length;
    const changed = marks.count - Object.keys(outcome.answers ?? {}).length;
    if (outcome.function === 'repair_plan') {
      parts.push(
        changed > 0
          ? `AI changed ${changed === 1 ? 'one thing' : `${changed} things`} after the plan failed.`
          : 'AI looked at the failed plan and found nothing it could change.',
      );
    } else if (answered > 0) {
      parts.push(`AI answered ${answered === 1 ? 'one question' : `${answered} questions`}.`);
    }
  }
  parts.push(
    missing > 0
      ? `Pando needs ${missing === 1 ? 'one answer' : `${missing} answers`} from you before it can deploy.`
      : 'Everything’s answered. Nothing runs until you accept.',
  );
  return parts.join(' ');
}

/** One segment per step: ink when done, water for AI, the rule for what's left. */
function Segments({ steps, hidden, failedFixed }: { steps: DiscoveryStep[]; hidden: boolean; failedFixed: boolean }) {
  return (
    <div className="pando-segments" data-hidden={hidden} aria-hidden="true">
      {steps.map((s) => (
        <span
          key={s.id}
          style={{
            background:
              s.state === 'done'
                ? s.failed
                  ? failedFixed
                    ? 'var(--contour-text)'
                    : 'var(--status-failed)'
                  : s.ai
                    ? 'var(--water)'
                    : 'var(--ink)'
                : s.state === 'current'
                  ? 'var(--rule-strong)'
                  : 'var(--rule)',
          }}
        />
      ))}
    </div>
  );
}

function Tallies({
  spec,
  running,
  steps,
  open,
  reached,
}: {
  spec: AppSpec | undefined;
  running: boolean;
  steps: DiscoveryStep[];
  open: number;
  reached: boolean;
}) {
  const workloads = reached ? (spec?.workloads ?? []).length : 0;
  const services = reached ? (spec?.slots ?? []).length : 0;
  const variables = reached
    ? new Set((spec?.workloads ?? []).flatMap((w) => (w.env ?? []).map((e) => e.key))).size
    : 0;
  const trial = steps.find((s) => s.id === 'trial');
  const broken = running && trial?.failed;
  const tallies: Array<{ label: string; value: number; hot?: boolean }> = [
    { label: workloads === 1 ? 'Process' : 'Processes', value: workloads },
    { label: services === 1 ? 'Service' : 'Services', value: services },
    { label: variables === 1 ? 'Variable' : 'Variables', value: variables },
    broken ? { label: 'Run failed', value: 1, hot: true } : { label: 'Questions for you', value: open },
  ];
  return (
    <div
      style={{
        display: 'grid',
        gridTemplateColumns: 'repeat(4, minmax(0, 1fr))',
        borderTop: 'var(--border-width) solid var(--rule)',
        borderBottom: 'var(--border-width) solid var(--rule)',
      }}
    >
      {tallies.map((t) => (
        <div
          key={t.label}
          style={{ display: 'flex', flexDirection: 'column', gap: 'calc(var(--space-1) / 2)', padding: 'var(--space-3) var(--space-3) var(--space-3) 0' }}
        >
          <span
            style={{
              font: 'var(--type-h3)',
              fontVariantNumeric: 'tabular-nums',
              color: t.hot ? 'var(--status-failed)' : t.value ? 'var(--ink)' : 'var(--ink-muted)',
            }}
          >
            {t.value}
          </span>
          <span style={{ font: 'var(--type-caption)', color: 'var(--ink-secondary)' }}>{t.label}</span>
        </div>
      ))}
    </div>
  );
}

// --- Steps -------------------------------------------------------------------

/** Newest on top while Pando works: the step under way is where the eye is. */
function StepList({ steps }: { steps: DiscoveryStep[] }) {
  return (
    <ol style={{ display: 'flex', flexDirection: 'column-reverse', margin: 0, padding: 0, listStyle: 'none' }}>
      {steps.map((s) => (
        <StepRow key={s.id} step={s} />
      ))}
    </ol>
  );
}

function StepLog({ steps, aiSteps }: { steps: DiscoveryStep[]; aiSteps: number }) {
  const [open, setOpen] = useState(false);
  const summary = `${steps.length} steps${aiSteps ? `, ${aiSteps} by AI` : ''}`;
  return (
    <>
      <button
        type="button"
        onClick={() => setOpen(!open)}
        aria-expanded={open}
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: 'var(--space-3)',
          border: 'none',
          background: 'transparent',
          padding: '0 0 var(--space-2)',
          cursor: 'pointer',
          textAlign: 'left',
          color: 'var(--ink)',
        }}
      >
        <span style={{ font: 'var(--type-label)' }}>How Pando got here</span>
        <span style={{ font: 'var(--type-caption)', color: 'var(--ink-secondary)' }}>{summary}</span>
        <span
          style={{ marginLeft: 'auto', display: 'inline-flex', alignItems: 'center', gap: 'var(--space-1)', color: 'var(--ink-secondary)' }}
        >
          {open ? 'Hide' : 'Show'}
          <Icon name={open ? 'chevron-up' : 'chevron-down'} size={16} />
        </span>
      </button>
      {open && <StepList steps={steps} />}
    </>
  );
}

function StepRow({ step }: { step: DiscoveryStep }) {
  const reached = step.state !== 'pending';
  const nameColor = step.ai ? 'var(--water)' : reached ? 'var(--ink)' : 'var(--ink-muted)';
  return (
    <li className="pando-step pando-onboard-enter" data-ai={Boolean(step.ai)} aria-current={step.state === 'current' ? 'step' : undefined}>
      <div className="pando-step-head">
        <span style={{ display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
          {step.ai ? (
            <AiStar size={16} />
          ) : (
            <StatusSymbol status={step.state === 'current' ? 'building' : step.failed ? 'failed' : 'running'} size={8} />
          )}
        </span>
        <span
          style={{
            display: 'flex',
            alignItems: 'center',
            gap: 'var(--space-2)',
            minWidth: 0,
            font: step.state === 'current' ? 'var(--type-label)' : 'var(--type-body-ui)',
            color: nameColor,
          }}
        >
          {step.name}
          {step.ai && <span className="pando-ai-label">AI</span>}
        </span>
        {step.result && (
          <span
            className="pando-onboard-enter"
            style={{ font: 'var(--type-caption)', color: step.failed ? 'var(--ink)' : 'var(--ink-secondary)', textAlign: 'right' }}
          >
            {step.result}
          </span>
        )}
      </div>
      {step.findings.length > 0 && (
        <div className="pando-step-findings">
          {step.findings.map((f, i) => (
            <FindingLine key={`${f.key}-${i}`} finding={f} ai={Boolean(step.ai)} stagger={i} />
          ))}
        </div>
      )}
    </li>
  );
}

function FindingLine({ finding, ai, stagger }: { finding: Finding; ai: boolean; stagger: number }) {
  return (
    <div className="pando-finding pando-onboard-enter" style={{ '--stagger': stagger } as React.CSSProperties}>
      <span style={{ font: 'var(--type-caption)', lineHeight: 'var(--space-5)', color: ai ? 'var(--water)' : 'var(--ink-secondary)' }}>
        {finding.key}
      </span>
      <span
        style={{
          font: finding.text ? 'var(--type-body-ui)' : 'var(--type-code-sm)',
          lineHeight: 'var(--space-5)',
          color: 'var(--ink)',
          overflowWrap: 'anywhere',
        }}
      >
        {finding.value}
      </span>
    </div>
  );
}

// --- Review --------------------------------------------------------------------

/**
 * What somebody should know before accepting, one row each between rules: what
 * the trial run showed and its output (R-107), what AI changed and what Pando
 * refused of it (R-334), an AI adapter that did not run, and the plan's
 * warnings. No tinted banners.
 */
function Notices({
  proposal,
  spec,
  marks,
}: {
  proposal: DetectionResponse['detection'];
  spec: AppSpec | undefined;
  marks: Suggestions;
}) {
  const outcome = proposal.screening;
  const trial = proposal.trial;
  const rows: React.ReactNode[] = [];
  const changed = (outcome?.applied ?? []).filter((a) => a.amendment.kind !== 'answer_question');

  if (outcome?.ran && outcome.function === 'repair_plan' && changed.length > 0) {
    rows.push(
      <NoticeRow key="repair" ai title="AI changed the plan after it failed.">
        {`${changed.map((c) => describeAmendment(c.amendment)).join('. ')}. Each change is marked below.`}
      </NoticeRow>,
    );
  }

  if (trial?.ran) {
    const ports = trial.observed_ports ?? [];
    rows.push(
      <NoticeRow
        key="trial"
        status={trial.crashed || !trial.started ? 'failed' : 'running'}
        title={trial.crashed ? 'The trial run failed.' : !trial.started ? 'The app didn’t start in a trial run.' : 'The app started in a trial run.'}
        detail={proposal.trial_log ? <CodeBlock title="Trial run" lines={proposal.trial_log} /> : undefined}
        detailLabel="output"
      >
        {trial.crashed
          ? 'Pando started the app and it exited with an error. Its output shows why.'
          : ports.length
            ? `It opened ${ports.length === 1 ? 'port' : 'ports'} ${ports.join(', ')}.`
            : 'It opened no ports.'}
      </NoticeRow>,
    );
  }

  if (outcome && !outcome.ran && outcome.skip_code && !['not_configured', 'not_needed', 'blocked'].includes(outcome.skip_code)) {
    rows.push(
      <NoticeRow key="skipped" status="info" title="AI didn’t run.">
        {outcome.skipped}
      </NoticeRow>,
    );
  }

  for (const warning of spec?.warnings ?? []) {
    const ai = warning.code === SCREENING_ADVISORY;
    const amendment = ai ? marks.warnings[warning.message.trim()] : undefined;
    rows.push(
      <NoticeRow key={warning.code + warning.message} ai={ai} status="info" title="">
        {warning.message}
        {amendment?.reason ? ` ${amendment.reason}` : ''}
      </NoticeRow>,
    );
  }

  const refused = outcome?.ran ? (outcome.refused ?? []) : [];
  if (refused.length > 0) {
    rows.push(
      <NoticeRow
        key="refused"
        ai
        title={refused.length === 1 ? 'Pando didn’t apply one AI suggestion.' : `Pando didn’t apply ${refused.length} AI suggestions.`}
        detail={
          <ul style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-3)', margin: 0, padding: 0, listStyle: 'none' }}>
            {refused.map((r, i) => (
              <li key={`${r.amendment.kind}-${i}`} style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-1)' }}>
                <span style={{ font: 'var(--type-body-ui)', color: 'var(--ink)' }}>{describeAmendment(r.amendment)}</span>
                <span style={{ font: 'var(--type-caption)', color: 'var(--ink-secondary)' }}>{r.reason}</span>
              </li>
            ))}
          </ul>
        }
        detailLabel="them"
      >
        None of them are in the plan.
      </NoticeRow>,
    );
  }

  if (rows.length === 0) return null;
  return <div style={{ display: 'flex', flexDirection: 'column' }}>{rows}</div>;
}

function NoticeRow({
  ai,
  status,
  title,
  detail,
  detailLabel,
  children,
}: {
  ai?: boolean;
  status?: 'running' | 'failed' | 'info';
  title: string;
  detail?: React.ReactNode;
  detailLabel?: string;
  children: React.ReactNode;
}) {
  const [open, setOpen] = useState(false);
  return (
    <div
      style={{
        display: 'grid',
        gridTemplateColumns: 'var(--space-4) minmax(0, 1fr)',
        gap: 'var(--space-3)',
        padding: 'var(--space-3) 0',
        borderTop: 'var(--border-width) solid var(--rule)',
        alignItems: 'baseline',
      }}
    >
      <span style={{ display: 'flex', alignItems: 'center', justifyContent: 'center', height: 'var(--space-5)' }}>
        {ai ? <AiStar size={14} /> : <StatusSymbol status={status ?? 'info'} size={8} />}
      </span>
      <div style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-3)', minWidth: 0 }}>
        <span style={{ color: 'var(--ink-secondary)', textWrap: 'pretty' } as React.CSSProperties}>
          {title && <span style={{ color: 'var(--ink)', fontWeight: 500 }}>{title} </span>}
          {children}
          {detail && (
            <>
              {' '}
              <button type="button" className="pando-link" onClick={() => setOpen(!open)} aria-expanded={open}>
                {open ? `Hide ${detailLabel}` : `Show ${detailLabel}`}
              </button>
            </>
          )}
        </span>
        {open && detail}
      </div>
    </div>
  );
}

function GroupHeading({
  title,
  count,
  ai,
  children,
}: {
  title: string;
  count: number;
  ai?: boolean;
  children: React.ReactNode;
}) {
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-1)' }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 'var(--space-2)' }}>
        {ai && <AiStar size={20} />}
        <h3 style={{ font: 'var(--type-h3)', margin: 0 }}>{title}</h3>
        <Badge count={count} />
      </div>
      <span style={{ color: 'var(--ink-secondary)', textWrap: 'pretty' } as React.CSSProperties}>{children}</span>
    </div>
  );
}

/**
 * One question. The prompt is verbatim with a copy button beside it (R-105).
 * A choice saves the moment it is picked; typed text saves when the field is
 * left, or with the accept. An AI answer shows its reason and the files it
 * rests on, and once changed offers the suggestion back (R-338).
 */
function QuestionCard({
  question,
  value,
  saving,
  error,
  onType,
  onSave,
}: {
  question: Question;
  value: string;
  saving: boolean;
  error?: unknown;
  /** Absent for somebody who may read the question but not answer it. */
  onType?: (value: string) => void;
  onSave?: (value: string) => void;
}) {
  const [copied, setCopied] = useState(false);
  const state = answerState(question, value);
  const suggested = question.suggested;
  const label = saving
    ? 'Saving'
    : state === 'needed'
      ? 'Needs an answer'
      : state === 'ai'
        ? 'AI answered'
        : 'Answered';
  const tone = saving || state === 'needed' ? 'building' : state === 'ai' ? 'info' : 'running';

  async function copy() {
    // The prompt exactly, and nothing else. Anything prepended would arrive in
    // the assistant as part of the question.
    await navigator.clipboard.writeText(question.prompt);
    setCopied(true);
    window.setTimeout(() => setCopied(false), 2_000);
  }

  const choice = question.kind === 'choice' && (question.options ?? []).length > 0;

  return (
    <Card tone="paper" padding="md">
      <div style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-4)' }}>
        <div style={{ display: 'flex', alignItems: 'flex-start', justifyContent: 'space-between', gap: 'var(--space-4)' }}>
          <div style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-1)', minWidth: 0 }}>
            {/* Verbatim. Not shortened, not re-worded, not split up. */}
            <span style={{ font: 'var(--type-label)', color: 'var(--ink)', textWrap: 'pretty' } as React.CSSProperties}>
              {question.prompt}
            </span>
            {question.why && (
              <span style={{ font: 'var(--type-caption)', color: 'var(--ink-secondary)', textWrap: 'pretty' } as React.CSSProperties}>
                {question.why}
              </span>
            )}
          </div>
          <div style={{ display: 'flex', alignItems: 'center', gap: 'var(--space-2)', flex: '0 0 auto' }}>
            <StatusIndicator status={tone} label={label} />
            <IconButton label={copied ? 'Copied' : 'Copy this question'} onClick={copy}>
              <Icon name={copied ? 'check' : 'copy'} size={16} />
            </IconButton>
          </div>
        </div>

        {choice ? (
          <div role="radiogroup" aria-label={question.prompt} style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-2)' }}>
            {(question.options ?? []).map((option) => (
              <Radio
                key={option}
                name={question.key}
                value={option}
                label={option}
                description={question.key === 'build_strategy' || question.key === 'build_method' ? strategyLabel(option) : undefined}
                checked={value === option}
                disabled={!onSave}
                onChange={() => {
                  onType?.(option);
                  onSave?.(option);
                }}
              />
            ))}
          </div>
        ) : (
          <Input
            aria-label={question.prompt}
            mono={question.kind !== 'text'}
            value={value}
            placeholder={question.kind === 'port' ? '3000' : undefined}
            disabled={!onType}
            onChange={(e) => onType?.(e.target.value)}
            onBlur={(e) => onSave?.(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter') onSave?.((e.target as HTMLInputElement).value);
            }}
          />
        )}

        {Boolean(error) && <Failure error={error} />}

        {suggested && state === 'ai' && (
          <div
            style={{
              display: 'grid',
              gridTemplateColumns: 'var(--space-4) minmax(0, 1fr)',
              gap: 'var(--space-2)',
              alignItems: 'start',
              paddingTop: 'var(--space-3)',
              borderTop: 'var(--border-width) solid var(--rule)',
            }}
          >
            <span style={{ display: 'flex', height: 'var(--space-4)', alignItems: 'center' }}>
              <AiStar size={14} />
            </span>
            <span style={{ font: 'var(--type-caption)', color: 'var(--ink-secondary)', textWrap: 'pretty' } as React.CSSProperties}>
              <span style={{ color: 'var(--water)', fontWeight: 500 }}>Why AI picked this.</span>{' '}
              {suggested.reason}
              {(suggested.evidence ?? []).length > 0 && (
                <> Based on {(suggested.evidence ?? []).map((f, i) => (
                  <span key={f}>
                    {i > 0 && ', '}
                    <code style={{ font: 'var(--type-code-sm)' }}>{f}</code>
                  </span>
                ))}.</>
              )}
            </span>
          </div>
        )}
        {suggested && state !== 'ai' && (
          <div
            style={{
              display: 'flex',
              flexWrap: 'wrap',
              gap: 'var(--space-2)',
              alignItems: 'center',
              paddingTop: 'var(--space-3)',
              borderTop: 'var(--border-width) solid var(--rule)',
              font: 'var(--type-caption)',
              color: 'var(--ink-secondary)',
            }}
          >
            <span>You changed the AI suggestion.</span>
            {onSave && (
              <button
                type="button"
                className="pando-link"
                onClick={() => {
                  onType?.(suggested.value);
                  onSave(suggested.value);
                }}
              >
                Use suggestion
              </button>
            )}
          </div>
        )}
      </div>
    </Card>
  );
}

function Variables({
  rows,
  spec,
  problems,
  marks,
  canEdit,
  canSecrets,
  onEdit,
  onAdd,
  onRemove,
}: {
  rows: VariableRow[];
  spec: AppSpec;
  problems: Record<string, string>;
  marks: Suggestions;
  canEdit: boolean;
  canSecrets: boolean;
  onEdit: (row: VariableRow, patch: RowEdit & { key?: string }) => void;
  onAdd: () => void;
  onRemove: (row: VariableRow) => void;
}) {
  const workloads = spec.workloads ?? [];
  const many = workloads.length > 1;
  const slots = (spec.slots ?? []) as Slot[];
  // A variable a dependency fills belongs to that dependency: shown, and not
  // editable here (onboarding.ts variableRows).
  const filled = workloads.flatMap((w) =>
    (w.env ?? []).filter((e) => e.slot_ref).map((e) => ({ workload: w.name, key: e.key, slot: e.slot_ref! })),
  );
  if (rows.length === 0 && filled.length === 0 && !canEdit) return null;

  return (
    <section style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-4)' }}>
      <div style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-1)' }}>
        <h3 style={{ font: 'var(--type-h3)', margin: 0 }}>Variables</h3>
        <span style={{ color: 'var(--ink-secondary)' }}>
          {rows.length + filled.length === 0
            ? 'Pando found no variables this app reads. Add any it needs.'
            : 'Change any value. Pando uses these on every deploy.'}
        </span>
      </div>

      {rows.length + filled.length > 0 && (
        <Card tone="paper" padding="none">
          {filled.map((f, i) => {
            const slot = slots.find((s) => s.key === f.slot);
            return (
              <VariableLine key={`slot-${f.workload}-${f.key}`} first={i === 0} name={f.key} workload={many ? f.workload : undefined}
                source={<span>From {slot ? slotLabel(slot.type) : 'a service'}</span>}
              >
                <span style={{ font: 'var(--type-caption)', color: 'var(--ink-secondary)' }}>
                  {slot ? fills(slot) : 'Pando fills this when it sets up the service.'}
                </span>
              </VariableLine>
            );
          })}
          {rows.map((row, i) => {
            const isNew = row.original === undefined;
            const ai = isNew ? undefined : marks.env[envKey(row.workload || primaryWorkload(spec), row.key)];
            const changed = !isNew && row.value !== row.original;
            let source: React.ReactNode;
            if (isNew) source = <span>Added by you</span>;
            else if (changed) {
              source = (
                <>
                  <span>Your value</span>
                  {canEdit && (
                    <button type="button" className="pando-link" onClick={() => onEdit(row, { value: row.original })}>
                      {row.original ? 'Use Pando’s value' : 'Clear'}
                    </button>
                  )}
                </>
              );
            } else if (ai) {
              source = (
                <span title={ai.reason} style={{ display: 'inline-flex', alignItems: 'center', gap: 'var(--space-2)' }}>
                  <AiStar size={12} />
                  Suggested by AI
                </span>
              );
            } else if (row.original) source = <span>Found in the repo</span>;
            else source = <span style={{ color: 'var(--ink)' }}>No value yet</span>;

            return (
              <VariableLine
                key={row.id}
                first={filled.length === 0 && i === 0}
                name={
                  isNew ? (
                    <Input
                      aria-label="Name"
                      placeholder="NAME"
                      mono
                      value={row.key}
                      error={problems[row.id]}
                      onChange={(e) => onEdit(row, { key: e.target.value })}
                    />
                  ) : (
                    row.key
                  )
                }
                workload={many && !isNew ? row.workload : undefined}
                source={source}
              >
                <div style={{ display: 'flex', alignItems: 'center', gap: 'var(--space-3)' }}>
                  <div style={{ flex: 1, minWidth: 0 }}>
                    <Input
                      aria-label={`Value of ${row.key || 'the new variable'}`}
                      mono
                      type={row.secret ? 'password' : 'text'}
                      autoComplete="off"
                      placeholder={row.secret ? 'Stored as a secret' : 'No value yet'}
                      value={row.value}
                      disabled={!canEdit}
                      onChange={(e) => onEdit(row, { value: e.target.value })}
                    />
                  </div>
                  <Checkbox
                    label="Secret"
                    checked={row.secret}
                    disabled={!canEdit || !canSecrets}
                    onChange={(e) => onEdit(row, { secret: e.target.checked })}
                  />
                  {isNew && canEdit && (
                    <IconButton label="Remove this variable" onClick={() => onRemove(row)}>
                      <Icon name="x" size={16} />
                    </IconButton>
                  )}
                </div>
              </VariableLine>
            );
          })}
        </Card>
      )}

      {canEdit && (
        <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'flex-start', gap: 'var(--space-2)' }}>
          <Button variant="secondary" onClick={onAdd}>
            Add variable
          </Button>
          <p style={{ font: 'var(--type-caption)', color: 'var(--ink-secondary)', margin: 0 }}>
            {canSecrets
              ? 'A secret is stored apart from the configuration and never appears in an export. Use it for keys and passwords.'
              : 'You can’t store secrets on this app, so every value here is saved in the configuration. Leave keys and passwords empty for somebody who can.'}
          </p>
        </div>
      )}
    </section>
  );
}

function VariableLine({
  first,
  name,
  workload,
  source,
  children,
}: {
  first: boolean;
  name: React.ReactNode;
  workload?: string;
  source: React.ReactNode;
  children: React.ReactNode;
}) {
  return (
    <div
      style={{
        display: 'flex',
        flexWrap: 'wrap',
        alignItems: 'center',
        gap: 'var(--space-2) var(--space-4)',
        padding: 'var(--space-3) var(--space-4)',
        borderTop: first ? 'none' : 'var(--border-width) solid var(--rule)',
      }}
    >
      <div style={{ flex: '1 1 15rem', minWidth: 0, display: 'flex', flexDirection: 'column', gap: 'calc(var(--space-1) / 2)' }}>
        <span style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 'var(--space-2)', font: 'var(--type-code-sm)', color: 'var(--ink)', overflowWrap: 'anywhere' }}>
          {name}
          {workload && <Tag mono>{workload}</Tag>}
        </span>
        <span
          style={{
            display: 'flex',
            alignItems: 'center',
            gap: 'var(--space-2)',
            flexWrap: 'wrap',
            font: 'var(--type-caption)',
            color: 'var(--ink-secondary)',
          }}
        >
          {source}
        </span>
      </div>
      <div style={{ flex: '2 1 20rem', minWidth: 0 }}>{children}</div>
    </div>
  );
}

/**
 * What the AI adapter said that changes nothing (design 10 §2: notes are never
 * warnings). After the variables and before the plan: read once the person has
 * settled the answers and values, as context for the plan below.
 */
function AiNotes({ notes }: { notes: string[] }) {
  if (notes.length === 0) return null;
  return (
    <section style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-4)' }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 'var(--space-2)' }}>
        <AiStar size={20} />
        <h3 style={{ font: 'var(--type-h3)', margin: 0 }}>Notes from AI</h3>
      </div>
      <div style={{ display: 'flex', flexDirection: 'column' }}>
        {notes.map((note) => (
          <p
            key={note}
            style={{
              margin: 0,
              padding: 'var(--space-3) 0',
              borderTop: 'var(--border-width) solid var(--rule)',
              color: 'var(--ink)',
              textWrap: 'pretty',
            } as React.CSSProperties}
          >
            {note}
          </p>
        ))}
      </div>
    </section>
  );
}

/** How the app will be built and run, and what runs beside it. */
function ThePlan({
  spec,
  marks,
  answers,
  source,
  commit,
}: {
  spec: AppSpec;
  marks: Suggestions;
  answers: Record<string, string>;
  source: Source | undefined;
  commit?: string;
}) {
  const workloads = spec.workloads ?? [];
  const primaryName = primaryWorkload(spec);
  const primary = workloads.find((w) => w.name === primaryName) ?? workloads[0];
  const command = answers.start_command || (primary?.command ?? []).join(' ');
  const port = answers.primary_port || (primary?.ports ?? [])[0]?.number?.toString();
  const build = spec.build;
  const health = spec.health;
  const short = commit ? commit.slice(0, 7) : undefined;

  const buildRows: Array<{ k: string; v: string; text?: boolean; ai?: Amendment }> = [
    { k: 'Approach', v: strategyLabel(build?.strategy), text: true },
  ];
  if (build?.dockerfile) buildRows.push({ k: 'Built from', v: build.dockerfile, ai: marks.build.dockerfile });
  if (build?.context && build.context !== '.') buildRows.push({ k: 'Build context', v: build.context, ai: marks.build.context });
  if (build?.static_dir) buildRows.push({ k: 'Serves', v: build.static_dir, ai: marks.build.static_dir });
  buildRows.push({
    k: 'Start',
    v: command || 'The image’s own command',
    text: !command,
    ai: primary ? marks.command[primary.name] : undefined,
  });
  if (port) buildRows.push({ k: 'Port', v: port, ai: primary ? marks.port[primary.name] : undefined });
  if (health?.source === 'http' && health.path) {
    buildRows.push({ k: 'Health check', v: `GET ${health.path} on :${health.port}`, ai: marks.health });
  }
  if (source?.type === 'git') {
    buildRows.push({
      k: 'Deploys',
      v: short ? `${short}${source.ref ? ` on ${source.ref}` : ''}` : (source.ref ?? 'The default branch'),
      text: !short,
    });
  }

  const slots = (spec.slots ?? []) as Slot[];
  const mounts = workloads.flatMap((w) => (w.mounts ?? []).map((m) => ({ workload: w.name, path: m.path })));

  return (
    <section style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-4)' }}>
      <h3 style={{ font: 'var(--type-h3)', margin: 0 }}>The plan</h3>
      <div
        style={{
          display: 'grid',
          gridTemplateColumns: 'repeat(auto-fit, minmax(min(100%, 21rem), 1fr))',
          gap: 'var(--space-5) var(--space-7)',
          alignItems: 'start',
        }}
      >
        <div style={{ display: 'flex', flexDirection: 'column' }}>
          <span style={{ font: 'var(--type-label)', color: 'var(--ink-secondary)', paddingBottom: 'var(--space-2)' }}>
            Build and run
          </span>
          {buildRows.map((r) => (
            <PlanRow key={r.k} label={r.k}>
              <span style={{ font: r.text ? 'var(--type-body-ui)' : 'var(--type-code-sm)', color: 'var(--ink)', overflowWrap: 'anywhere' }}>
                {r.v}
              </span>
              {r.ai && (
                <span title={r.ai.reason ? `Changed by AI. ${r.ai.reason}` : 'Changed by AI'}>
                  <AiStar size={14} />
                </span>
              )}
            </PlanRow>
          ))}
        </div>
        <div style={{ display: 'flex', flexDirection: 'column' }}>
          <span style={{ font: 'var(--type-label)', color: 'var(--ink-secondary)', paddingBottom: 'var(--space-2)' }}>
            Processes and services
          </span>
          {workloads.map((w) => (
            <ProcessRow
              key={w.name}
              name={w.name}
              detail={(w.command ?? []).join(' ') || 'image command'}
              status="running"
              state={workloads.length > 1 && w.primary ? 'Runs, serves the app' : 'Runs'}
            />
          ))}
          {slots.map((s) => (
            <ProcessRow
              key={s.key}
              name={slotLabel(s.type)}
              detail={s.key}
              status="info"
              state={s.required ? 'Required' : 'Optional'}
              ai={Boolean(marks.slots[s.key])}
            />
          ))}
          {mounts.map((m) => (
            <ProcessRow
              key={m.workload + m.path}
              name="Storage"
              detail={m.path}
              status="info"
              state="Kept across deploys"
              ai={Boolean(marks.volumes[m.path])}
            />
          ))}
        </div>
      </div>
    </section>
  );
}

function PlanRow({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div
      style={{
        display: 'grid',
        gridTemplateColumns: '7.5rem minmax(0, 1fr)',
        gap: 'var(--space-3)',
        padding: 'var(--space-2) 0',
        borderTop: 'var(--border-width) solid var(--rule)',
        alignItems: 'baseline',
      }}
    >
      <span style={{ color: 'var(--ink-secondary)' }}>{label}</span>
      <span style={{ display: 'flex', alignItems: 'center', gap: 'var(--space-2)', minWidth: 0 }}>{children}</span>
    </div>
  );
}

function ProcessRow({
  name,
  detail,
  status,
  state,
  ai,
}: {
  name: string;
  detail: string;
  status: 'running' | 'info' | 'stopped';
  state: string;
  ai?: boolean;
}) {
  return (
    <div
      style={{
        display: 'grid',
        gridTemplateColumns: '6rem minmax(0, 1fr) auto',
        gap: 'var(--space-3)',
        padding: 'var(--space-2) 0',
        borderTop: 'var(--border-width) solid var(--rule)',
        alignItems: 'baseline',
      }}
    >
      <span style={{ font: 'var(--type-label)', display: 'inline-flex', alignItems: 'center', gap: 'var(--space-1)' }}>
        {name}
        {ai && <AiStar size={12} />}
      </span>
      <span style={{ font: 'var(--type-code-sm)', color: 'var(--ink-secondary)', overflowWrap: 'anywhere' }}>{detail}</span>
      <StatusIndicator status={status} label={state} />
    </div>
  );
}

// --- The action bar -------------------------------------------------------------

function barStatus(
  missing: Question[],
  problems: number,
  canDeploy: boolean,
  canEdit: boolean,
): { status: 'building' | 'running' | 'info'; label: string; detail: string } {
  if (!canEdit) {
    return { status: 'info', label: 'Waiting for review', detail: 'Accepting this plan needs permission to change the app.' };
  }
  if (missing.length > 0) {
    return {
      status: 'building',
      label: missing.length === 1 ? '1 answer needed' : `${missing.length} answers needed`,
      detail: `Still needed: ${listNames(missing.map((q) => questionName(q.key)))}.`,
    };
  }
  if (problems > 0) {
    return { status: 'building', label: 'A variable needs a name', detail: 'Name the variable you added, or remove it.' };
  }
  return {
    status: 'running',
    label: canDeploy ? 'Ready to deploy' : 'Ready to accept',
    detail: 'Nothing runs until you accept.',
  };
}

function ActionBar({
  status,
  label,
  detail,
  error,
  children,
}: {
  status: 'building' | 'running' | 'info' | 'failed';
  label: string;
  detail: string;
  error?: unknown;
  children: React.ReactNode;
}) {
  return (
    <div className="pando-actionbar pando-onboard-enter" style={{ '--stagger': 2 } as React.CSSProperties}>
      <div>
        <div style={{ display: 'flex', flexDirection: 'column', gap: 'calc(var(--space-1) / 2)', minWidth: 0 }}>
          <StatusIndicator status={status} label={label} />
          <span style={{ font: 'var(--type-caption)', color: 'var(--ink-secondary)', paddingLeft: 'var(--space-4)' }}>
            {detail}
          </span>
          {Boolean(error) && (
            <div style={{ paddingLeft: 'var(--space-4)' }}>
              <Failure error={error} />
            </div>
          )}
        </div>
        <div style={{ display: 'flex', gap: 'var(--space-2)', flexWrap: 'wrap' }}>{children}</div>
      </div>
    </div>
  );
}

// --- Pieces ------------------------------------------------------------------------

/**
 * The AI mark: Lucide's sparkle with a small solid four-point star at its
 * lower right, in water blue. The design handoff approved it as the one
 * exception to the avoid-list's "no sparkle icons", and it is used everywhere
 * AI is indicated and nowhere else.
 */
function AiStar({ size }: { size: number }) {
  const star = Math.round(size * 0.56 + 2);
  const offset = -(Math.round(star * 0.3) + 1);
  return (
    <span
      aria-hidden="true"
      style={{ position: 'relative', display: 'inline-flex', width: size, height: size, flex: '0 0 auto' }}
    >
      <Icon name="sparkle" size={size} color="var(--water)" />
      <span style={{ position: 'absolute', right: offset, bottom: offset, display: 'flex' }}>
        <svg width={star} height={star} viewBox="0 0 24 24">
          <path
            d="M12 1 C12.8 7.5 16.5 11.2 23 12 C16.5 12.8 12.8 16.5 12 23 C11.2 16.5 7.5 12.8 1 12 C7.5 11.2 11.2 7.5 12 1 Z"
            fill="var(--water)"
          />
        </svg>
      </span>
    </span>
  );
}

function useReducedMotion(): boolean {
  const [reduced] = useState(
    () => typeof window !== 'undefined' && Boolean(window.matchMedia?.('(prefers-reduced-motion: reduce)').matches),
  );
  return reduced;
}
