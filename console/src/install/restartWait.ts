/** How long to wait for Pando to come back before saying it has not. */
export const WAIT_SECONDS = 90;

/**
 * Whether Pando is back: answering, with a start time other than the one it
 * had before. A failed request in between is the restart itself.
 */
export async function waitForRestart(
  before: string | undefined,
  read: () => Promise<string | undefined>,
  sleep: (ms: number) => Promise<void> = (ms) => new Promise((r) => setTimeout(r, ms)),
  tries = WAIT_SECONDS,
): Promise<boolean> {
  for (let i = 0; i < tries; i++) {
    await sleep(1000);
    try {
      const now = await read();
      if (now !== undefined && now !== before) return true;
    } catch {
      // Down for the moment, which is expected.
    }
  }
  return false;
}
