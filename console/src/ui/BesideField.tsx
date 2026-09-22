// A button that sits beside a labeled field, lined up with the field.
//
// A field is its label and then a 36px box; a button is 32px. In a row aligned
// to the bottom the button's lower edge met the field's and its top sat 4px
// low, and in a row aligned to the center it lined up with label and box
// together — both read as a button that had slipped. This gives the button a
// box the field's height to center in, so the two share a middle. The row it
// is in aligns to the bottom (`alignItems: 'flex-end'`).

export function BesideField({ children }: { children: React.ReactNode }) {
  return (
    <div style={{ height: 'var(--control-input)', display: 'inline-flex', alignItems: 'center', gap: 'var(--space-3)' }}>
      {children}
    </div>
  );
}
