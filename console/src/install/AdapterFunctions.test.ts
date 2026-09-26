import { describe, expect, it } from 'vitest';

import { choiceChanges, currentChoice } from './AdapterFunctions';
import type { AIFunction } from './AIFunctions';

const fn = (name: string, adapter?: string, model?: string): AIFunction => ({
  function: name,
  title: name,
  adapter_id: adapter,
  model,
  on: Boolean(adapter),
});

describe('choosing functions on an AI adapter (R-259)', () => {
  it('starts from what this adapter handles, not what others do', () => {
    const all = [fn('repair_plan', 'ai_anthropic'), fn('search_audit', 'ai_openai', 'small'), fn('draft_access')];
    expect(currentChoice(all, 'ai_anthropic')).toEqual({ repair_plan: '' });
    expect(currentChoice(all, 'ai_openai')).toEqual({ search_audit: 'small' });
  });

  it('turns ticks into assignments, and unticks and model changes into the calls for them', () => {
    const calls = choiceChanges('ai_anthropic', { repair_plan: '', revise_plan: '' }, { repair_plan: 'fast', search_audit: '' });
    expect(calls).toEqual([
      { method: 'DELETE', fn: 'revise_plan', adapterID: 'ai_anthropic' },
      { method: 'PUT', fn: 'repair_plan', model: 'fast', adapterID: 'ai_anthropic' },
      { method: 'PUT', fn: 'search_audit', model: '', adapterID: 'ai_anthropic' },
    ]);
  });

  it('makes no calls when nothing changed', () => {
    expect(choiceChanges('ai_anthropic', { repair_plan: '' }, { repair_plan: '' })).toEqual([]);
  });
});
