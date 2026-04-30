import { useCallback, useEffect, useRef, useState } from "react";

const HOVER_CLOSE_DELAY_MS = 150;

type UseNestedDropdownHoverStateResult = {
  /** Key of the row whose submenu is open, or null when no submenu is active. */
  activeRowKey: string | null;
  /** Open the submenu for `key`, cancelling any pending close. */
  handleRowEnter: (key: string) => void;
  /** Schedule a deferred close — gives the cursor time to traverse to the submenu. */
  scheduleClose: () => void;
  /** Cancel a pending deferred close (e.g., cursor entered the submenu in time). */
  cancelClose: () => void;
  /** Force-close: clears active row, cancels pending close, calls the optional callback. */
  closeAll: () => void;
};

/**
 * Manages the hover state machine for a meta-dropdown whose rows open nested submenus on
 * hover. Centralises the open/close timer so the row and the submenu can hand off the
 * cursor without flicker.
 */
export const useNestedDropdownHoverState = (
  /** Optional side effect run alongside `closeAll` (e.g., closing the parent popover). */
  onClose?: () => void,
): UseNestedDropdownHoverStateResult => {
  const [activeRowKey, setActiveRowKey] = useState<string | null>(null);
  const closeTimerRef = useRef<number | null>(null);

  const cancelClose = useCallback(() => {
    if (closeTimerRef.current !== null) {
      window.clearTimeout(closeTimerRef.current);
      closeTimerRef.current = null;
    }
  }, []);

  const scheduleClose = useCallback(() => {
    cancelClose();
    closeTimerRef.current = window.setTimeout(() => {
      setActiveRowKey(null);
      closeTimerRef.current = null;
    }, HOVER_CLOSE_DELAY_MS);
  }, [cancelClose]);

  const handleRowEnter = useCallback(
    (key: string) => {
      cancelClose();
      setActiveRowKey(key);
    },
    [cancelClose],
  );

  const closeAll = useCallback(() => {
    cancelClose();
    setActiveRowKey(null);
    onClose?.();
  }, [cancelClose, onClose]);

  // Cleanup pending timers on unmount.
  useEffect(() => () => cancelClose(), [cancelClose]);

  return { activeRowKey, handleRowEnter, scheduleClose, cancelClose, closeAll };
};
