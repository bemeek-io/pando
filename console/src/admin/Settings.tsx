// Your settings: the signed-in person's own preferences, and signing out.
//
// Open to everyone signed in, like the API screen — nothing here is
// administration. It sits under /admin because that prefix is already
// reserved against app slugs (consoleRoutes in httpapi); a new top-level path
// would be one more slug an app could not have (R-023).
//
// The theme is per browser rather than per account (see ui/theme.ts), which is
// why this screen says so: someone who picks dark here and finds a different
// laptop still light has not found a bug.

import { Radio } from '@design';

import { usePrincipal } from '../app/principal';
import { useTheme } from '../ui/theme';
import type { Preference } from '../ui/theme';
import { MEASURE } from '../ui/layout';
import { Sheet } from '../ui/Sheet';
import { SignOut } from '../ui/SignOut';

const THEMES: { value: Preference; label: string; description: string }[] = [
  { value: 'system', label: 'Match this device', description: 'Follows your device’s light or dark setting.' },
  { value: 'light', label: 'Light', description: '' },
  { value: 'dark', label: 'Dark', description: '' },
];

export function Settings({ onSignedOut }: { onSignedOut?: () => void }) {
  const me = usePrincipal();
  const { preference, set } = useTheme();

  const who = me.data?.display_name || me.data?.username || me.data?.email;

  return (
    <Sheet heading="Your settings">
      <div style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-7)', maxWidth: MEASURE }}>
        <section>
          <h4 style={{ font: 'var(--type-h4)', margin: '0 0 var(--space-3)' }}>Appearance</h4>
          <p style={{ font: 'var(--type-body-ui)', color: 'var(--ink-secondary)', margin: '0 0 var(--space-3)' }}>
            Saved in this browser only.
          </p>
          <div style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-2)' }}>
            {THEMES.map((t) => (
              <Radio
                key={t.value}
                name="theme"
                value={t.value}
                checked={preference === t.value}
                onChange={() => set(t.value)}
                label={t.label}
                description={t.description || undefined}
              />
            ))}
          </div>
        </section>

        <section>
          <h4 style={{ font: 'var(--type-h4)', margin: '0 0 var(--space-3)' }}>Account</h4>
          {who && (
            <p style={{ font: 'var(--type-body-ui)', color: 'var(--ink-secondary)', margin: '0 0 var(--space-3)' }}>
              Signed in as <strong style={{ color: 'var(--ink)' }}>{who}</strong>.
            </p>
          )}
          <SignOut onSignedOut={onSignedOut} />
        </section>
      </div>
    </Sheet>
  );
}
