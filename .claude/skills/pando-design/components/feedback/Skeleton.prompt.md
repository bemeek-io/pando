One-sentence: the shape of something still loading, standing where it will be, so a page never shows blank space or a bare "Loading".

```jsx
<Skeleton width="24ch" />                       // a line of text
<Skeleton height="var(--control-input)" />      // a field
<Skeleton width="100%" height="8rem" radius="md" /> // a card or tile
<SkeletonText lines={3} />                       // a paragraph
```

- Static. No shimmer, no pulse: motion responds to what a person does, and loading is not something they did.
- Match the real thing's size and radius, so nothing moves when it arrives.
- Hidden for the first 150ms with its space kept, so a fast load shows nothing rather than a flash.
- For a table, pass `loading` to the console's Table rather than building rows by hand.
- Not for a failure: a request that failed says what happened (R-105), it does not keep loading.
