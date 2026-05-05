import { useCallback, useMemo, useState } from "react";
import {
  canonicalizeSearchParams,
  createDefaultSavedViewsRecord,
  createUserView,
  getSavedViewsStorageKey,
  isBuiltInViewId,
  isSavedViewsRecordDefault,
  normalizeSavedViewsRecord,
  type SavedView,
  type SavedViewsRecord,
} from "./savedViews";

const readSavedViewsRecord = (storageKey: string): SavedViewsRecord => {
  try {
    const rawValue = localStorage.getItem(storageKey);
    if (!rawValue) {
      return createDefaultSavedViewsRecord();
    }
    return normalizeSavedViewsRecord(JSON.parse(rawValue));
  } catch {
    return createDefaultSavedViewsRecord();
  }
};

const persistSavedViewsRecord = (storageKey: string, record: SavedViewsRecord): void => {
  try {
    if (isSavedViewsRecordDefault(record)) {
      localStorage.removeItem(storageKey);
      return;
    }
    localStorage.setItem(storageKey, JSON.stringify(record));
  } catch {
    // Ignore persistence failures and continue using in-memory state.
  }
};

type RecordState = {
  storageKey: string;
  record: SavedViewsRecord;
};

export type UseMinerViewsResult = {
  record: SavedViewsRecord;
  addUserView: (input: { name: string; searchParams: string }) => SavedView;
  renameUserView: (id: string, name: string) => void;
  updateUserViewParams: (id: string, searchParams: string) => void;
  deleteUserView: (id: string) => void;
  reorderUserViews: (orderedIds: string[]) => void;
  dismissBuiltInView: (id: string) => void;
  restoreBuiltInView: (id: string) => void;
};

const useMinerViews = (username: string): UseMinerViewsResult => {
  const storageKey = useMemo(() => getSavedViewsStorageKey(username), [username]);
  const [recordState, setRecordState] = useState<RecordState>(() => ({
    storageKey,
    record: readSavedViewsRecord(storageKey),
  }));

  // Re-key when the username changes (e.g. after sign-out/sign-in).
  const record = useMemo(
    () => (recordState.storageKey === storageKey ? recordState.record : readSavedViewsRecord(storageKey)),
    [recordState.record, recordState.storageKey, storageKey],
  );

  // Functional updater so consecutive mutations within a single React batch
  // each read the latest state. Persistence happens inside the updater so it
  // can never lag the in-memory state. Callers that detect a no-op can return
  // the same reference to short-circuit the localStorage write + re-render.
  const commit = useCallback(
    (updater: (prev: SavedViewsRecord) => SavedViewsRecord) => {
      setRecordState((prev) => {
        const base = prev.storageKey === storageKey ? prev.record : readSavedViewsRecord(storageKey);
        const draft = updater(base);
        if (draft === base) return prev;
        const next = normalizeSavedViewsRecord(draft);
        persistSavedViewsRecord(storageKey, next);
        return { storageKey, record: next };
      });
    },
    [storageKey],
  );

  const addUserView = useCallback<UseMinerViewsResult["addUserView"]>(
    ({ name, searchParams }) => {
      const view = createUserView({ name, searchParams });
      commit((current) => ({
        ...current,
        views: [...current.views, view],
      }));
      return view;
    },
    [commit],
  );

  const renameUserView = useCallback<UseMinerViewsResult["renameUserView"]>(
    (id, name) => {
      const trimmed = name.trim();
      if (!trimmed) return;
      commit((current) => {
        const target = current.views.find((view) => view.id === id);
        if (!target || target.name === trimmed) return current;
        return {
          ...current,
          views: current.views.map((view) => (view.id === id ? { ...view, name: trimmed } : view)),
        };
      });
    },
    [commit],
  );

  const updateUserViewParams = useCallback<UseMinerViewsResult["updateUserViewParams"]>(
    (id, searchParams) => {
      const canonical = canonicalizeSearchParams(searchParams);
      commit((current) => {
        const target = current.views.find((view) => view.id === id);
        if (!target || target.searchParams === canonical) return current;
        return {
          ...current,
          views: current.views.map((view) => (view.id === id ? { ...view, searchParams: canonical } : view)),
        };
      });
    },
    [commit],
  );

  const deleteUserView = useCallback<UseMinerViewsResult["deleteUserView"]>(
    (id) => {
      commit((current) => {
        if (!current.views.some((view) => view.id === id)) return current;
        return { ...current, views: current.views.filter((view) => view.id !== id) };
      });
    },
    [commit],
  );

  const reorderUserViews = useCallback<UseMinerViewsResult["reorderUserViews"]>(
    (orderedIds) => {
      const indexById = new Map(orderedIds.map((id, index) => [id, index]));
      commit((current) => {
        const next = [...current.views].sort((a, b) => {
          const aIndex = indexById.has(a.id) ? indexById.get(a.id)! : Number.MAX_SAFE_INTEGER;
          const bIndex = indexById.has(b.id) ? indexById.get(b.id)! : Number.MAX_SAFE_INTEGER;
          return aIndex - bIndex;
        });
        const sameOrder = next.every((view, i) => view.id === current.views[i]?.id);
        if (sameOrder) return current;
        return { ...current, views: next };
      });
    },
    [commit],
  );

  const dismissBuiltInView = useCallback<UseMinerViewsResult["dismissBuiltInView"]>(
    (id) => {
      if (!isBuiltInViewId(id)) return;
      commit((current) =>
        current.deletedBuiltInIds.includes(id)
          ? current
          : { ...current, deletedBuiltInIds: [...current.deletedBuiltInIds, id] },
      );
    },
    [commit],
  );

  const restoreBuiltInView = useCallback<UseMinerViewsResult["restoreBuiltInView"]>(
    (id) => {
      commit((current) =>
        current.deletedBuiltInIds.includes(id)
          ? { ...current, deletedBuiltInIds: current.deletedBuiltInIds.filter((entry) => entry !== id) }
          : current,
      );
    },
    [commit],
  );

  return {
    record,
    addUserView,
    renameUserView,
    updateUserViewParams,
    deleteUserView,
    reorderUserViews,
    dismissBuiltInView,
    restoreBuiltInView,
  };
};

export default useMinerViews;
