import { useEffect, useState } from "react";

/** Keep CT1 observation ages current between polls, including after polling stops. */
export function usePsuObservationTime(enabled = true) {
  const [now, setNow] = useState(Date.now);
  useEffect(() => {
    if (!enabled) return;
    const timer = setInterval(() => setNow(Date.now()), 1_000);
    return () => clearInterval(timer);
  }, [enabled]);
  return now;
}
