import { type RefObject, useEffect, useState } from "react";

interface UseInViewportOptions {
  /**
   * IntersectionObserver root margin. Pre-load before the element fully
   * enters the viewport so a fast scroller doesn't see a skeleton.
   * Defaults to "200px" — about one card-row of pre-roll.
   */
  rootMargin?: string;
  /**
   * Sticky after first sighting: once the element has been visible once,
   * stay reported as visible forever. Useful when the cost we're gating
   * is one-time work (lazy data fetch); for ongoing polling leave this
   * `false` so we suspend again when scrolled away.
   */
  once?: boolean;
}

/**
 * Reports whether the supplied element is intersecting the viewport.
 *
 * Used to throttle expensive per-card work (polling, heavy renders) so
 * that an "All Sites" view with hundreds of building cards doesn't
 * stampede the API every poll tick. Cards below the fold report
 * `false`; flipping into view resumes their work.
 */
// SSR / older browsers without IO default to visible so gated work still
// runs. Resolved at module load — checking `typeof IntersectionObserver`
// inside an effect would force a synchronous setState that the React 19
// lint forbids.
const HAS_INTERSECTION_OBSERVER = typeof IntersectionObserver !== "undefined";

export const useInViewport = (
  ref: RefObject<Element | null>,
  { rootMargin = "200px", once = false }: UseInViewportOptions = {},
): boolean => {
  const [isVisible, setIsVisible] = useState(!HAS_INTERSECTION_OBSERVER);

  useEffect(() => {
    if (!HAS_INTERSECTION_OBSERVER) return undefined;
    const node = ref.current;
    if (!node) return undefined;

    const observer = new IntersectionObserver(
      (entries) => {
        const entry = entries[0];
        if (!entry) return;
        if (entry.isIntersecting) {
          setIsVisible(true);
          if (once) observer.disconnect();
        } else if (!once) {
          setIsVisible(false);
        }
      },
      { rootMargin },
    );
    observer.observe(node);
    return () => observer.disconnect();
  }, [ref, rootMargin, once]);

  return isVisible;
};
