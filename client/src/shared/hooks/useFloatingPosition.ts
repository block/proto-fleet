import { type CSSProperties, useCallback, useEffect, useRef, useState } from "react";
import { clamp } from "@/shared/utils/math";

type VerticalPlacement = "top" | "bottom";
type HorizontalAlignment = "start" | "center" | "end";
type FloatingPlacement = `${VerticalPlacement}-${HorizontalAlignment}`;

const DEFAULT_GAP = 4;
const DEFAULT_VIEWPORT_MARGIN = 8;
/** Minimum usable space (px) before flipping to the opposite side. */
const FLIP_THRESHOLD = 200;

interface Viewport {
  width: number;
  height: number;
}

interface FloatingPositionOptions {
  placement?: FloatingPlacement;
  gap?: number;
  viewportMargin?: number;
  maxHeight?: number;
  /**
   * Known/minimum width of the floating element, used to keep it within viewport bounds.
   * Required for viewport-safe clamping with `center` alignment — without it, the
   * floating element may clip at viewport edges.
   */
  minWidth?: number;
  autoFlip?: boolean;
}

function computeFloatingStyle(rect: DOMRect, viewport: Viewport, options: FloatingPositionOptions = {}): CSSProperties {
  const {
    placement = "bottom-start",
    gap = DEFAULT_GAP,
    viewportMargin = DEFAULT_VIEWPORT_MARGIN,
    maxHeight,
    minWidth = 0,
    autoFlip = true,
  } = options;

  const [preferredVertical, alignment] = placement.split("-") as [VerticalPlacement, HorizontalAlignment];

  const spaceBelow = viewport.height - rect.bottom - viewportMargin;
  const spaceAbove = rect.top - viewportMargin;

  // Flip to the opposite side when preferred side is cramped and the other has more room
  let vertical = preferredVertical;
  if (autoFlip) {
    if (vertical === "bottom" && spaceBelow < FLIP_THRESHOLD && spaceAbove > spaceBelow) {
      vertical = "top";
    } else if (vertical === "top" && spaceAbove < FLIP_THRESHOLD && spaceBelow > spaceAbove) {
      vertical = "bottom";
    }
  }

  const style: CSSProperties = {};

  if (vertical === "bottom") {
    style.top = rect.bottom + gap;
  } else {
    // CSS bottom in fixed positioning: distance from viewport bottom to element bottom
    style.bottom = viewport.height - rect.top + gap;
  }

  if (maxHeight != null) {
    const availableSpace = (vertical === "bottom" ? spaceBelow : spaceAbove) - gap;
    style.maxHeight = Math.min(maxHeight, Math.max(0, availableSpace));
    style.overflowY = "auto";
  }

  const maxLeftEdge = viewport.width - minWidth - viewportMargin;
  const clampToViewport = (left: number) => clamp(left, viewportMargin, maxLeftEdge);

  if (alignment === "start") {
    style.left = clampToViewport(rect.left);
  } else if (alignment === "end") {
    const naturalRight = viewport.width - rect.right;
    const maxRightEdge = minWidth > 0 ? viewport.width - minWidth - viewportMargin : Infinity;
    style.right = clamp(naturalRight, viewportMargin, maxRightEdge);
  } else {
    if (minWidth > 0) {
      const centeredLeft = rect.left + rect.width / 2 - minWidth / 2;
      style.left = clampToViewport(centeredLeft);
    } else {
      // Without a known width, center via CSS transform as a best-effort fallback
      style.left = rect.left + rect.width / 2;
      style.transform = "translateX(-50%)";
    }
  }

  return style;
}

/**
 * Lightweight hook for positioning fixed floating elements (tooltips, popovers)
 * relative to a trigger element with viewport-aware clamping.
 */
function useFloatingPosition<T extends HTMLElement>(options: FloatingPositionOptions = {}) {
  const triggerRef = useRef<T>(null);
  const [isVisible, setIsVisible] = useState(false);
  const [triggerRect, setTriggerRect] = useState<DOMRect | null>(null);

  const show = useCallback(() => {
    const rect = triggerRef.current?.getBoundingClientRect();
    if (rect) {
      setTriggerRect(rect);
      setIsVisible(true);
    }
  }, []);

  const hide = useCallback(() => {
    setTriggerRect(null);
    setIsVisible(false);
  }, []);

  // Re-read trigger position on scroll/resize to keep the floating element anchored.
  // Capture mode catches scroll events from nested scrollable containers.
  useEffect(() => {
    if (!isVisible) return;

    let rafId: number;
    const scheduleUpdate = () => {
      cancelAnimationFrame(rafId);
      rafId = requestAnimationFrame(() => {
        const rect = triggerRef.current?.getBoundingClientRect();
        if (rect) {
          setTriggerRect(rect);
        } else {
          hide();
        }
      });
    };

    window.addEventListener("scroll", scheduleUpdate, { capture: true, passive: true });
    window.addEventListener("resize", scheduleUpdate, { passive: true });

    return () => {
      cancelAnimationFrame(rafId);
      window.removeEventListener("scroll", scheduleUpdate, { capture: true });
      window.removeEventListener("resize", scheduleUpdate);
    };
  }, [isVisible, hide]);

  const floatingStyle = triggerRect
    ? computeFloatingStyle(triggerRect, { width: window.innerWidth, height: window.innerHeight }, options)
    : undefined;

  return { triggerRef, floatingStyle, isVisible, show, hide };
}

export { computeFloatingStyle, useFloatingPosition };
export type { FloatingPlacement, FloatingPositionOptions, Viewport };
