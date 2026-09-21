// Whether the console is at phone width.
//
// For the few places the markup itself has to change — the admin sidebar
// becomes a menu, the launcher's search takes a line of its own. Styles that
// only change go in narrow.css. Both use 48em.

import { useEffect, useState } from 'react';

const QUERY = '(max-width: 48em)';

export function useNarrow(): boolean {
  const [narrow, setNarrow] = useState(() => window.matchMedia?.(QUERY).matches ?? false);
  useEffect(() => {
    const media = window.matchMedia?.(QUERY);
    if (!media) return undefined;
    const onChange = () => setNarrow(media.matches);
    media.addEventListener('change', onChange);
    return () => media.removeEventListener('change', onChange);
  }, []);
  return narrow;
}
