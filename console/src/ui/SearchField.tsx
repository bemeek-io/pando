// The one search field, so every screen's search looks and behaves the same.
//
// The design system's Input, labelled for a screen reader rather than above
// the field — a visible "Search" label over a box that says "Search apps" is
// the same word twice. Escape clears it, which is what a search box is
// expected to do and the fastest way back to the whole list.

import { Input } from '@design';

export function SearchField({
  value,
  onChange,
  placeholder,
  width = '32ch',
  id,
}: {
  value: string;
  onChange: (value: string) => void;
  /** Says what is searched, e.g. "Search apps". Also the accessible name. */
  placeholder: string;
  /** How wide, as a CSS length. */
  width?: string;
  /** For a page that focuses the field from a keyboard shortcut. */
  id?: string;
}) {
  return (
    <div style={{ width, maxWidth: '100%' }}>
      <Input
        id={id}
        type="search"
        aria-label={placeholder}
        placeholder={placeholder}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === 'Escape' && value) {
            e.preventDefault();
            onChange('');
          }
        }}
      />
    </div>
  );
}

/** What a search that found nothing says. */
export function NoMatches({ what, query }: { what: string; query: string }) {
  return (
    <p style={{ font: 'var(--type-body-ui)', color: 'var(--ink-secondary)', margin: 0 }}>
      No {what} match &ldquo;{query.trim()}&rdquo;.
    </p>
  );
}
