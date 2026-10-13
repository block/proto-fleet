import { useCallback, useEffect, useRef, useState } from "react";
import { useSetActionBarVisible } from "@/protoFleet/store";

export default function useActionBarLifecycle(selectedCount: number, onActionBusyChange?: (busy: boolean) => void) {
  const setActionBarVisible = useSetActionBarVisible();
  const [isActionBusy, setIsActionBusy] = useState(false);
  const mountedRef = useRef(true);

  useEffect(() => {
    setActionBarVisible(selectedCount > 0 && !isActionBusy);
  }, [selectedCount, isActionBusy, setActionBarVisible]);

  useEffect(() => {
    mountedRef.current = true;
    return () => {
      mountedRef.current = false;
      setActionBarVisible(false);
      onActionBusyChange?.(false);
    };
  }, [onActionBusyChange, setActionBarVisible]);

  const onActionStart = useCallback(() => {
    if (!mountedRef.current) return;
    setIsActionBusy(true);
    onActionBusyChange?.(true);
  }, [onActionBusyChange]);

  const onActionComplete = useCallback(() => {
    // A late completion after navigation must not restore the toaster offset.
    if (!mountedRef.current) return;
    setIsActionBusy(false);
    onActionBusyChange?.(false);
  }, [onActionBusyChange]);

  return { isActionBusy, onActionStart, onActionComplete };
}
