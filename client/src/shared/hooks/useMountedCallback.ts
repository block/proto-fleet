import { useCallback, useInsertionEffect, useRef } from "react";

// Let asynchronous work outlive its initiating row, but not the data owner.
export function useMountedCallback(callback?: () => void) {
  const mounted = useRef(false);
  const latest = useRef(callback);
  // Publish only committed callbacks, before descendants run layout or passive effects.
  useInsertionEffect(() => {
    latest.current = callback;
  }, [callback]);
  useInsertionEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);
  return useCallback(() => {
    if (mounted.current) latest.current?.();
  }, []);
}
