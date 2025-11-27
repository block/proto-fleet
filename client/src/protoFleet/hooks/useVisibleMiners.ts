import { useCallback, useEffect, useRef, useState } from "react";
import { debounce } from "@/shared/utils/utility";

type UseVisibleMinersOptions = {
  /**
   * Root margin for Intersection Observer.
   * Positive values extend the viewport, allowing preloading of nearby items.
   * Default: "200px" (load items 200px before they enter viewport)
   */
  rootMargin?: string;
  /**
   * Debounce delay in ms for visibility updates.
   * Prevents subscription thrashing during fast scrolls.
   * Default: 300ms
   */
  debounceMs?: number;
};

/**
 * Hook to track which miner rows are currently visible in the viewport.
 * Uses Intersection Observer API to efficiently detect visibility changes.
 *
 * @param options - Configuration options for visibility detection
 * @returns Object with visibleMinerIds Set and registerMiner callback
 *
 * @example
 * ```tsx
 * const { visibleMinerIds, registerMiner } = useVisibleMiners();
 *
 * // In list row component:
 * <div ref={(el) => registerMiner(minerId, el)}>
 *   {miner.name}
 * </div>
 * ```
 */
const useVisibleMiners = (options: UseVisibleMinersOptions = {}) => {
  const { rootMargin = "200px", debounceMs = 300 } = options;

  const [visibleMinerIds, setVisibleMinerIds] = useState<Set<string>>(new Set());

  // Track element references and their visibility state
  const elementRefsMap = useRef<Map<string, Element>>(new Map());
  const visibilityMap = useRef<Map<string, boolean>>(new Map());

  const observerRef = useRef<IntersectionObserver | null>(null);
  const debouncedUpdateRef = useRef<(() => void) | null>(null);

  // Create debounced update function
  useEffect(() => {
    debouncedUpdateRef.current = debounce(() => {
      const visible = new Set<string>();
      visibilityMap.current.forEach((isVisible, minerId) => {
        if (isVisible) {
          visible.add(minerId);
        }
      });

      // Only update state if the set of visible IDs actually changed
      setVisibleMinerIds((prev) => {
        // Early exit if sizes differ
        if (visible.size !== prev.size) {
          return visible;
        }

        // Check if contents differ (iterate Set directly, no array allocation)
        for (const id of visible) {
          if (!prev.has(id)) {
            return visible;
          }
        }

        // No changes detected
        return prev;
      });
    }, debounceMs);
  }, [debounceMs]);

  const updateVisibleMiners = useCallback(() => {
    debouncedUpdateRef.current?.();
  }, []);

  // Initialize Intersection Observer
  useEffect(() => {
    observerRef.current = new IntersectionObserver(
      (entries) => {
        entries.forEach((entry) => {
          const minerId = entry.target.getAttribute("data-miner-id");
          if (minerId) {
            visibilityMap.current.set(minerId, entry.isIntersecting);
          }
        });
        updateVisibleMiners();
      },
      {
        rootMargin,
        threshold: 0.1, // Consider visible when 10% of element is in viewport
      },
    );

    return () => {
      observerRef.current?.disconnect();
    };
  }, [rootMargin, updateVisibleMiners]);

  // Register a miner element for visibility tracking
  const registerMiner = useCallback(
    (minerId: string, element: Element | null) => {
      const observer = observerRef.current;
      if (!observer) return;

      // Unobserve previous element for this minerId if it exists
      const prevElement = elementRefsMap.current.get(minerId);
      if (prevElement) {
        observer.unobserve(prevElement);
        elementRefsMap.current.delete(minerId);
        visibilityMap.current.delete(minerId);
      }

      // Observe new element
      if (element) {
        element.setAttribute("data-miner-id", minerId);
        elementRefsMap.current.set(minerId, element);
        observer.observe(element);
      } else {
        // Element removed (cleanup)
        visibilityMap.current.delete(minerId);
        updateVisibleMiners();
      }
    },
    [updateVisibleMiners],
  );

  return {
    visibleMinerIds,
    registerMiner,
  };
};

export default useVisibleMiners;
