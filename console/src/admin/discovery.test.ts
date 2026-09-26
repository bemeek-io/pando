import { describe, expect, it } from 'vitest';

import type { Proposal, Question } from '@api/types.gen';
import {
  aiFrom,
  answerFor,
  answerState,
  discoverySteps,
  elevation,
  groupOf,
  neededFunction,
  peakOf,
  planSpec,
  progress,
  questionName,
  stillNeeded,
  stops,
} from './discovery';

const source = { type: 'git', url: 'https://github.com/acme/inventory.git', ref: 'main' };

const found: Partial<Proposal> = {
  commit: '3f9a2c1d',
  winning_bid: { detector: 'dockerfile', strategy: 'dockerfile', confidence: 0.9, evidence: ['Dockerfile at the root'] },
  draft_spec: {
    workloads: [
      {
        name: 'web',
        command: ['npm', 'start'],
        ports: [{ number: 3000 }],
        env: [{ key: 'PORT', value: '3000' }, { key: 'DATABASE_URL', slot_ref: 'DATABASE_URL' }],
        exposed: true,
        primary: true,
      },
    ],
    slots: [{ key: 'DATABASE_URL', type: 'postgres', required: true }],
  } as unknown as Proposal['draft_spec'],
};

describe('discoverySteps', () => {
  it('shows only reading the repo while fetching', () => {
    const steps = discoverySteps({ status: 'running', stage: 'fetching', proposal: {}, source });
    expect(steps.filter((s) => s.state !== 'pending').map((s) => s.id)).toEqual(['read']);
    expect(steps[0]!.state).toBe('current');
  });

  it('finishes the auction steps together, each with what it found', () => {
    const steps = discoverySteps({ status: 'ready', proposal: found, source, commit: '3f9a2c1d' });
    expect(steps.map((s) => s.id)).toEqual(['read', 'stack', 'runs', 'vars']);
    expect(steps.every((s) => s.state === 'done')).toBe(true);
    expect(steps[0]!.result).toBe('At 3f9a2c1');
    expect(steps[1]!.result).toBe('Dockerfile');
    expect(steps[2]!.findings).toContainEqual({ key: 'web', value: 'npm start on :3000', text: false });
    expect(steps[3]!.result).toBe('2 found, Pando fills 1');
  });

  it('adds a trial step only when a trial runs, and marks a crash as failed', () => {
    const crashed = { ...found, trial: { ran: true, crashed: true } };
    const steps = discoverySteps({ status: 'ready', proposal: crashed, source });
    const trial = steps.find((s) => s.id === 'trial');
    expect(trial?.failed).toBe(true);
    expect(discoverySteps({ status: 'ready', proposal: found, source }).some((s) => s.id === 'trial')).toBe(false);
  });

  it('adds an AI step only when AI was called (R-336)', () => {
    const quiet = { ...found, screening: { ran: false, skip_code: 'not_needed' } };
    expect(discoverySteps({ status: 'ready', proposal: quiet, source }).some((s) => s.ai)).toBe(false);

    const answered = {
      ...found,
      questions: [{ key: 'primary_port', prompt: 'Which port?', why: '', kind: 'port' }],
      screening: { ran: true, function: 'answer_questions', answers: { primary_port: '3000' }, files_read: ['server.js'] },
    };
    const ai = discoverySteps({ status: 'ready', proposal: answered, source }).find((s) => s.ai);
    expect(ai?.name).toBe('Answer what it can');
    expect(ai?.result).toBe('Answered 1 of 1');
    expect(ai?.findings).toContainEqual({ key: 'port', value: '3000' });
  });

  it('names a running AI step by what detection needs', () => {
    const crashed = { ...found, trial: { ran: true, crashed: true } };
    const steps = discoverySteps({ status: 'running', stage: 'screening', proposal: crashed, source });
    expect(steps.at(-1)?.name).toBe('Review the failed plan');
    expect(steps.at(-1)?.state).toBe('current');
  });
});

