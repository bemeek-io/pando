// Light or dark, and who decides.
//
// The design system ships both themes as one attribute — `data-theme="dark"` on
// the root — and nothing in the console ever set it, so an installation was
// paper-white at three in the morning whatever the machine was set to.
//
// Three states, not two. "System" is the default and is not a third appearance:
// it is the absence of a choice, and it keeps following the machine when the
// machine changes. Choosing light or dark records that choice and stops
// following. The choice is per browser, in localStorage, because it is a
// property of where somebody is sitting rather than of who they are — the same
// account on a bright desk and a dark room wants different answers, and neither
// belongs in the database.

import { useEffect, useState, useSyncExternalStore } from 'react';

export type Theme = 'light' | 'dark';
export type Preference = Theme | 'system';

const KEY = 'pando.theme';
const DARK = '(prefers-color-scheme: dark)';

function stored(): Preference {
  try {
    const value = window.localStorage.getItem(KEY);
    return value === 'light' || value === 'dark' ? value : 'system';
  } catch {
    // Storage can be denied outright — a locked-down browser, a private
    // window. The console still has to render, in whatever the machine says.
    return 'system';
  }
}

function systemTheme(): Theme {
  return window.matchMedia?.(DARK).matches ? 'dark' : 'light';
}

function apply(theme: Theme) {
  document.documentElement.dataset.theme = theme;
  // So the browser paints form controls, scrollbars and the area outside the
  // page to match. Without it a dark console sits in a white window.
  document.documentElement.style.colorScheme = theme;
}

// One preference for the whole page, not one per caller. Each useTheme() used
// to hold its own copy in useState, so the settings screen could choose dark
// while App's copy still said "system" — and the next time the machine
// changed its appearance, App applied the machine's theme over the choice.
let current: Preference = stored();
const listeners = new Set<() => void>();

function subscribe(listener: () => void) {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

/**
 * The theme in force, the preference behind it, and a way to change it.
 *
 * Every caller shares one preference. It writes to the document rather than to
 * a context, because every color in the console already comes from a custom
 * property and the one thing that has to change is which set is in scope.
 */
export function useTheme(): { theme: Theme; preference: Preference; set: (next: Preference) => void } {
  const preference = useSyncExternalStore(subscribe, () => current);
  const [system, setSystem] = useState<Theme>(systemTheme);

  useEffect(() => {
    const media = window.matchMedia?.(DARK);
    if (!media) return undefined;
    const onChange = () => setSystem(media.matches ? 'dark' : 'light');
    media.addEventListener('change', onChange);
    return () => media.removeEventListener('change', onChange);
  }, []);

  const theme: Theme = preference === 'system' ? system : preference;

  useEffect(() => {
    apply(theme);
  }, [theme]);

  const set = (next: Preference) => {
    current = next;
    try {
      if (next === 'system') window.localStorage.removeItem(KEY);
      else window.localStorage.setItem(KEY, next);
    } catch {
      // The choice still holds for this page. It just will not be remembered.
    }
    listeners.forEach((listener) => listener());
  };

  return { theme, preference, set };
}
