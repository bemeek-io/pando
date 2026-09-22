// Onboarding a new app: what Pando found, and the choice to keep it (R-102,
// R-103, R-105).
//
// An app with no pinned spec has never been reviewed, and this page is the
// review. It replaces the one-tab app screen for that app — same address, no
// tabs — and walks down what detection produced: how Pando will build and run
// it, the questions it could not answer itself, the variables the app reads
// (with a place to set them), what it needs beside it, and the warnings. At the
// bottom the app is accepted, accepted and deployed, or rejected, which removes
// it.
//
// It builds as detection runs. `GET /detection` carries `stage` and whatever
// has been found so far while status is 'running', so each section appears as
// soon as its data exists and stands as a skeleton until then, and the
// contour map at the top draws in a ring at a time as the stages pass (readme
// §Motion records this page as the second orchestrated moment).
//
// The rules DetectionReview.tsx's header states still hold here, and for the
// same reasons: question text is verbatim with a copy button beside it, and the
// runners-up are shown. So does the rule for an AI screening (R-331, R-334):
// everything it changed is shown with its reason and the files it rests on,
// and what Pando refused is shown too — here inline, beside the thing it
// changed, marked with the sparkles, rather than as a card of its own.
//
// Reading is app.view. Answering and accepting are app.spec.edit; deploying is
// app.deploy; rejecting is app.delete; a secret value is app.secrets.write.
// Without a verb its control is absent or read-only (R-261).

import './onboarding.css';

import { useEffect, useRef, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  Button,
  Card,
  Checkbox,
  CodeBlock,
  ContourMap,
  Dialog,
  Icon,
  IconButton,
  Input,
  InlineCode,
  Skeleton,
  SkeletonText,
  Tag,
} from '@design';

import { api, RequestFailed } from '@api/client';
import type { Amendment, AppSpec, Candidate, Outcome, Proposal, Source } from '@api/types.gen';
import { InlineWarning } from '../ui/InlineWarning';
import { Sheet } from '../ui/Sheet';
import { Table } from '../ui/Table';
import { LineSkeleton, Loading } from '../ui/Loading';
import { MEASURE } from '../ui/layout';
import { deletePath } from './delete-app';
import {
  Blocked,
  DetectionFailed,
  Failure,
  QuestionCard,
  RunnersUp,
  SLOT_NAMES,
  fills,
  type DetectionResponse,
  type Slot,
} from './DetectionReview';
import {
  SCREENING_ADVISORY,
  answeredPrompt,
  detectionSteps,
  envKey,
  mergeRows,
  ringsFor,
  rowProblems,
  suggestions,
  trialSentence,
  valuesPayload,
  variableRows,
  volumePath,
  type RowEdit,
  type Step,
  type Suggestions,
  type VariableRow,
} from './onboarding';
import { GLYPHS, GLYPH_MS, PHRASE_MS, phrasesFor } from './onboardingPhrases';
import { describeAmendment, filesRead, screeningVisible } from './screeningText';
import { AppVerb, can, type AppWithVerbs } from './verbs';

