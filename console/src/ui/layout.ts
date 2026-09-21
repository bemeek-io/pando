// How wide console content is laid out.
//
// The page itself is not capped: rows and their rules run to the edge of the
// window, which is what makes a wide display read as one console rather than a
// narrow one with a margin. What is capped is the content inside them. Every
// table sizes its columns in `ch`, so a three-column table stops at a measure
// instead of stretching Status and Updated across 2500px of paper, and a page
// header's action stops where the last column does.
//
// 84ch is that measure — the sum of a table's column widths — plus the Table's
// own column gaps and horizontal padding at three columns. `ch` resolves
// against the font of the element it is used on, so a header carrying this
// width also carries `--type-body-ui`: the table's font, and therefore the
// table's `ch`.
export const MEASURE = 'calc(84ch + var(--space-4) * 2 + var(--space-3) * 2)';
