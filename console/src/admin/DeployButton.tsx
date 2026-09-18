// Deploy, in the app's header.
//
// It used to sit at the bottom of the overview, under the status card, the
// security score and every warning — so deploying meant scrolling past
// everything on the page to reach the one thing somebody came to do. It is an
// action on the app rather than a part of the overview, which is what the
// header is for, and from there it is reachable from every tab.
//
// The primary button of the view, and the only one (the brand's rule): Delete
// beside it is destructive, and nothing else on an app's screen is primary.
//
// It owns its own query of the app's revisions rather than taking them as a
// prop. The key is the one the overview already uses, so asking twice costs one
// request, and the alternative — passing the revision down — is how a button
// ends up shipping a spec that changed while somebody was reading the page.

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Button } from '@design';

import { api, RequestFailed } from '@api/client';
import type { App } from '@api/types.gen';

interface SpecRevision {
  id: string;
  revision: number;
}

export function DeployButton({ app }: { app: App }) {
  const queries = useQueryClient();

  const specs = useQuery({
    queryKey: ['apps', app.id, 'specs'],
    queryFn: () =>
      api.get<{ revisions: SpecRevision[] | null; pinned_spec_id: string }>(`/apps/${app.id}/specs`),
    enabled: Boolean(app.pinned_spec_id),
  });

  const revisions = (specs.data?.revisions ?? []).slice().sort((a, b) => b.revision - a.revision);
  const newest = revisions[0];
  const pinned = revisions.find((r) => r.id === app.pinned_spec_id);

  // Editing anything writes a new revision and leaves the pinned one alone
  // (R-152), so Deploy ships the newest when there is one — which is what the
  // banner on the overview is telling somebody about.
  const unshipped = Boolean(newest && pinned && newest.revision > pinned.revision);

  const deploy = useMutation({
    mutationFn: () =>
      api.post(`/apps/${app.id}/deployments`, unshipped ? { spec_revision: newest?.revision } : {}),
    // ['apps'] rather than this app alone, so the list's status follows it
    // through building to running.
    onSuccess: () => queries.invalidateQueries({ queryKey: ['apps'] }),
  });

  const failed = deploy.error instanceof RequestFailed ? deploy.error : null;

  return (
    <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'flex-end', gap: 'var(--space-1)' }}>
      <Button
        variant="primary"
        onClick={() => deploy.mutate()}
        disabled={deploy.isPending || app.state === 'deploying'}
      >
        {app.state === 'deploying' ? 'Deploying' : 'Deploy'}
      </Button>

      {deploy.isError && (
        // Beside the button that caused it rather than at the foot of the
        // page: the message is the server's, written to be acted on (R-105).
        <div style={{ textAlign: 'right', maxWidth: '48ch' }}>
          <p style={{ font: 'var(--type-caption)', color: 'var(--marker-deep)', margin: 0 }}>
            {failed?.message ?? 'Pando couldn’t start a deploy. Try again.'}
          </p>
          {failed?.remedy && (
            <p style={{ font: 'var(--type-caption)', color: 'var(--ink-secondary)', margin: 0 }}>
              {failed.remedy}
            </p>
          )}
        </div>
      )}
    </div>
  );
}
