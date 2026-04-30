import { type RefObject, useLayoutEffect, useRef, useState } from "react";

import { minimalMargin } from "@/shared/components/Popover/constants";
import { useWindowDimensions } from "@/shared/hooks/useWindowDimensions";

export const POPOVER_VIEWPORT_PADDING = minimalMargin * 2;
// Width budget for the nested side-anchored popover. Matches Popover's `popoverSizes.normal` (w-80 = 320px).
export const NESTED_POPOVER_WIDTH = 320;
export const NESTED_GAP = 2;
// Soft floor used before the panel content is measured — keeps the first-pass position
// reasonable while we wait for ResizeObserver to report the natural height.
export const NESTED_MIN_HEIGHT = 120;

export type NestedPopoverPosition = {
  top: number;
  left: number;
  /** undefined = panel renders at its natural size; number = clip natural overflow. */
  maxHeight: number | undefined;
};

/**
 * Position the nested submenu beside the parent popover.
 *
 * - When the natural content height fits in the viewport, the panel renders at its full
 *   natural size and `top` is shifted upward as needed so the bottom stays inside the
 *   viewport. `maxHeight` is undefined.
 * - When the natural content exceeds the viewport, `top` is pinned near the top edge and
 *   `maxHeight` clips the panel; the inner scroll area absorbs the overflow.
 *
 * `contentHeight` is the panel's natural (unconstrained) height in pixels — typically
 * measured via `scrollHeight` after the first paint. Pass `null` on the first render
 * before measurement; the soft minimum is used as a placeholder.
 */
export const computeNestedPosition = (
  parentRect: DOMRect,
  rowRect: DOMRect,
  contentHeight: number | null,
  viewportWidth: number,
  viewportHeight: number,
): NestedPopoverPosition => {
  const desiredLeft = parentRect.right + NESTED_GAP;
  const fitsRight = desiredLeft + NESTED_POPOVER_WIDTH <= viewportWidth - POPOVER_VIEWPORT_PADDING;
  const left = fitsRight
    ? desiredLeft
    : Math.max(POPOVER_VIEWPORT_PADDING, parentRect.left - NESTED_GAP - NESTED_POPOVER_WIDTH);

  const availableHeight = Math.max(0, viewportHeight - 2 * POPOVER_VIEWPORT_PADDING);
  const naturalHeight = contentHeight ?? NESTED_MIN_HEIGHT;
  const renderedHeight = Math.min(naturalHeight, availableHeight);

  // Shift top upward so the rendered panel fits inside the viewport.
  const maxTopForFit = viewportHeight - POPOVER_VIEWPORT_PADDING - renderedHeight;
  const top = Math.max(POPOVER_VIEWPORT_PADDING, Math.min(rowRect.top, maxTopForFit));

  // Only constrain max-height when the natural content actually exceeds the viewport.
  const maxHeight = contentHeight !== null && contentHeight > availableHeight ? availableHeight : undefined;

  return { top, left, maxHeight };
};

type UseFilterDropdownPositionArgs = {
  /** When false, position calculation pauses (e.g., the panel is closed). */
  enabled: boolean;
  /** Element the panel anchors against — typically the row that opened it. */
  triggerRef: RefObject<HTMLElement | null>;
  /** Parent popover surface. The panel renders to the right of this element's right edge. */
  parentRef: RefObject<HTMLElement | null>;
};

type UseFilterDropdownPositionResult = {
  /** Computed position. `null` until the first measurement completes. */
  position: NestedPopoverPosition | null;
  /** Attach to the panel root so the hook can measure its natural height. */
  nestedRef: RefObject<HTMLDivElement | null>;
};

/**
 * Manages position state for a portal-rendered nested popover that anchors to a parent
 * popover surface. Measures the panel's natural height via ResizeObserver and recomputes
 * on viewport resize and ancestor scroll. Hide the panel until `position` is non-null
 * to avoid flashing it at an unmeasured location.
 */
export const useFilterDropdownPosition = ({
  enabled,
  triggerRef,
  parentRef,
}: UseFilterDropdownPositionArgs): UseFilterDropdownPositionResult => {
  const nestedRef = useRef<HTMLDivElement>(null);
  const [position, setPosition] = useState<NestedPopoverPosition | null>(null);
  const { width: windowWidth, height: windowHeight } = useWindowDimensions();

  useLayoutEffect(() => {
    if (!enabled || !triggerRef.current || !parentRef.current) {
      return;
    }

    const updatePosition = () => {
      if (!triggerRef.current || !parentRef.current) return;
      const parentRect = parentRef.current.getBoundingClientRect();
      const rowRect = triggerRef.current.getBoundingClientRect();
      const viewportWidth = window.visualViewport?.width ?? windowWidth;
      const viewportHeight = window.visualViewport?.height ?? windowHeight;
      // scrollHeight reflects the panel's natural (unconstrained) size — the inner scroll
      // div drops its max-height when the outer panel does, so the value is honest.
      const contentHeight = nestedRef.current?.scrollHeight ?? null;
      setPosition(computeNestedPosition(parentRect, rowRect, contentHeight, viewportWidth, viewportHeight));
    };

    updatePosition();

    // Coalesce high-frequency events (scroll fires faster than the browser paints) into
    // one update per animation frame.
    let rafId: number | null = null;
    const scheduleUpdate = () => {
      if (rafId !== null) return;
      rafId = window.requestAnimationFrame(() => {
        rafId = null;
        updatePosition();
      });
    };

    // Re-measure once the portal mounts and whenever its content size changes (selection
    // count badges shifting, scrollbars appearing, etc.). Without this, the first paint
    // uses the soft-min height and the panel may render larger than necessary.
    let observer: ResizeObserver | undefined;
    if (nestedRef.current && typeof ResizeObserver !== "undefined") {
      observer = new ResizeObserver(scheduleUpdate);
      observer.observe(nestedRef.current);
    }

    // Window resize is already covered by useWindowDimensions (windowWidth/windowHeight
    // are deps above), so no separate `window.resize` listener is needed. visualViewport
    // resize fires for zoom changes the shared hook doesn't track, and scroll keeps the
    // anchor in sync when an ancestor scrolls. `passive: true` keeps the scroll thread
    // from blocking on this handler.
    const scrollOptions = { capture: true, passive: true } as const;
    window.addEventListener("scroll", scheduleUpdate, scrollOptions);
    window.visualViewport?.addEventListener("resize", scheduleUpdate);
    return () => {
      if (rafId !== null) window.cancelAnimationFrame(rafId);
      observer?.disconnect();
      window.removeEventListener("scroll", scheduleUpdate, scrollOptions);
      window.visualViewport?.removeEventListener("resize", scheduleUpdate);
    };
  }, [enabled, triggerRef, parentRef, windowWidth, windowHeight]);

  return { position, nestedRef };
};
