import { useCallback, useEffect, useLayoutEffect, useRef, useState } from "react";
import { useSetActionBarVisible } from "@/protoFleet/store";

export default function useActionBarLifecycle(selectedCount: number, onActionBusyChange?: (busy: boolean) => void) {
  const setActionBarVisible = useSetActionBarVisible();
  const [isActionBusy, setIsActionBusy] = useState(false);
  const mountedRef = useRef(true);
  const onActionBusyChangeRef = useRef(onActionBusyChange);
  useLayoutEffect(() => {
    onActionBusyChangeRef.current = onActionBusyChange;
  }, [onActionBusyChange]);

  useEffect(() => {
    setActionBarVisible(selectedCount > 0 && !isActionBusy);
  }, [selectedCount, isActionBusy, setActionBarVisible]);

  useEffect(() => {
    mountedRef.current = true;
    return () => {
      mountedRef.current = false;
      setActionBarVisible(false);
      onActionBusyChangeRef.current?.(false);
    };
  }, [setActionBarVisible]);

  const onActionStart = useCallback(() => {
    if (!mountedRef.current) return;
    setIsActionBusy(true);
    onActionBusyChangeRef.current?.(true);
  }, []);

  const onActionComplete = useCallback(() => {
    // A late completion after navigation must not restore the toaster offset.
    if (!mountedRef.current) return;
    setIsActionBusy(false);
    onActionBusyChangeRef.current?.(false);
  }, []);

  return { isActionBusy, onActionStart, onActionComplete };
}