export interface DeployRefusal {
  message: string;
  remedy?: string;
  code?: string;
}

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

  const answer = useMutation({
    mutationFn: (answers: Record<string, string>) => api.post(`/apps/${appID}/detection/answers`, { answers }),
    onSuccess: () => queries.invalidateQueries({ queryKey: ['apps', appID, 'detection'] }),
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
  const spec = proposal?.draft_spec as AppSpec | undefined;
  const detected = variableRows(spec).map((row) => (canSecrets ? row : { ...row, secret: false }));
  const rows = mergeRows(detected, edits, added);
  const problems = rowProblems(rows);

  const accept = useMutation({
    mutationFn: async (andDeploy: boolean) => {
      await api.post(`/apps/${appID}/detection/accept`, { values: valuesPayload(rows) });
      if (!andDeploy) return;
      // Accepting pins a spec and does not deploy it (Sequence A). The deploy is
      // its own request, made only once the accept has gone through, so a
      // refused deploy leaves a configured app rather than nothing.
      try {
        await api.post(`/apps/${appID}/deployments`, {});
        onDeployRefused(null);
      } catch (error) {
        const failed = error instanceof RequestFailed ? error : null;
        onDeployRefused({
          message:
            failed?.message ??
            'Pando saved the configuration but couldn’t start a deploy. Deploy it from the app’s overview.',
          remedy: failed?.remedy,
          code: failed?.code,
        });
      }
    },
    // ['apps']: the app leaves draft, which changes both this screen — it gives
    // way to the ordinary one, with tabs — and the app's row in the list.
    onSuccess: () => queries.invalidateQueries({ queryKey: ['apps'] }),
  });

  function edit(row: VariableRow, patch: RowEdit & { key?: string }) {
    if (row.original === undefined) {
      setAdded((list) => list.map((r) => (r.id === row.id ? { ...r, ...patch } : r)));
    } else {
      setEdits((all) => ({ ...all, [row.id]: { ...all[row.id], ...patch } }));
    }
  }

  function addRow() {
    nextRow.current += 1;
    setAdded((list) => [
      ...list,
      { id: `added-${nextRow.current}`, workload: '', key: '', value: '', secret: false },
    ]);
  }

  const heading = (
    <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'flex-start', gap: 'var(--space-3)' }}>
      <Button variant="ghost" icon={<Icon name="arrow-left" />} onClick={onBack}>
        Apps
      </Button>
      <h3 style={{ font: 'var(--type-h3)', margin: 0 }}>{app.name}</h3>
    </div>
  );

  const lede = (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-1)' }}>
      <SourceLine source={app.source} />
      <p style={{ font: 'var(--type-body-ui)', color: 'var(--ink-secondary)', margin: 0 }}>
        Review what Pando found. Nothing runs until you accept.
      </p>
    </div>
  );

  const rejectControl = canDelete && (
    <Button variant="destructive" onClick={() => setRejecting(true)}>
      Reject
    </Button>
  );

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
            {reject.isPending ? 'Rejecting' : 'Reject'}
          </Button>
        </>
      }
    >
      {reject.isError && <Failure error={reject.error} />}
    </Dialog>
  );

  // The page's shape before the first answer: the map's place, the steps'
  // place, and a card where the findings will be.
  if (detection.isPending) {
    return (
      <Sheet heading={heading} lede={lede} note={app.id}>
        <div style={{ maxWidth: MEASURE }}>
          <Loading gap="var(--space-6)">
            <div style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 'var(--space-5)' }}>
              <Skeleton width="12.5rem" height="9rem" radius="md" />
              <SkeletonText lines={3} />
            </div>
            <Skeleton height="10rem" radius="md" />
          </Loading>
        </div>
      </Sheet>
    );
  }

  if (detection.isError || !data || !proposal) {
    return (
      <Sheet heading={heading} lede={lede} note={app.id}>
        <div style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-5)', maxWidth: MEASURE }}>
          <Failure error={detection.error} />
          {rejectControl && <Footer left={rejectControl} />}
        </div>
        {rejectDialog}
      </Sheet>
    );
  }

  const { status, answers } = data;
  const stage = proposal.stage;
  const running = status === 'running';
  const failed = status === 'failed';
  const blocked = status === 'blocked';
  const finished = !running;
  const bid = proposal.winning_bid as Candidate | undefined;
  const outcome = proposal.screening;
  const marks = suggestions(outcome, spec);
  const steps = detectionSteps(status, stage, Boolean(outcome?.ran));

  const asked = (proposal.questions ?? []).filter((q) => !q.deferred);
  const unanswered = asked.filter((q) => !(answers ?? {})[q.key]);
  const problemCount = Object.keys(problems).length;

  // The contour map builds with the stages and gets its summit only when there
  // is something to accept. A failure stops the drawing where it was; a blocked
  // app has been read in full and has no summit to reach.
  const rings = failed ? ringsFor(undefined, false) : ringsFor(stage, finished);
  const summit = finished && !failed && !blocked;

  const hero = (
    <div style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 'var(--space-5)' }}>
      {/* Keyed by ring count, so each new stage draws in afresh. */}
      <ContourMap key={rings} className="pando-onboard-map" size={200} rings={rings} summit={summit} animate />
      <div style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-3)', minWidth: 0, flex: '1 1 24ch' }}>
        <Steps steps={steps} />
        {running && <StatusLine key={stage ?? 'fetching'} stage={stage} />}
        <ScreeningLine outcome={outcome} count={marks.count} />
      </div>
    </div>
  );

  if (failed || blocked) {
    return (
      <Sheet heading={heading} lede={lede} note={app.id}>
        <div style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-6)', maxWidth: MEASURE }}>
          {hero}
          <Rise>
            {failed ? (
              <DetectionFailed
                error={proposal.error}
                onRetry={canEdit ? () => rerun.mutate() : undefined}
                retrying={rerun.isPending}
              />
            ) : (
              <Blocked proposal={proposal} />
            )}
          </Rise>
          {rejectControl && <Footer left={rejectControl} />}
        </div>
        {rejectDialog}
      </Sheet>
    );
  }

  // Each section in page order, present once it has something to say. Keyed,
  // so one arriving between two others does not replay theirs; the index is
  // only the stagger for sections arriving together.
  const sections: Array<[string, React.ReactNode]> = [];

  sections.push([
    'found',
    bid ? (
      <Found proposal={proposal} bid={bid} spec={spec} marks={marks} trying={running && stage === 'trying'} />
    ) : (
      <SectionSkeleton title="What Pando found" />
    ),
  ]);

  if (asked.length > 0 || marks.answers.length > 0) {
    sections.push([
      'questions',
      <section style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-4)' }}>
        <SectionHeading
          title={asked.length === 0 ? 'Questions' : asked.length === 1 ? 'One question' : `${asked.length} questions`}
        >
          {running
            ? 'You can answer once Pando has finished looking. The trial run sometimes settles a question on its own.'
            : 'Pando asks only when it genuinely can’t work something out. Copy a question into the tool that wrote this app and paste its answer back.'}
        </SectionHeading>
        {asked.map((question) => (
          <QuestionCard
            key={question.key}
            question={question}
            answer={(answers ?? {})[question.key]}
            onAnswer={canEdit && !running ? (value) => answer.mutate({ [question.key]: value }) : undefined}
            saving={answer.isPending}
          />
        ))}
        {marks.answers.map((answered) => (
          <AnsweredByAI key={answered.key} proposal={proposal} answered={answered} />
        ))}
        {answer.isError && <Failure error={answer.error} />}
      </section>,
    ]);
  }

  sections.push([
    'variables',
    spec ? (
      <Variables
        rows={rows}
        problems={problems}
        marks={marks}
        canEdit={canEdit}
        canSecrets={canSecrets}
        manyWorkloads={(spec.workloads ?? []).length > 1}
        onEdit={edit}
        onAdd={addRow}
        onRemove={(row) => setAdded((list) => list.filter((r) => r.id !== row.id))}
      />
    ) : (
      <SectionSkeleton title="Variables" />
    ),
  ]);

  if (spec && ((spec.slots ?? []).length > 0 || (spec.volumes ?? []).length > 0)) {
    sections.push(['needs', <Needs spec={spec} marks={marks} />]);
  }

  if ((spec?.warnings ?? []).length > 0) {
    sections.push(['warnings', <Warnings spec={spec!} marks={marks} />]);
  }

  if (outcome?.ran && (outcome.refused ?? []).length > 0) {
    sections.push(['refused', <Refused outcome={outcome} />]);
  }

  const acceptBlocked = running || unanswered.length > 0 || problemCount > 0 || accept.isPending;
  const why = running
    ? 'Pando is still looking. You can fill in variables meanwhile.'
    : unanswered.length > 0
      ? unanswered.length === 1
        ? 'One question still needs an answer.'
        : `${unanswered.length} questions still need answers.`
      : problemCount > 0
        ? 'A variable above needs a usable name.'
        : undefined;

  // One primary on the page. Accept and deploy is it when the viewer may
  // deploy; otherwise Accept is the only way forward and takes the role.
  const acceptActions = canEdit && (
    <div
      // Re-keyed when detection finishes, so the actions rise into place at
      // the moment they become usable.
      key={finished ? 'ready' : 'waiting'}
      className={finished ? 'pando-onboard-enter' : undefined}
      style={{ display: 'flex', flexWrap: 'wrap', gap: 'var(--space-3)' }}
    >
      <Button
        variant={canDeploy ? 'secondary' : 'primary'}
        disabled={acceptBlocked}
        onClick={() => accept.mutate(false)}
      >
        {accept.isPending && accept.variables === false ? 'Accepting' : 'Accept'}
      </Button>
      {canDeploy && (
        <Button variant="primary" disabled={acceptBlocked} onClick={() => accept.mutate(true)}>
          {accept.isPending && accept.variables === true ? 'Accepting' : 'Accept and deploy'}
        </Button>
      )}
    </div>
  );

  return (
    <Sheet heading={heading} lede={lede} note={app.id}>
      <div style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-6)', maxWidth: MEASURE }}>
        {hero}

        {sections.map(([key, node], index) => (
          <Rise key={key} stagger={index}>
            {node}
          </Rise>
        ))}

        {(rejectControl || acceptActions) && (
          <Footer
            left={rejectControl}
            right={acceptActions}
            caption={
              canEdit && (
                <>
                  {why ?? 'Accepting saves this as the app’s configuration, with the variables you set.'}
                  {accept.isError && <Failure error={accept.error} />}
                </>
              )
            }
          />
        )}
      </div>
      {rejectDialog}
    </Sheet>
  );
}

