import { useCallback, useMemo, useState } from "react";
import {
  areMinerTableColumnPreferencesDefault,
  createDefaultMinerTableColumnPreferences,
  getMinerTableColumnPreferencesStorageKey,
  type MinerTableColumnPreferences,
  normalizeMinerTableColumnPreferences,
} from "./minerTableColumnPreferences";

const readMinerTableColumnPreferences = (storageKey: string): MinerTableColumnPreferences => {
  try {
    const rawValue = localStorage.getItem(storageKey);
    if (!rawValue) {
      return createDefaultMinerTableColumnPreferences();
    }

    return normalizeMinerTableColumnPreferences(JSON.parse(rawValue));
  } catch {
    return createDefaultMinerTableColumnPreferences();
  }
};

const persistMinerTableColumnPreferences = (storageKey: string, preferences: MinerTableColumnPreferences): void => {
  const normalizedPreferences = normalizeMinerTableColumnPreferences(preferences);

  try {
    if (areMinerTableColumnPreferencesDefault(normalizedPreferences)) {
      localStorage.removeItem(storageKey);
      return;
    }

    localStorage.setItem(storageKey, JSON.stringify(normalizedPreferences));
  } catch {
    // Ignore persistence failures and continue using in-memory state.
  }
};

type PreferenceState = {
  storageKey: string;
  preferences: MinerTableColumnPreferences;
};

const useMinerTableColumnPreferences = (username: string) => {
  const storageKey = useMemo(() => getMinerTableColumnPreferencesStorageKey(username), [username]);
  const [preferenceState, setPreferenceState] = useState<PreferenceState>(() => ({
    storageKey,
    preferences: readMinerTableColumnPreferences(storageKey),
  }));
  const preferences = useMemo(
    () =>
      preferenceState.storageKey === storageKey
        ? preferenceState.preferences
        : readMinerTableColumnPreferences(storageKey),
    [preferenceState.preferences, preferenceState.storageKey, storageKey],
  );

  const setPreferences = useCallback(
    (nextPreferences: MinerTableColumnPreferences) => {
      const normalizedPreferences = normalizeMinerTableColumnPreferences(nextPreferences);
      setPreferenceState({
        storageKey,
        preferences: normalizedPreferences,
      });
      persistMinerTableColumnPreferences(storageKey, normalizedPreferences);
    },
    [storageKey],
  );

  return {
    preferences,
    setPreferences,
  };
};

export default useMinerTableColumnPreferences;
