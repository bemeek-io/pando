import { describe, expect, it } from 'vitest';
import { waitForRestart } from './restartWait';

const instant = () => Promise.resolve();

describe('waiting for a restart', () => {
  it('is back once Pando answers with a new start time, however many failures come first', async () => {
    const answers: (string | Error)[] = ['t1', new Error('down'), new Error('down'), 't2'];
    const read = async () => {
      const next = answers.shift();
      if (next instanceof Error) throw next;
      return next;
    };
    expect(await waitForRestart('t1', read, instant, 10)).toBe(true);
    expect(answers).toHaveLength(0);
  });

  it('is not back while the start time is the old one', async () => {
    expect(await waitForRestart('t1', async () => 't1', instant, 5)).toBe(false);
  });
});
