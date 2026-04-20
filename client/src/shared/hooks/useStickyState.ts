import { useEffect, useRef, useState } from "react";

type ScrollDirection = "horizontal" | "vertical";
type StickyState = {
  [key in ScrollDirection]: {
    mightStick: boolean;
    isStuck: boolean;
  };
};

const useStickyState = () => {
  const [stickyState, setStickyState] = useState<StickyState>({
    horizontal: { mightStick: false, isStuck: false },
    vertical: { mightStick: false, isStuck: false },
  });

  // Create individual refs (stable across renders)
  const horizontalEndRef = useRef<HTMLDivElement>(null);
  const horizontalStartRef = useRef<HTMLDivElement>(null);
  const verticalEndRef = useRef<HTMLDivElement>(null);
  const verticalStartRef = useRef<HTMLDivElement>(null);

  // Create refs object directly (not memoized, not wrapped in useRef)
  // The object identity will change on each render, but the ref objects inside are stable
  const refs = {
    horizontal: {
      end: horizontalEndRef,
      start: horizontalStartRef,
    },
    vertical: {
      end: verticalEndRef,
      start: verticalStartRef,
    },
  };

  useEffect(() => {
    const observers: IntersectionObserver[] = [];

    // Create observers for both directions
    (["horizontal", "vertical"] as const).forEach((direction) => {
      // Observer for scrollable state (checks if content is larger than container)
      const scrollableObserver = new IntersectionObserver(([entry]) => {
        setStickyState((prev) => ({
          ...prev,
          [direction]: {
            ...prev[direction],
            mightStick: !entry.isIntersecting,
          },
        }));
      }, {});

      // Observer for current sticky state
      const stickyObserver = new IntersectionObserver(([entry]) => {
        setStickyState((prev) => ({
          ...prev,
          [direction]: {
            ...prev[direction],
            isStuck: !entry.isIntersecting,
          },
        }));
      }, {});

      if (refs[direction].end.current) {
        scrollableObserver.observe(refs[direction].end.current);
      }
      if (refs[direction].start.current) {
        stickyObserver.observe(refs[direction].start.current);
      }

      observers.push(scrollableObserver, stickyObserver);
    });

    return () => {
      observers.forEach((observer) => observer.disconnect());
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  return {
    refs,
    stickyState,
  };
};

export { useStickyState };
