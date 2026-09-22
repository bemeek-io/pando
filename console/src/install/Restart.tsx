/**
 * Restarting Pando from the console (POST /restart).
 *
 * Adapters and the configuration file are read at startup (R-253), so saving
 * one ends with "restart Pando". This is the button for that, rather than a
 * command for someone with a shell on the host.
 */
import { useState } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { Banner, Button, Dialog } from '@design';
import { api } from '@api/client';
import { messageOf } from './Accounts';
import { WAIT_SECONDS, waitForRestart } from './restartWait';

async function startedAt(): Promise<string | undefined> {
  const out = await api.get<{ started_at?: string }>('/adapters');
  return out.started_at;
}

export function RestartButton({ onRestarted }: { onRestarted?: () => void }) {
  const queries = useQueryClient();
  const [stage, setStage] = useState<'idle' | 'confirm' | 'restarting'>('idle');
  const [error, setError] = useState<string | null>(null);

  const restart = async () => {
    setError(null);
    setStage('restarting');
    try {
      const before = await startedAt();
      await api.post('/restart');
      if (await waitForRestart(before, startedAt)) {
        // Everything on screen came from the process that just stopped.
        await queries.invalidateQueries();
        setStage('idle');
        onRestarted?.();
        return;
      }
      setError(
        `Pando has not come back after ${WAIT_SECONDS} seconds. Check its logs where it runs, for example docker compose logs pando.`,
      );
    } catch (e) {
      setError(messageOf(e));
    }
    setStage('confirm');
  };

  const restarting = stage === 'restarting';
  return (
    <>
      <Button variant="secondary" onClick={() => setStage('confirm')}>
        Restart Pando
      </Button>
      <Dialog
        open={stage !== 'idle'}
        title="Restart Pando?"
        description="Pando finishes the requests in flight, then starts again and loads its adapters and configuration file. Apps behind it are unreachable for the seconds that takes, and a build or deploy under way is interrupted."
        onClose={restarting ? undefined : () => setStage('idle')}
        footer={
          <>
            <Button variant="ghost" disabled={restarting} onClick={() => setStage('idle')}>
              Cancel
            </Button>
            <Button variant="primary" disabled={restarting} onClick={restart}>
              {restarting ? 'Restarting…' : 'Restart'}
            </Button>
          </>
        }
      >
        {error && <Banner tone="failed">{error}</Banner>}
      </Dialog>
    </>
  );
}