// --- Header ----------------------------------------------------------------

function SourceLine({ source }: { source: Source }) {
  const caption = { font: 'var(--type-caption)', color: 'var(--ink-secondary)' };
  switch (source.type) {
    case 'git':
      return (
        <span style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 'var(--space-2)', ...caption }}>
          <InlineCode>{source.url}</InlineCode>
          {source.ref && <Tag mono icon={<Icon name="git-branch" size={12} />}>{source.ref}</Tag>}
          {source.subdir && <Tag mono>{source.subdir}</Tag>}
        </span>
      );
    case 'image':
      return (
        <span style={caption}>
          <InlineCode>{source.image}</InlineCode>
        </span>
      );
    case 'upload':
      return <span style={caption}>An uploaded archive</span>;
    default:
      return null;
  }
}

function Steps({ steps }: { steps: Step[] }) {
  return (
    <ol style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-2)', margin: 0, padding: 0, listStyle: 'none' }}>
      {steps.map((step) => (
        <li
          key={step.label}
          className="pando-onboard-step"
          data-state={step.state}
          aria-current={step.state === 'active' ? 'step' : undefined}
          style={{ display: 'flex', alignItems: 'center', gap: 'var(--space-2)', font: 'var(--type-body-ui)' }}
        >
          <StepMarker state={step.state} />
          {step.label}
        </li>
      ))}
    </ol>
  );
}

