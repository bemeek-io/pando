One-sentence: the printed sheet's neatline — a 1px rule-strong frame with 8px corner ticks — used to frame a page or a bounded region as a survey sheet, without drawing terrain.

```jsx
<MapCollar inset={false}>{page}</MapCollar>                          {/* console page frame */}
<MapCollar marginalia={['38°31′30″N', '111°45′00″W']} scale>{map}</MapCollar>  {/* hero, via ContourMap */}
```

- The collar is structure, not illustration. It does not spend the screen's one
  bold element — the spec's own hero prints a contour inside a collar.
- Never nest a collar in a collar, and never put one on a `Card`: a card is
  already a bounded object, and two frames around one thing is two frames.
- Marginalia and the scale bar are hero furniture. A console page takes the
  frame and the ticks and nothing else; invented coordinates on a settings
  screen are costume, which the brand's first rule rules out.
- `ContourMap collar` composes this. Do not re-draw corner ticks by hand.
