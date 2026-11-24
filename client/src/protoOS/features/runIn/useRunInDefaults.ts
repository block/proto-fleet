import { useCallback, useMemo, useSyncExternalStore } from "react";
import { type RunInDefaults, validateDefaults } from "./defaults";
import { useSystemInfo } from "@/protoOS/store";

const CHANGE_EVENT = "proto-os-run-in-defaults-change";

const subscribe = (notify: () => void) => {
  window.addEventListener("storage", notify);
  window.addEventListener(CHANGE_EVENT, notify);
  return () => {
    window.removeEventListener("storage", notify);
    window.removeEventListener(CHANGE_EVENT, notify);
  };
};

export const useRunInDefaults = () => {
  const deviceId = useSystemInfo()?.identity.deviceId;
  const key = deviceId ? `proto-os:run-in-defaults:v1:${encodeURIComponent(deviceId)}` : undefined;
  const read = useCallback(() => {
    try {
      return key ? localStorage.getItem(key) : null;
    } catch {
      return null;
    }
  }, [key]);
  const stored = useSyncExternalStore(subscribe, read, () => null);
  const defaults = useMemo(() => {
    try {
      const value: unknown = JSON.parse(stored ?? "null");
      return validateDefaults(value) ? value : null;
    } catch {
      return null;
    }
  }, [stored]);
  const save = (value: RunInDefaults) => {
    if (!key || !validateDefaults(value)) throw new Error("Container defaults are unavailable.");
    localStorage.setItem(key, JSON.stringify(value));
    window.dispatchEvent(new Event(CHANGE_EVENT));
  };
  return { defaults, save, deviceId };
};