/** A contour ring drawing itself for the step under way, a check drawn in for
 *  one that is done, a faint ring for one still to come. */
function StepMarker({ state }: { state: Step['state'] }) {
  return (
    <svg
      viewBox="0 0 16 16"
      aria-hidden="true"
      style={{ width: 'var(--space-4)', height: 'var(--space-4)', flex: '0 0 auto' }}
      fill="none"
      strokeWidth="1.5"
      strokeLinecap="round"
      strokeLinejoin="round"
    >
      {state === 'done' ? (
        // Keyed by state, so the check draws in when the step finishes.
        <path key="done" className="pando-onboard-check" d="M3.5 8.5l3 3 6-7" pathLength={1} stroke="var(--ink)" />
      ) : state === 'active' ? (
        <circle key="active" className="pando-onboard-ring" cx="8" cy="8" r="6" pathLength={1} stroke="var(--contour)" />
      ) : (
        <circle key="pending" cx="8" cy="8" r="6" stroke="var(--rule)" />
      )}
    </svg>
  );
}

function useReducedMotion(): boolean {
  const [reduced] = useState(
    () => typeof window !== 'undefined' && Boolean(window.matchMedia?.('(prefers-reduced-motion: reduce)').matches),
  );
  return reduced;
}

/**
 * A live line while detection runs: a turning glyph and a phrase for the
 * stage, rotating. Remounted per stage (keyed by it), so each stage starts
 * from its first, literal phrase. Under reduced motion it is that phrase and a
 * still glyph.
 *
 * Hidden from assistive technology: a sentence changing every few seconds is
 * noise read aloud. The step list above carries the same progress, with the
 * current step marked.
 */
function StatusLine({ stage }: { stage: string | undefined }) {
  const reduced = useReducedMotion();
  const phrases = phrasesFor(stage);
  const [tick, setTick] = useState(0);
  const [frame, setFrame] = useState(0);

  useEffect(() => {
    if (reduced) return;
    const phrase = window.setInterval(() => setTick((n) => n + 1), PHRASE_MS);
    const glyph = window.setInterval(() => setFrame((n) => (n + 1) % GLYPHS.length), GLYPH_MS);
    return () => {
      window.clearInterval(phrase);
      window.clearInterval(glyph);
    };
  }, [reduced]);

  const phrase = phrases[tick % phrases.length];
  return (
    <p
      aria-hidden="true"
      style={{ display: 'flex', alignItems: 'center', gap: 'var(--space-2)', margin: 0, font: 'var(--type-body-ui)' }}
    >
      <span style={{ display: 'inline-block', width: '1em', textAlign: 'center', color: 'var(--contour)' }}>
        {GLYPHS[frame]}
      </span>
      <span key={phrase} className="pando-onboard-phrase" style={{ color: 'var(--ink-secondary)' }}>
        {phrase}
      </span>
    </p>
  );
}

/**
 * One line about the AI screening, near the top: what read the repository, and
 * that its suggestions are marked where they landed. The files it read are
 * behind a disclosure — they are what left the host (R-337), so they are there
 * to check, but they are not the first thing anybody needs.
 */
