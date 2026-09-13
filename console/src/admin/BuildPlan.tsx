// The build plan, before it runs and afterwards.
//
// An app with no Dockerfile is built from a plan Pando generates. It used to be
// generated at build time, used, and thrown away — so the answer to "how is
// this being built" was "read the adapter's source". R-102 is ask-never-guess,
// and showing the decision is the same principle applied one step later.
//
// Editable because R-020 says the state store is the sole record of how an app
// runs, and "commit a Dockerfile to your repo to take control" is the opposite
// of that: it puts deployment files in the source tree, which is the thing
// Pando exists to avoid. An edit here is a spec revision like any other
// (R-152), so it can be diffed and rolled back.

import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Banner, Button, Input } from '@design';

import { api } from '@api/client';
import type { AppSpec } from '@api/types.gen';
import { Quiet, messageOf } from '../install/Accounts';

interface Revision {
  id: string;
  revision: number;
  body: AppSpec;
}

export function BuildPlan({ appID }: { appID: string }) {
  const queries = useQueryClient();
  const [draft, setDraft] = useState<string | null>(null);

  const specs = useQuery({
    queryKey: ['apps', appID, 'specs'],
    queryFn: () => api.get<{ revisions: Revision[] | null }>(`/apps/${appID}/specs`),
  });

  const newest = (specs.data?.revisions ?? []).slice().sort((a, b) => b.revision - a.revision)[0];

  const full = useQuery({
    queryKey: ['apps', appID, 'spec', newest?.revision],
    queryFn: () => api.get<Revision>(`/apps/${appID}/specs/${newest?.revision}`),
    enabled: Boolean(newest),
  });

  const spec = full.data?.body;
  const files = spec?.build?.generated_files ?? {};
  const path = spec?.build?.dockerfile || '.nixpacks/Dockerfile';
  const current = files[path];

  const save = useMutation({
    mutationFn: (content: string) =>
      api.post(`/apps/${appID}/specs`, {
        ...spec,
        build: { ...spec?.build, generated_files: { ...files, [path]: content } },
      }),
    onSuccess: () => {
      setDraft(null);
      void queries.invalidateQueries({ queryKey: ['apps', appID] });
      void queries.invalidateQueries({ queryKey: ['apps', appID, 'specs'] });
    },
  });

  // Nothing to show for an app built from its own Dockerfile, or one that has
  // not been through detection yet. Saying so beats an empty panel.
  if (!spec || current === undefined) return null;

  return (
    <section>
      <div style={{ display: 'flex', alignItems: 'baseline', justifyContent: 'space-between' }}>
        <h4 style={{ font: 'var(--type-h4)', margin: '0 0 var(--space-2)' }}>How this app is built</h4>
        {draft === null ? (
          <Button variant="ghost" onClick={() => setDraft(current)}>
            Edit
          </Button>
        ) : (
          <div style={{ display: 'flex', gap: 'var(--space-3)' }}>
            <Button variant="ghost" onClick={() => setDraft(null)}>
              Discard
            </Button>
            <Button variant="primary" disabled={save.isPending} onClick={() => save.mutate(draft)}>
              {save.isPending ? 'Saving' : 'Save'}
            </Button>
          </div>
        )}
      </div>

      <Quiet>
        Pando worked this out because the repository has no Dockerfile. It is kept here rather than
        in your repository, and it takes effect at the next deploy.
      </Quiet>

      {save.isError && <Banner tone="failed">{messageOf(save.error)}</Banner>}

      <div style={{ marginTop: 'var(--space-4)' }}>
        {draft === null ? (
          <pre
            style={{
              font: 'var(--type-code-sm)',
              background: 'var(--paper-sunken)',
              border: 'var(--border-width) solid var(--rule)',
              borderRadius: 'var(--radius-sm)',
              padding: 'var(--space-4)',
              overflowX: 'auto',
              margin: 0,
            }}
          >
            {current}
          </pre>
        ) : (
          <Input
            as="textarea"
            rows={22}
            mono
            value={draft}
            label={path}
            helper="Saved as a new revision. The previous one stays in the app's history."
            onChange={(e) => setDraft(e.target.value)}
          />
        )}
      </div>
    </section>
  );
}
