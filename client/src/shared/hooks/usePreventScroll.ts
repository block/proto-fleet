import { useCallback, useLayoutEffect, useRef } from "react";

// Module-level state for reference counting across multiple hook instances.
// Note: This counter may desync during HMR in development, but works correctly in production.
let scrollLockCount = 0;

const lockScroll = () => {
  scrollLockCount++;
  if (scrollLockCount === 1) {
    document.documentElement.style.overflow = "hidden";
  }
};

const unlockScroll = () => {
  scrollLockCount = Math.max(0, scrollLockCount - 1);
  if (scrollLockCount === 0) {
    document.documentElement.style.overflow = "";
  }
};

const usePreventScroll = () => {
  const isLockedRef = useRef(false);

  useLayoutEffect(() => {
    return () => {
      if (isLockedRef.current) {
        unlockScroll();
        isLockedRef.current = false;
      }
    };
  }, []);

  const preventScroll = useCallback(() => {
    if (!isLockedRef.current) {
      lockScroll();
      isLockedRef.current = true;
    }
  }, []);

  return {
    preventScroll,
  };
};

export { usePreventScroll };