function ScreeningLine({ outcome, count }: { outcome: Outcome | undefined; count: number }) {
  if (!screeningVisible(outcome)) return null;
  const caption = { font: 'var(--type-caption)', color: 'var(--ink-secondary)', margin: 0 };

  if (!outcome.ran) {
    return <p style={caption}>This plan was not checked by AI. {outcome.skipped}</p>;
  }

  const files = outcome.files_read ?? [];
  const who = outcome.model ? `An AI model (${outcome.model})` : 'An AI model';
  const read = files.length === 1 ? 'read one file' : `read ${files.length} files`;

  return (
    <div className="pando-onboard-enter" style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-2)' }}>
      <p style={{ ...caption, display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 'var(--space-1)' }}>
        <Icon name="sparkles" size={14} color="var(--contour-text)" className="pando-onboard-ai-icon" />
        {count > 0 ? (
          <>
            {who} {read} and suggested the items marked
            <Icon name="sparkles" size={14} color="var(--contour-text)" />
          </>
        ) : (
          `${who} ${read} and found nothing in this plan to change.`
        )}
      </p>
      {(outcome.notes ?? []).map((note) => (
        <p key={note} style={{ font: 'var(--type-body-ui)', color: 'var(--ink)', margin: 0 }}>
          {note}
        </p>
      ))}
      {files.length > 0 && (
        <Disclosure show="Show which files" hide="Hide the files">
          <div style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-2)' }}>
            <span style={caption}>{filesRead(files.length)}</span>
            <div style={{ display: 'flex', flexWrap: 'wrap', gap: 'var(--space-2)' }}>
              {files.map((file) => (
                <InlineCode key={file}>{file}</InlineCode>
              ))}
            </div>
          </div>
        </Disclosure>
      )}
    </div>
  );
}

// --- Sections --------------------------------------------------------------

function Found({
  proposal,
  bid,
  spec,
  marks,
  trying,
}: {
  proposal: Proposal;
  bid: Candidate;
  spec: AppSpec | undefined;
  marks: Suggestions;
  trying: boolean;
}) {
  const build = spec?.build;
  const workloads = spec?.workloads ?? [];
  const health = spec?.health;
  const trial = trialSentence(proposal.trial);

  return (
    <Card padding="md">
      <div style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-5)' }}>
        <div style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-3)' }}>
          <div style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 'var(--space-3)' }}>
            <h4 style={{ font: 'var(--type-h4)', margin: 0 }}>What Pando found</h4>
            <Tag mono>{bid.strategy}</Tag>
          </div>
          {/* The evidence is the reasoning, as the API worded it (R-102). */}
          <ul style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-1)', margin: 0, paddingLeft: 'var(--space-4)' }}>
            {(bid.evidence ?? []).map((line) => (
              <li key={line} style={{ font: 'var(--type-body-ui)', color: 'var(--ink)' }}>
                {line}
              </li>
            ))}
          </ul>
        </div>

        {build && (build.dockerfile || build.static_dir || (build.context && build.context !== '.')) && (
          <Facts title="Build">
            {build.dockerfile && (
              <Fact label="Built from" ai={marks.build.dockerfile}>
                <InlineCode>{build.dockerfile}</InlineCode>
              </Fact>
            )}
            {build.static_dir && (
              <Fact label="Serves the files in" ai={marks.build.static_dir}>
                <InlineCode>{build.static_dir}</InlineCode>
              </Fact>
            )}
            {build.context && build.context !== '.' && (
              <Fact label="Build context" ai={marks.build.context}>
                <InlineCode>{build.context}</InlineCode>
              </Fact>
            )}
          </Facts>
        )}

        {workloads.length > 0 && (
          <Facts title="What runs">
            {workloads.map((w) => {
              const port = (w.ports ?? [])[0]?.number;
              const command = (w.command ?? []).join(' ');
              return (
                <div key={w.name} style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-2)' }}>
                  <div style={{ display: 'flex', alignItems: 'center', gap: 'var(--space-2)' }}>
                    <span style={{ font: 'var(--type-code)', color: 'var(--ink)' }}>{w.name}</span>
                    {w.primary && workloads.length > 1 && <Tag>primary</Tag>}
                  </div>
                  <Fact label="Starts with" ai={marks.command[w.name]}>
                    {command ? <InlineCode>{command}</InlineCode> : 'The image’s own command'}
                  </Fact>
                  {port !== undefined && (
                    <Fact label="Serves HTTP on" ai={marks.port[w.name]}>
                      port {port}
                    </Fact>
                  )}
                </div>
              );
            })}
            {health?.source === 'http' && health.path && (
              <Fact label="Health check" ai={marks.health}>
                <InlineCode>{`GET ${health.path}`}</InlineCode> on port {health.port}
              </Fact>
            )}
          </Facts>
        )}

        {/* The trial run: a line in outline while it runs, what it showed once
            it has. */}
        {trying ? (
          <LineSkeleton width="40ch" />
        ) : (
          trial && (
            <div className="pando-onboard-enter" style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-2)' }}>
              <p style={{ font: 'var(--type-body-ui)', color: 'var(--ink)', margin: 0 }}>{trial}</p>
              {proposal.trial_log && (
                <Disclosure show="Show what the trial run printed" hide="Hide the output">
                  <CodeBlock title="Trial run" lines={proposal.trial_log} />
                </Disclosure>
              )}
            </div>
          )
        )}

        <RunnersUp candidates={proposal.runners_up ?? []} />
      </div>
    </Card>
  );
}

