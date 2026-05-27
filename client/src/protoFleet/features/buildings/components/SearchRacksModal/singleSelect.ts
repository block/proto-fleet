// Reduces the List component's multi-select callback into a single-
// select store. The List component doesn't ship a singleSelect prop,
// so callers that want one-item-at-a-time semantics enforce it on the
// set-side. Extracted so the reduce can be unit-tested without
// standing up the full Modal.

export const reduceToSingleSelection = (currentSelected: string[], incoming: unknown): string[] => {
  // Defensive: List always sends an array, but the type signature
  // allows wider input.
  const next = Array.isArray(incoming) ? incoming.filter((v): v is string => typeof v === "string") : [];
  if (next.length <= 1) return next;
  // Multi-select case: the user toggled something on. Find the new
  // id (not in the prior selection) and keep only that one.
  const newId = next.find((id) => !currentSelected.includes(id));
  return newId ? [newId] : [next[next.length - 1]];
};
