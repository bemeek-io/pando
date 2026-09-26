import { Icon } from '@design';

/**
 * The AI mark: Lucide's sparkle with a small solid four-point star at its
 * lower right, in water blue. The design handoff approved it as the one
 * exception to the avoid-list's "no sparkle icons", and it is used everywhere
 * AI is indicated and nowhere else.
 */
export function AiStar({ size }: { size: number }) {
  const star = Math.round(size * 0.56 + 2);
  const offset = -(Math.round(star * 0.3) + 1);
  return (
    <span
      aria-hidden="true"
      style={{ position: 'relative', display: 'inline-flex', width: size, height: size, flex: '0 0 auto' }}
    >
      <Icon name="sparkle" size={size} color="var(--water)" />
      <span style={{ position: 'absolute', right: offset, bottom: offset, display: 'flex' }}>
        <svg width={star} height={star} viewBox="0 0 24 24">
          <path
            d="M12 1 C12.8 7.5 16.5 11.2 23 12 C16.5 12.8 12.8 16.5 12 23 C11.2 16.5 7.5 12.8 1 12 C7.5 11.2 11.2 7.5 12 1 Z"
            fill="var(--water)"
          />
        </svg>
      </span>
    </span>
  );
}
