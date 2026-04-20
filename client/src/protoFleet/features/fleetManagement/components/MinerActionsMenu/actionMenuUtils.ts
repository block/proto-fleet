import type { BulkAction } from "../BulkActions/types";

export function insertActionAfter<TAction>(
  actions: BulkAction<TAction>[],
  targetAction: TAction,
  insertedAction: BulkAction<TAction>,
): BulkAction<TAction>[] {
  const targetIndex = actions.findIndex((action) => action.action === targetAction);

  if (targetIndex === -1) {
    return actions;
  }

  return [...actions.slice(0, targetIndex + 1), insertedAction, ...actions.slice(targetIndex + 1)];
}

export function insertActionBefore<TAction>(
  actions: BulkAction<TAction>[],
  targetAction: TAction,
  insertedAction: BulkAction<TAction>,
): BulkAction<TAction>[] {
  const targetIndex = actions.findIndex((action) => action.action === targetAction);

  if (targetIndex === -1) {
    return actions;
  }

  return [...actions.slice(0, targetIndex), insertedAction, ...actions.slice(targetIndex)];
}
