// How usage numbers read (R-245). Apart from Usage.tsx so they can be tested
// without rendering anything.

/** Thousandths of a core, as cores. */
export function cores(millis: number): string {
  const c = millis / 1000;
  return `${c < 0.1 && c > 0 ? c.toFixed(3) : c.toFixed(2)} ${c === 1 ? 'core' : 'cores'}`;
}

/** Bytes as people read them. */
export function bytes(n: number): string {
  if (n >= 1 << 30) return `${(n / (1 << 30)).toFixed(1)} GB`;
  if (n >= 1 << 20) return `${(n / (1 << 20)).toFixed(1)} MB`;
  if (n >= 1 << 10) return `${Math.round(n / (1 << 10))} KB`;
  return `${n} B`;
}