function Facts({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-3)' }}>
      <span style={{ font: 'var(--type-label)', color: 'var(--ink-secondary)' }}>{title}</span>
      {children}
    </div>
  );
}

function Fact({ label, ai, children }: { label: string; ai?: Amendment; children: React.ReactNode }) {
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-1)' }}>
      <div style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'baseline', gap: 'var(--space-2)' }}>
        <span style={{ font: 'var(--type-body-ui)', color: 'var(--ink-secondary)' }}>{label}</span>
        <span style={{ font: 'var(--type-body-ui)', color: 'var(--ink)' }}>{children}</span>
      </div>
      {ai && <AiNote amendment={ai} />}
    </div>
  );
}

/** A question the screening answered. Detection stopped asking it, so this is
 *  the one place somebody can see what was decided on their behalf and why. */
function AnsweredByAI({
  proposal,
  answered,
}: {
  proposal: Proposal;
  answered: Suggestions['answers'][number];
}) {
  const question = answeredPrompt(proposal, answered.key);
  return (
    <Card padding="md">
      <div style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-2)' }}>
        {/* Verbatim when Pando still has the wording; the key otherwise. */}
        {question ? (
          <p style={{ font: 'var(--type-body-ui)', color: 'var(--ink)', margin: 0 }}>{question.prompt}</p>
        ) : (
          <InlineCode>{answered.key}</InlineCode>
        )}
        <span style={{ font: 'var(--type-body-ui)', color: 'var(--ink)' }}>
          Answered: <InlineCode>{answered.value}</InlineCode>
        </span>
        {answered.amendment ? (
          <AiNote amendment={answered.amendment} />
        ) : (
          <AiMarkOnly />
        )}
      </div>
    </Card>
  );
}

function Variables({
  rows,
  problems,
  marks,
  canEdit,
  canSecrets,
  manyWorkloads,
  onEdit,
  onAdd,
  onRemove,
}: {
  rows: VariableRow[];
  problems: Record<string, string>;
  marks: Suggestions;
  canEdit: boolean;
  canSecrets: boolean;
  manyWorkloads: boolean;
  onEdit: (row: VariableRow, patch: RowEdit & { key?: string }) => void;
  onAdd: () => void;
  onRemove: (row: VariableRow) => void;
}) {
  if (rows.length === 0 && !canEdit) return null;

  return (
    <section style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-4)' }}>
      <SectionHeading title="Variables">
        {rows.length === 0
          ? 'Pando found no variables this app reads. Add any it needs.'
          : 'What the app reads from its environment. Pando found the names in the repository and can’t know the values. What you set here is saved when you accept.'}
      </SectionHeading>

      {rows.length > 0 && (
        <div style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-4)' }}>
          {rows.map((row) => {
            const ai = row.original !== undefined ? marks.env[envKey(row.workload, row.key)] : undefined;
            const isNew = row.original === undefined;
            return (
              <div key={row.id} style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-2)' }}>
                <div style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'flex-start', gap: 'var(--space-3)' }}>
                  <div style={{ flex: '1 1 22ch', minWidth: 0 }}>
                    {isNew ? (
                      <Input
                        aria-label="Name"
                        placeholder="NAME"
                        mono
                        value={row.key}
                        error={problems[row.id]}
                        onChange={(e) => onEdit(row, { key: e.target.value })}
                      />
                    ) : (
                      <div
                        style={{
                          display: 'flex',
                          flexWrap: 'wrap',
                          alignItems: 'center',
                          gap: 'var(--space-2)',
                          minHeight: 'var(--control-input)',
                        }}
                      >
                        <span style={{ font: 'var(--type-code)', color: 'var(--ink)', overflowWrap: 'anywhere' }}>
                          {row.key}
                        </span>
                        {manyWorkloads && <Tag mono>{row.workload}</Tag>}
                      </div>
                    )}
                  </div>
                  <div style={{ flex: '2 1 24ch', minWidth: 0 }}>
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
                  <div
                    style={{
                      display: 'flex',
                      alignItems: 'center',
                      gap: 'var(--space-2)',
                      minHeight: 'var(--control-input)',
                    }}
                  >
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
                </div>
                {ai && <AiNote amendment={ai} />}
              </div>
            );
          })}
        </div>
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

