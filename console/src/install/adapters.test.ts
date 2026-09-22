import { describe, expect, it } from 'vitest';

import { adapterRequest, blankForm, categoryLabel, formProblems, orderCategories, sortKinds } from './adapters';
import type { AdapterKind } from './adapters';

const anthropic: AdapterKind = {
  category: 'ai',
  kind: 'anthropic',
  name: 'Anthropic',
  description: 'Reads a repository and checks the plan detection made.',
  id_prefix: 'ai_',
  fields: [
    { key: 'api_key', label: 'API key', type: 'string', credential: true, required: true },
    { key: 'model', label: 'Model', type: 'string' },
    { key: 'base_url', label: 'Base URL', type: 'string' },
  ],
};

const trivy: AdapterKind = {
  category: 'scanner',
  kind: 'trivy',
  name: 'Trivy',
  description: 'Scans images.',
  id_prefix: 'scan_',
  fields: [
    { key: 'timeout_seconds', label: 'Timeout', type: 'int', placeholder: '300' },
    { key: 'offline', label: 'Offline', type: 'bool' },
  ],
};

describe('adding an adapter', () => {
  it('prefills an ID from the kind and makes the first of a category the default', () => {
    expect(blankForm(anthropic, true)).toEqual({ id: 'ai_anthropic', name: 'Anthropic', isDefault: true, values: {} });
    expect(blankForm(anthropic, false).isDefault).toBe(false);
  });

  it('sends credentials in credentials, never in config (R-190)', () => {
    const body = adapterRequest(anthropic, {
      ...blankForm(anthropic, true),
      values: { api_key: ' sk-ant-123 ', model: 'claude-sonnet-5', base_url: '' },
    });
    expect(body).toEqual({
      id: 'ai_anthropic',
      category: 'ai',
      kind: 'anthropic',
      name: 'Anthropic',
      config: { model: 'claude-sonnet-5' },
      credentials: { api_key: 'sk-ant-123' },
      is_default: true,
    });
  });

  it('leaves out an empty credential, which keeps the stored one', () => {
    const body = adapterRequest(anthropic, { ...blankForm(anthropic, false), values: { api_key: '' } }, true);
    expect(body.credentials).toBeUndefined();
    expect(body.config).toEqual({});
    expect(body.enabled).toBe(true);
  });

  it('sends whole numbers as numbers and a bool only when on', () => {
    const on = adapterRequest(trivy, { ...blankForm(trivy, true), values: { timeout_seconds: '120', offline: true } });
    expect(on.config).toEqual({ timeout_seconds: 120, offline: true });
    const off = adapterRequest(trivy, { ...blankForm(trivy, true), values: { offline: false } });
    expect(off.config).toEqual({});
  });

  it('says what is wrong in words that say how to fix it', () => {
    const form = { ...blankForm(trivy, true), id: ' ', values: { timeout_seconds: '2m' } };
    expect(formProblems(trivy, form)).toEqual({
      id: 'An adapter needs an ID, for example scan_trivy.',
      timeout_seconds: 'Timeout takes a whole number, such as 300.',
    });
  });

  it('requires a required credential unless one is already stored', () => {
    const form = blankForm(anthropic, true);
    expect(formProblems(anthropic, form)).toEqual({ api_key: 'API key is required.' });
    expect(formProblems(anthropic, form, ['api_key'])).toEqual({});
  });

  it('offers kinds by category, then name', () => {
    expect(sortKinds([trivy, anthropic]).map((k) => k.kind)).toEqual(['anthropic', 'trivy']);
    expect(categoryLabel('ai')).toBe('AI');
    expect(categoryLabel('routing')).toBe('Routing');
  });
});

describe('adapter categories', () => {
  it('orders categories as an installation is built up, unknown ones last', () => {
    expect(orderCategories(['ai', 'runtime', 'zeta', 'routing', 'ai'])).toEqual(['runtime', 'routing', 'ai', 'zeta']);
  });
});
