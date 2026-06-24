import { useEffect, useState } from "react";

// Re-renders the caller on a fixed interval so values derived from the current time
// (e.g. whether a maintenance window is active) refresh at their start/end boundary
// without a manual reload.
export const useNow = (intervalMs = 30_000): number => {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), intervalMs);
    return () => clearInterval(id);
  }, [intervalMs]);
  return now;
};