/** Dependencies and storage: what was found, read-only. Both are set up on the
 *  Settings tab once the app is accepted. */
function Needs({ spec, marks }: { spec: AppSpec; marks: Suggestions }) {
  const slots = (spec.slots ?? []) as Slot[];
  const volumes = spec.volumes ?? [];
  const mounts = (spec.workloads ?? []).flatMap((w) =>
    (w.mounts ?? []).map((m) => ({ volume: m.volume_id, path: m.path, workload: w.name })),
  );
  const aiSlots = slots.filter((s) => marks.slots[s.key]);

  return (
    <section style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-4)' }}>
      <SectionHeading title="Dependencies and storage">
        You choose how each dependency is filled, and manage storage, on the Settings tab after accepting.
      </SectionHeading>

      {slots.length > 0 && (
        <div style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-3)' }}>
          <Table
            columns={[
              {
                key: 'key',
                header: 'Variable',
                width: 'minmax(0,26ch)',
                mono: true,
                render: (row: Slot) =>
                  marks.slots[row.key] ? (
                    <span style={{ display: 'inline-flex', alignItems: 'center', gap: 'var(--space-2)' }}>
                      {row.key}
                      <Icon name="sparkles" size={14} color="var(--contour-text)" className="pando-onboard-ai-icon" />
                    </span>
                  ) : (
                    row.key
                  ),
              },
              { key: 'type', header: 'Kind', width: '18ch', render: (row: Slot) => SLOT_NAMES[row.type] ?? row.type },
              { key: 'resolution', header: 'Filled by', width: 'minmax(0,30ch)', render: (row: Slot) => fills(row) },
            ]}
            rows={slots}
          />
          {aiSlots.map((slot) => (
            <AiNote key={slot.key} amendment={marks.slots[slot.key]!} subject={<InlineCode>{slot.key}</InlineCode>} />
          ))}
        </div>
      )}

      {volumes.length > 0 && (
        <div style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-3)' }}>
          {volumes.map((volume) => {
            const at = mounts.filter((m) => m.volume === volume.id);
            const ai = at.map((m) => marks.volumes[volumePath(m.path)]).find(Boolean);
            return (
              <div key={volume.id} style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-1)' }}>
                <div style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'baseline', gap: 'var(--space-2)' }}>
                  <span style={{ font: 'var(--type-body-ui)', color: 'var(--ink-secondary)' }}>Keeps data at</span>
                  {at.length > 0 ? (
                    at.map((m) => <InlineCode key={m.workload + m.path}>{m.path}</InlineCode>)
                  ) : (
                    <InlineCode>{volume.name}</InlineCode>
                  )}
                </div>
                {ai && <AiNote amendment={ai} />}
              </div>
            );
          })}
        </div>
      )}
    </section>
  );
}

function Warnings({ spec, marks }: { spec: AppSpec; marks: Suggestions }) {
  return (
    <section style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-3)' }}>
      {(spec.warnings ?? []).map((warning) => {
        const ai = warning.code === SCREENING_ADVISORY ? marks.warnings[warning.message.trim()] : undefined;
        return (
          <InlineWarning
            key={warning.code + warning.message}
            code={warning.code}
            action={
              ai ? <AiNote amendment={ai} /> : warning.code === SCREENING_ADVISORY ? <AiMarkOnly /> : undefined
            }
          >
            {warning.message}
          </InlineWarning>
        );
      })}
    </section>
  );
}

/** What the screening asked for that Pando refused, with Pando's reason as the
 *  server wrote it. Shown rather than dropped (R-334), and tucked away, because
 *  none of it is in the plan. */
function Refused({ outcome }: { outcome: Outcome }) {
  const refused = outcome.refused ?? [];
  const label = refused.length === 1 ? '1 suggestion Pando didn’t apply' : `${refused.length} suggestions Pando didn’t apply`;
  return (
    <Disclosure show={`Show ${label}`} hide={`Hide ${label}`}>
      <ul style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-4)', margin: 0, padding: 0, listStyle: 'none' }}>
        {refused.map((refusal, index) => (
          <li
            key={`${refusal.amendment.kind}-${index}`}
            style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-1)' }}
          >
            <span style={{ font: 'var(--type-body-ui)', color: 'var(--ink)' }}>{describeAmendment(refusal.amendment)}</span>
            <span style={{ font: 'var(--type-body-ui)', color: 'var(--ink-secondary)' }}>{refusal.reason}</span>
            <AiNote amendment={refusal.amendment} />
          </li>
        ))}
      </ul>
    </Disclosure>
  );
}

