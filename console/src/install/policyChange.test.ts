import { describe, expect, it } from 'vitest';

import { describeChange } from './policyChange';

describe('a proposed policy change reads as the Policy screen puts it (R-344)', () => {
  it('names terminal access as its switch, not as disabled_verbs', () => {
    expect(describeChange({ key: 'disabled_verbs', from: null, to: ['app.exec'] })).toEqual([
      {
        title: 'Turn off terminal access for the whole installation',
        detail: 'Off to on. Nobody gets a terminal, including the person who owns the app.',
      },
    ]);
  });

  it('says which other permissions are added or removed', () => {
    expect(describeChange({ key: 'disabled_verbs', from: ['app.delete'], to: ['app.exec'] }).map((d) => d.detail)).toEqual([
      'Off to on. Nobody gets a terminal, including the person who owns the app.',
      'Removes app.delete.',
    ]);
  });

  it('names sharing and switches in words', () => {
    expect(describeChange({ key: 'public_sharing', from: 'allowed', to: 'none' })[0]!.detail).toBe('Allowed to Not allowed.');
    expect(describeChange({ key: 'disable_ai_screening', from: null, to: true })[0]).toEqual({
      title: 'Turn off AI screening of deployment plans',
      detail: 'Off to on.',
    });
  });
});

describe('egress and deploy approval read as the Policy screen puts them (R-181, R-154)', () => {
  it('names the egress mode in words, unset as anywhere', () => {
    expect(describeChange({ key: 'egress_mode', from: null, to: 'denylist' })).toEqual([
      { title: 'Where apps may connect out to', detail: 'Anywhere to Anywhere except the list.' },
    ]);
    expect(describeChange({ key: 'egress_mode', from: 'denylist', to: 'allowlist' })[0]!.detail).toBe(
      'Anywhere except the list to Only the list.',
    );
  });

  it('names each entry added to or removed from a list', () => {
    expect(describeChange({ key: 'egress_list', from: ['a.example.com'], to: ['*.example.org'] })).toEqual([
      { title: "The installation's egress list", detail: 'Adds *.example.org.' },
      { title: "The installation's egress list", detail: 'Removes a.example.com.' },
    ]);
    expect(describeChange({ key: 'deploy_approval_apps', from: null, to: ['app_01'] })).toEqual([
      { title: 'Apps that always need approval', detail: 'Adds app_01.' },
    ]);
  });

  it('names the private-address switch and the loosening rule', () => {
    expect(describeChange({ key: 'egress_block_private', from: null, to: true })[0]).toEqual({
      title: 'Block private addresses',
      detail: 'Off to on.',
    });
    expect(describeChange({ key: 'egress_loosening', from: null, to: 'approval' })[0]).toEqual({
      title: 'App changes that loosen these rules',
      detail: 'Allowed for people with permission to Need a deploy approval.',
    });
    expect(describeChange({ key: 'egress_loosening', from: 'approval', to: 'forbidden' })[0]!.detail).toBe(
      'Need a deploy approval to Not allowed.',
    );
  });

  it('reads approval count and expiry as the server does: zero is one, and zero hours is never', () => {
    expect(describeChange({ key: 'deploy_approval_required', from: false, to: true })[0]).toEqual({
      title: "Require approval for every app's deploys",
      detail: 'Off to on.',
    });
    expect(describeChange({ key: 'deploy_approval_count', from: 0, to: 2 })[0]).toEqual({
      title: 'Approvals needed',
      detail: '1 to 2.',
    });
    expect(describeChange({ key: 'deploy_approval_expiry_hours', from: 168, to: 0 })[0]).toEqual({
      title: 'Requests expire after, in hours',
      detail: '168 hours to never.',
    });
    expect(describeChange({ key: 'deploy_approval_expiry_hours', from: null, to: 24 })[0]!.detail).toBe(
      'Never to 24 hours.',
    );
  });
});
