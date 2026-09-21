// How a deploy reads in a list.
//
// Its own file because the answer is not obvious and is asserted in a test: a
// deploy's status says whether Pando did the work, and the app it started may
// never have reported healthy. A history of three rows reading "Deployed", for
// an app that had not once served a request, is a true answer to a question
// nobody asked.

export function deployLabel(status: string, result?: string): string {
  switch (status) {
    case 'succeeded':
      // A deploy that built, applied and routed is a deploy that worked, and
      // the app it started may never have reported healthy. Three rows reading
      // "Deployed" for an app that had not once served a request is a true
      // answer to the question nobody asked.
      return result === 'degraded' ? 'Deployed, not healthy' : 'Deployed';
    case 'failed':
      return 'Failed';
    case 'rolled_back':
      return 'Rolled back';
    case 'running':
      return 'Deploying';
    default:
      return status.charAt(0).toUpperCase() + status.slice(1).replace(/_/g, ' ');
  }
}

/** Deploy statuses onto the design system's symbols. */
export function deployStatus(
  status: string,
  result?: string,
): 'running' | 'building' | 'failed' | 'stopped' {
  switch (status) {
    case 'succeeded':
      // `building`, the same symbol a degraded app carries everywhere else:
      // not the failure symbol, because nothing failed, and not the running
      // one, because it is not running properly either.
      return result === 'degraded' ? 'building' : 'running';
    case 'failed':
      return 'failed';
    case 'rolled_back':
      return 'stopped';
    default:
      return 'building';
  }
}
