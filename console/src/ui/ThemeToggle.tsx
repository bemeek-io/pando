// The control for it.
//
// One button, naming what it will do rather than what is in force — "Dark" in
// the light theme, "Light" in the dark one — because a toggle labelled with its
// current state reads as a statement and gets clicked by people who wanted the
// opposite. It is a ghost button: this is chrome, and there is one primary
// action per view already.

import { Button } from '@design';

import { useTheme } from './theme';

export function ThemeToggle() {
  const { theme, set } = useTheme();

  return (
    <Button
      variant="ghost"
      onClick={() => set(theme === 'dark' ? 'light' : 'dark')}
      title={`Switch to the ${theme === 'dark' ? 'light' : 'dark'} theme`}
    >
      {theme === 'dark' ? 'Light' : 'Dark'}
    </Button>
  );
}
