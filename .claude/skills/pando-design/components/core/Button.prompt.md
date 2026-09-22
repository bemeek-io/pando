One-sentence: names an action in plain words and sentence case; `primary` is ink on paper and appears once per view.

```jsx
<Button variant="primary" size="marketing">Install Pando</Button>
<Button>Share app</Button>
<Button variant="ghost">Cancel</Button>
<Button variant="destructive">Delete app</Button>
```

- Heights: 32px console, 40px marketing; 12px / 18px horizontal padding.
- No arrows or icons after the label. Press scales to 0.98 over 80ms.
- Keep the verb stable through the flow: "Deploy" → "Deployed".
- `ghost` is for the quiet half of a pair — Cancel, Close, Discard beside the action they back out of — and for back links and "clear" links. An action that stands on its own (Edit, Reset password, Remove, Add group) is `secondary`: a ghost button alone on a page reads as a word, not as something to press. Delete on its own page is `destructive`; in a table row, `secondary`, since a column of red is louder than one deletion.
