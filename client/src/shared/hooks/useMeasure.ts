import { useCallback, useEffect, useLayoutEffect, useRef, useState } from "react";
import { debounce } from "@/shared/utils/utility";

export type UseMeasureRect = Pick<
  DOMRectReadOnly,
  "x" | "y" | "top" | "left" | "right" | "bottom" | "height" | "width"
>;
export type UseMeasureRef<E extends Element = Element> = (element: E | null) => void;

export interface UseMeasureOptions {
  /** Whether to use MutationObserver for DOM changes like animations (default: true) */
  observeMutations?: boolean;
}

export type UseMeasureResult<E extends Element = Element> = [UseMeasureRef<E>, UseMeasureRect, UseMeasureRect];

const defaultState: UseMeasureRect = {
  x: 0,
  y: 0,
  width: 0,
  height: 0,
  top: 0,
  left: 0,
  bottom: 0,
  right: 0,
};

const hasRequiredAPIs = () =>
  typeof window !== "undefined" &&
  typeof document !== "undefined" &&
  typeof ResizeObserver !== "undefined" &&
  typeof MutationObserver !== "undefined";

const MUTATION_DEBOUNCE_MS = 16;

const isRectEqual = (rect1: UseMeasureRect, rect2: UseMeasureRect): boolean => {
  return (
    rect1.width === rect2.width &&
    rect1.height === rect2.height &&
    rect1.x === rect2.x &&
    rect1.y === rect2.y &&
    rect1.top === rect2.top &&
    rect1.left === rect2.left &&
    rect1.bottom === rect2.bottom &&
    rect1.right === rect2.right
  );
};

/**
 * Custom hook for measuring DOM elements using ResizeObserver and MutationObserver
 *
 * @template E - The type of DOM element being measured
 * @param options - Configuration options for the hook behavior
 * @returns A tuple containing:
 *   - ref: Callback ref to attach to the element you want to measure
 *   - contentRect: The content rectangle from ResizeObserver (excludes padding/border)
 *   - boundingRect: The bounding rectangle from getBoundingClientRect (includes padding/border)
 *
 * @example
 * ```tsx
 * const [measureRef, contentRect, boundingRect] = useMeasure<HTMLDivElement>();
 * return <div ref={measureRef}>Content: {contentRect.width} x {contentRect.height}</div>;
 * ```
 */
function useMeasure<E extends Element = Element>(options: UseMeasureOptions = {}): UseMeasureResult<E> {
  const { observeMutations = true } = options;

  const [element, setElement] = useState<E | null>(null);
  const [contentRect, setContentRect] = useState<UseMeasureRect>(defaultState);
  const [boundingRect, setBoundingRect] = useState<UseMeasureRect>(defaultState);

  const resizeObserverRef = useRef<ResizeObserver | null>(null);
  const mutationObserverRef = useRef<MutationObserver | null>(null);

  const getRectFromElement = useCallback((el: Element): UseMeasureRect => {
    try {
      const { x, y, width, height, top, left, bottom, right } = el.getBoundingClientRect();
      return { x, y, width, height, top, left, bottom, right };
    } catch (error) {
      console.warn("useMeasure: Failed to get bounding client rect", error);
      return defaultState;
    }
  }, []);

  const updateBoundingRect = useCallback(() => {
    if (!element || !hasRequiredAPIs()) return;

    const newRect = getRectFromElement(element);
    setBoundingRect((prevRect) => {
      if (!isRectEqual(newRect, prevRect)) {
        return newRect;
      }
      return prevRect;
    });
  }, [element, getRectFromElement]);

  const updateBoundingRectRef = useRef(updateBoundingRect);
  const debouncedUpdateBoundingRectRef = useRef<ReturnType<typeof debounce> | undefined>(undefined);

  useEffect(() => {
    updateBoundingRectRef.current = updateBoundingRect;

    if (debouncedUpdateBoundingRectRef.current == null) {
      debouncedUpdateBoundingRectRef.current = debounce(() => {
        if (hasRequiredAPIs()) {
          updateBoundingRectRef.current();
        }
      }, MUTATION_DEBOUNCE_MS);
    }
  }, [updateBoundingRect]);

  const ref = useCallback((node: E | null) => {
    if (!node) return;
    setElement(node);
  }, []);

  useLayoutEffect(() => {
    if (!element) {
      setContentRect(defaultState);
      setBoundingRect(defaultState);
      return;
    }

    if (!hasRequiredAPIs()) {
      return;
    }

    if (resizeObserverRef.current) {
      resizeObserverRef.current.disconnect();
    }

    if (mutationObserverRef.current) {
      mutationObserverRef.current.disconnect();
    }

    try {
      const resizeObserver = new ResizeObserver((entries) => {
        try {
          const entry = entries[0];
          if (entry && entry.target === element) {
            const { width, height } = entry.contentRect;
            const newContentRect = {
              x: 0,
              y: 0,
              width,
              height,
              top: 0,
              left: 0,
              bottom: height,
              right: width,
            };

            setContentRect((prevRect) => {
              if (!isRectEqual(newContentRect, prevRect)) {
                return newContentRect;
              }
              return prevRect;
            });

            debouncedUpdateBoundingRectRef.current?.();
          }
        } catch (error) {
          console.warn("useMeasure: ResizeObserver callback error", error);
        }
      });

      resizeObserver.observe(element);
      resizeObserverRef.current = resizeObserver;

      if (observeMutations) {
        const mutationObserver = new MutationObserver(() => {
          try {
            debouncedUpdateBoundingRectRef.current?.();
          } catch (error) {
            console.warn("useMeasure: MutationObserver callback error", error);
          }
        });

        mutationObserver.observe(element, {
          attributes: true,
          attributeFilter: ["style", "class"],
          childList: true,
          subtree: true,
        });

        mutationObserverRef.current = mutationObserver;
      }

      const initialBoundingRect = getRectFromElement(element);
      const initialContentRect = {
        x: 0,
        y: 0,
        width: initialBoundingRect.width,
        height: initialBoundingRect.height,
        top: 0,
        left: 0,
        bottom: initialBoundingRect.height,
        right: initialBoundingRect.width,
      };

      setBoundingRect(initialBoundingRect);
      setContentRect(initialContentRect);
    } catch (error) {
      console.error("useMeasure: Failed to setup observers", error);
    }

    return () => {
      if (resizeObserverRef.current) {
        resizeObserverRef.current.disconnect();
        resizeObserverRef.current = null;
      }

      if (mutationObserverRef.current) {
        mutationObserverRef.current.disconnect();
        mutationObserverRef.current = null;
      }
    };
  }, [element, getRectFromElement, observeMutations]);

  return [ref, contentRect, boundingRect];
}

export default useMeasure;