// --- Pieces ----------------------------------------------------------------

/** The marker for something an AI screening put in the plan. */
function AiMark() {
  return (
    <Tag
      tone="contour"
      icon={<Icon name="sparkles" size={12} color="var(--contour-text)" className="pando-onboard-ai-icon" />}
    >
      Suggested by AI
    </Tag>
  );
}

function AiMarkOnly() {
  return (
    <div className="pando-onboard-ai" style={{ alignSelf: 'flex-start' }}>
      <AiMark />
    </div>
  );
}

/**
 * The marker with the screening's reason and the files it rests on. The reason
 * is the model's, shown as the server stored it — it is what somebody checks
 * the suggestion against. Washed in contour once on arrival (onboarding.css).
 */
function AiNote({ amendment, subject }: { amendment: Amendment; subject?: React.ReactNode }) {
  const evidence = (amendment.evidence ?? []).filter(Boolean);
  return (
    <div
      className="pando-onboard-ai"
      style={{
        display: 'flex',
        flexWrap: 'wrap',
        alignItems: 'center',
        gap: 'var(--space-1) var(--space-2)',
        padding: 'var(--space-1)',
        font: 'var(--type-caption)',
        color: 'var(--ink-secondary)',
      }}
    >
      <AiMark />
      {subject}
      {amendment.reason && <span>{amendment.reason}</span>}
      {evidence.length > 0 && (
        <span style={{ display: 'inline-flex', flexWrap: 'wrap', alignItems: 'center', gap: 'var(--space-1)' }}>
          Based on
          {evidence.map((file) => (
            <InlineCode key={file}>{file}</InlineCode>
          ))}
        </span>
      )}
    </div>
  );
}

function SectionHeading({ title, children }: { title: string; children?: React.ReactNode }) {
  return (
    <div>
      <h4 style={{ font: 'var(--type-h4)', margin: 0 }}>{title}</h4>
      {children && (
        <p style={{ font: 'var(--type-body-ui)', color: 'var(--ink-secondary)', margin: 'var(--space-1) 0 0' }}>
          {children}
        </p>
      )}
    </div>
  );
}

/** A section Pando has not reached yet: its heading, and its body in outline. */
function SectionSkeleton({ title }: { title: string }) {
  return (
    <section style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-3)' }}>
      <h4 style={{ font: 'var(--type-h4)', margin: 0, color: 'var(--ink-secondary)' }}>{title}</h4>
      <SkeletonText lines={3} />
    </section>
  );
}

/** A section arriving: rises into place once, on mount (onboarding.css). */
function Rise({ stagger = 0, children }: { stagger?: number; children: React.ReactNode }) {
  return (
    <div className="pando-onboard-enter" style={{ '--stagger': stagger } as React.CSSProperties}>
      {children}
    </div>
  );
}

function Footer({
  left,
  right,
  caption,
}: {
  left?: React.ReactNode;
  right?: React.ReactNode;
  caption?: React.ReactNode;
}) {
  return (
    <section
      style={{
        display: 'flex',
        flexDirection: 'column',
        gap: 'var(--space-3)',
        paddingTop: 'var(--space-5)',
        borderTop: 'var(--border-width) solid var(--rule)',
      }}
    >
      <div
        style={{
          display: 'flex',
          flexWrap: 'wrap',
          alignItems: 'center',
          justifyContent: 'space-between',
          gap: 'var(--space-3)',
        }}
      >
        <div>{left}</div>
        {right}
      </div>
      {caption && (
        <div style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-2)', font: 'var(--type-caption)', color: 'var(--ink-secondary)' }}>
          {caption}
        </div>
      )}
    </section>
  );
}

function Disclosure({ show, hide, children }: { show: string; hide: string; children: React.ReactNode }) {
  const [open, setOpen] = useState(false);
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-3)' }}>
      <button
        onClick={() => setOpen(!open)}
        aria-expanded={open}
        style={{
          alignSelf: 'flex-start',
          border: 'none',
          background: 'transparent',
          padding: 0,
          cursor: 'pointer',
          font: 'var(--type-body-ui)',
          color: 'var(--ink-secondary)',
          textAlign: 'left',
        }}
      >
        {open ? hide : show}
      </button>
      {open && children}
    </div>
  );
}
