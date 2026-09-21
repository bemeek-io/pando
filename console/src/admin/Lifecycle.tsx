// Stopping an app, and starting it again.
//
// The API has had `POST /apps/{id}/start`, `/stop` and `/restart` since the
// lifecycle handlers were written, and no surface called any of them: the only
// way to take an app down was to delete it, which also asks what to do with its
// data and cannot be undone. R-261 says a capability the API has is a
// capability every surface has.
//
// Stop is desired state, not an act: the reconciler converges to it, so an app
// somebody stopped stays stopped across a restart of Pando itself rather than
// coming back up on its own (design 05).

import { useState } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { Banner, Button, Dialog } from '@design';

import { api } from '@api/client';
import type { App } from '@api/types.gen';
import { messageOf } from '../install/Accounts';

export function Lifecycle({ app }: { app: App }) {
  const queries = useQueryClient();
  const [confirming, setConfirming] = useState(false);

  const act = useMutation({
    mutationFn: (what: 'start' | 'stop' | 'restart') =>
      api.post(`/apps/${app.id}/${what}`, {}),
    onSuccess: () => {
      // ['apps'], because the list says what each app is doing too.
      void queries.invalidateQueries({ queryKey: ['apps'] });
      setConfirming(false);
    },
  });

  // Nothing to stop or start before there is something to run.
  if (!app.pinned_spec_id) return null;

  const stopped = app.desired_state === 'stopped';

  return (
    <>
      {stopped ? (
        <Button variant="secondary" disabled={act.isPending} onClick={() => act.mutate('start')}>
          {act.isPending ? 'Starting' : 'Start'}
        </Button>
      ) : (
        <Button variant="secondary" disabled={act.isPending} onClick={() => setConfirming(true)}>
          Stop
        </Button>
      )}

      {/* Restart is an act rather than a state, and a short one: no dialog,
          because the app is coming straight back. */}
      {!stopped && (
        <Button variant="ghost" disabled={act.isPending} onClick={() => act.mutate('restart')}>
          {act.isPending ? 'Restarting' : 'Restart'}
        </Button>
      )}

      {confirming && (
        <Dialog
          open
          onClose={() => setConfirming(false)}
          title={`Stop ${app.name}?`}
          description="It stops serving until you start it again."
          footer={
            <>
              <Button variant="ghost" onClick={() => setConfirming(false)}>
                Cancel
              </Button>
              <Button variant="primary" disabled={act.isPending} onClick={() => act.mutate('stop')}>
                {act.isPending ? 'Stopping' : 'Stop the app'}
              </Button>
            </>
          }
        >
          <div style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-4)' }}>
            {/* The difference from Delete, said plainly: this is the reason
                somebody reaches for one rather than the other. */}
            <Banner tone="info">
              Nothing is removed. Its storage, its configuration and its address are kept, and
              starting it again brings back the version that was running.
            </Banner>
            {act.isError && <Banner tone="failed">{messageOf(act.error)}</Banner>}
          </div>
        </Dialog>
      )}
    </>
  );
}