describe('neededFunction', () => {
  it('mirrors core/detection.needed', () => {
    expect(neededFunction({ trial: { ran: true, crashed: true } })).toBe('repair_plan');
    expect(neededFunction({ winning_bid: { detector: '', strategy: 'unknown', confidence: 0 } })).toBe('repair_plan');
    expect(neededFunction({ questions: [{ key: 'k', prompt: '', why: '', kind: 'text' }] })).toBe('answer_questions');
    expect(neededFunction({ questions: [{ key: 'k', prompt: '', why: '', kind: 'text', deferred: true }] })).toBeUndefined();
  });
});

describe('progress', () => {
  it('counts finished steps and eases into the current one without reaching its end', () => {
    const steps = discoverySteps({ status: 'running', stage: 'detecting', proposal: {}, source });
    const early = progress(steps, 0);
    const late = progress(steps, 60_000);
    expect(early).toBeCloseTo(1 / steps.length);
    expect(late).toBeGreaterThan(early);
    expect(late).toBeLessThan(2 / steps.length);
  });

  it('places waypoints between steps and marks where AI starts', () => {
    const answered = { ...found, screening: { ran: true, function: 'answer_questions' } };
    const steps = discoverySteps({ status: 'ready', proposal: answered, source });
    expect(stops(steps)).toHaveLength(steps.length - 1);
    expect(aiFrom(steps)).toBeCloseTo(4 / 5);
    expect(aiFrom(discoverySteps({ status: 'ready', proposal: found, source }))).toBe(1);
  });
});

describe('questions', () => {
  const plain: Question = { key: 'primary_port', prompt: 'Which port?', why: '', kind: 'port' };
  const suggested: Question = { ...plain, key: 'start_command', suggested: { value: 'npm start', reason: 'package.json' } };

  it('groups by whether AI answered, not by what is typed', () => {
    expect(groupOf(plain)).toBe('needs');
    expect(groupOf(suggested)).toBe('ai');
  });

  it('takes a draft, then a saved answer, then the suggestion (R-338)', () => {
    expect(answerFor(suggested, null)).toBe('npm start');
    expect(answerFor(suggested, { start_command: 'node server.js' })).toBe('node server.js');
    expect(answerFor(suggested, { start_command: 'node server.js' }, 'yarn start')).toBe('yarn start');
  });

  it('says whether an answer is AI’s, the person’s, or missing', () => {
    expect(answerState(suggested, 'npm start')).toBe('ai');
    expect(answerState(suggested, 'yarn start')).toBe('answered');
    expect(answerState(plain, '')).toBe('needed');
  });

  it('lists what is still needed, counting a suggestion as an answer', () => {
    expect(stillNeeded([plain, suggested], null, {}).map((q) => q.key)).toEqual(['primary_port']);
    expect(stillNeeded([plain, suggested], null, { primary_port: '8080' })).toEqual([]);
  });

  it('names questions briefly', () => {
    expect(questionName('primary_port')).toBe('port');
    expect(questionName('build_arg.NODE_VERSION')).toBe('NODE_VERSION');
    expect(questionName('mystery')).toBe('mystery');
  });
});

describe('planSpec', () => {
  it('shows the reading the build-method answer picks', () => {
    const compose = { workloads: [{ name: 'app' }] } as unknown as Proposal['draft_spec'];
    const proposal: Partial<Proposal> = {
      ...found,
      runners_up: [{ detector: 'compose', strategy: 'compose', confidence: 0.8, spec: compose }],
    };
    expect(planSpec(proposal, { build_strategy: 'compose' })).toBe(compose);
    expect(planSpec(proposal, { build_strategy: 'dockerfile' })).toBe(found.draft_spec);
    expect(planSpec(proposal, {})).toBe(found.draft_spec);
  });
});

describe('terrain', () => {
  it('is the same for the same seed, peaks at 1, and differs by seed', () => {
    const a = elevation('acme/inventory@3f9a2c1');
    const b = elevation('acme/inventory@3f9a2c1');
    const c = elevation('acme/other@3f9a2c1');
    expect(a(0.42)).toBe(b(0.42));
    expect(a(peakOf(a))).toBeCloseTo(1, 5);
    expect(a(0.42)).not.toBe(c(0.42));
  });
});
