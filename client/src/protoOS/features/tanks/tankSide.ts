// CT1 tank numbers subdivide fluid volume A (left, 1–3) and B (right, 4–6).
// Missing tanks must not shift the remaining tanks into a different fluid bank.
export const tankSide = (slot?: number): "Left" | "Right" | undefined =>
  [1, 2, 3].includes(slot ?? 0) ? "Left" : [4, 5, 6].includes(slot ?? 0) ? "Right" : undefined;
