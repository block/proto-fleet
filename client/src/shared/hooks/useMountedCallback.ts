import { useCallback, useEffect, useRef } from "react";

// Let asynchronous work outlive its initiating row, but not the data owner.
export function useMountedCallback(callback?: () => void) {
  const mounted = useRef(false);
  const latest = useRef(callback);
  latest.current = callback;
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);
  return useCallback(() => {
    if (mounted.current) latest.current?.();
  }, []);
}
