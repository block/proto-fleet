import { CSSProperties, MutableRefObject, useCallback, useEffect, useLayoutEffect, useState } from "react";
import { minimalMargin } from "@/shared/components/Popover/constants";
import { Position, positions } from "@/shared/constants";
import useMeasure, { UseMeasureRect } from "@/shared/hooks/useMeasure";
import { useWindowDimensions } from "@/shared/hooks/useWindowDimensions";

const isScrollable = (overflowValue: string) => ["auto", "scroll", "overlay"].includes(overflowValue);

const getScrollParents = (element: HTMLElement | null): Array<HTMLElement | Window> => {
  const scrollParents: Array<HTMLElement | Window> = [window];

  let current = element?.parentElement;

  while (current) {
    const styles = window.getComputedStyle(current);

    if (isScrollable(styles.overflow) || isScrollable(styles.overflowX) || isScrollable(styles.overflowY)) {
      scrollParents.push(current);
    }

    current = current.parentElement;
  }

  return scrollParents;
};

const computeBasePosition = (
  triggerRect: UseMeasureRect,
  popoverRect: UseMeasureRect,
  offset: number,
  xOffset: number,
  yOffset: number,
  position?: Position,
) => {
  let top;
  let left;

  switch (position) {
    case positions.top:
      top = -popoverRect.height;
      left = (-popoverRect.width + triggerRect.width) / 2;
      break;
    case positions["top left"]:
      top = -popoverRect.height;
      left = -popoverRect.width + triggerRect.width;
      break;
    case positions["top right"]:
      top = -popoverRect.height;
      left = 0;
      break;
    case positions.bottom:
      top = triggerRect.height + offset;
      left = (-popoverRect.width + triggerRect.width) / 2;
      break;
    case positions["bottom left"]:
      top = triggerRect.height + offset;
      left = -popoverRect.width + triggerRect.width;
      break;
    default:
      // bottom right
      top = triggerRect.height + offset;
      left = 0;
  }

  if (offset > minimalMargin) {
    // correction for bigger offset because animation translates only by minimalMargin (8px)
    if (position?.startsWith("top")) {
      top -= offset - minimalMargin;
    } else {
      top -= minimalMargin;
    }
  }

  // Apply custom offsets
  top += yOffset;
  left += xOffset;

  return { top, left };
};

const clampPosition = (value: number, minValue: number, maxValue: number): number => {
  const safeMaxValue = Math.max(minValue, maxValue);
  return Math.min(Math.max(value, minValue), safeMaxValue);
};

type PopoverRenderMode = "inline" | "portal-fixed" | "portal-scrolling";

const usePopoverPosition = (
  triggerRef: MutableRefObject<HTMLDivElement | null>,
  offset: number,
  xOffset: number,
  yOffset: number,
  renderMode: PopoverRenderMode,
  position?: Position,
) => {
  const { width: viewportWidth, height: viewportHeight } = useWindowDimensions();

  const [popoverAnimation, setPopoverAnimation] = useState("");
  const [popoverStyle, setPopoverStyle] = useState({
    visibility: "hidden",
  } as CSSProperties);

  const [popoverRef, , popoverRect] = useMeasure<HTMLDivElement>();
  const [triggerRect, setTriggerRect] = useState<UseMeasureRect | null>(null);
  const [initialPageOffset, setInitialPageOffset] = useState<number>(0);
  // Track actual visible viewport dimensions (changes with zoom)
  const [visibleViewport, setVisibleViewport] = useState({ width: viewportWidth, height: viewportHeight });

  const updateMeasurements = useCallback(() => {
    if (triggerRef.current) {
      const rect = triggerRef.current.getBoundingClientRect();
      const vv = window.visualViewport;
      const currentViewportHeight = vv?.height ?? viewportHeight;

      // Only update if the trigger is visible in the viewport.
      // When scrolled out of view, getBoundingClientRect returns off-screen coordinates
      // which cause incorrect overflow detection and position flipping.
      const isInViewport = rect.bottom > 0 && rect.top < currentViewportHeight;
      if (!isInViewport) {
        setTriggerRect(null);
        setPopoverStyle({ visibility: "hidden" });
        return;
      }

      const { x, y, width, height, top, left, bottom, right } = rect;
      setTriggerRect({ x, y, width, height, top, left, bottom, right });
      setInitialPageOffset(window.scrollY);

      // Use visualViewport dimensions when available (reflects actual visible area after zoom)
      setVisibleViewport({
        width: vv?.width ?? viewportWidth,
        height: currentViewportHeight,
      });
    }
  }, [triggerRef, viewportWidth, viewportHeight]);

  useEffect(() => {
    updateMeasurements();
  }, [updateMeasurements]);

  useEffect(() => {
    if (renderMode === "inline") {
      return;
    }

    const triggerElement = triggerRef.current;

    if (!triggerElement || typeof ResizeObserver === "undefined") {
      return;
    }

    const resizeObserver = new ResizeObserver(() => {
      updateMeasurements();
    });

    resizeObserver.observe(triggerElement);

    return () => {
      resizeObserver.disconnect();
    };
  }, [renderMode, triggerRef, updateMeasurements]);

  useEffect(() => {
    if (renderMode !== "portal-scrolling") {
      return;
    }

    const triggerElement = triggerRef.current;
    if (!triggerElement) {
      return;
    }

    const scrollParents = getScrollParents(triggerElement);
    const visualViewport = window.visualViewport;

    scrollParents.forEach((scrollParent) => {
      scrollParent.addEventListener("scroll", updateMeasurements, { passive: true });
    });
    visualViewport?.addEventListener("scroll", updateMeasurements);

    return () => {
      scrollParents.forEach((scrollParent) => {
        scrollParent.removeEventListener("scroll", updateMeasurements);
      });
      visualViewport?.removeEventListener("scroll", updateMeasurements);
    };
  }, [renderMode, triggerRef, updateMeasurements]);

  // Listen for visualViewport resize events to detect zoom changes.
  // Browser zoom doesn't change window.innerWidth/Height, but visualViewport.resize fires reliably.
  useEffect(() => {
    const visualViewport = window.visualViewport;
    if (!visualViewport) return;

    visualViewport.addEventListener("resize", updateMeasurements);
    return () => visualViewport.removeEventListener("resize", updateMeasurements);
  }, [updateMeasurements]);

  const flipPosition = (position?: Position): Position | undefined => {
    if (!position) {
      return;
    }

    const TOP = "top";
    const BOTTOM = "bottom";

    if (position.startsWith(TOP)) return position.replace(TOP, BOTTOM) as Position;
    else return position.replace(BOTTOM, TOP) as Position;
  };

  useLayoutEffect(() => {
    if (!popoverRef) return;

    if (triggerRect === null) {
      return;
    }

    const computePosition = () => {
      if (triggerRect === null || !popoverRef) return;

      let finalPosition = position;

      let { top, left } = computeBasePosition(triggerRect, popoverRect, offset, xOffset, yOffset, finalPosition);
      const getPopoverTopInViewport = () => triggerRect.top + top;

      // handle overflow on top
      // top position on page is less than some margin
      if (getPopoverTopInViewport() < minimalMargin) {
        // flip position from top to bottom
        finalPosition = flipPosition(finalPosition);
        ({ top, left } = computeBasePosition(triggerRect, popoverRect, offset, xOffset, yOffset, finalPosition));
      }

      // handle overflow on bottom
      // top position on page + height of popover is greater than viewport height minus some margin
      if (getPopoverTopInViewport() + popoverRect.height > visibleViewport.height - minimalMargin) {
        // flip position from bottom to top
        finalPosition = flipPosition(finalPosition);
        ({ top, left } = computeBasePosition(triggerRect, popoverRect, offset, xOffset, yOffset, finalPosition));
      }

      const alignedLeft = left;

      // handle overflow on the left side
      // left position on page is less than some margin
      if (left + triggerRect.left < minimalMargin) {
        // width of popover exceeding trigger on the left
        const leftTriggerOverflow = left;
        // subtract trigger.left - how much is not overflowing on the left
        left += -leftTriggerOverflow - triggerRect.left + minimalMargin;
      }

      // handle overflow on the right side
      // left position on page + width of popover is greater than viewport width minus some margin
      if (left + triggerRect.left + popoverRect.width > visibleViewport.width - minimalMargin) {
        // width of popover exceeding trigger on the right
        const rightTriggerOverflow = popoverRect.width - triggerRect.width + left;
        // how much of popover is visible on the right side of the trigger
        const notOverflowing = visibleViewport.width - triggerRect.width - triggerRect.left;
        // subtract notOverflowing - how much is not overflowing on the right
        left -= rightTriggerOverflow - notOverflowing + minimalMargin;
      }

      setPopoverAnimation(
        finalPosition?.includes("bottom") ? "animate-slide-down-popover" : "animate-slide-up-popover",
      );

      // Adjust positioning based on render mode
      let style = {
        top: `${top}px`,
        left: `${left}px`,
        visibility: "visible",
      } as CSSProperties;

      if (renderMode === "portal-fixed") {
        // Portal with fixed positioning: use viewport coordinates (no page offset)
        top = triggerRect.top + top;
        left = triggerRect.left + left;

        // If both top and bottom would overflow, keep the popover pinned within viewport bounds.
        // This prevents action rows from being rendered outside the tappable area on small screens.
        top = clampPosition(top, minimalMargin, visibleViewport.height - popoverRect.height - minimalMargin);
        left = clampPosition(left, minimalMargin, visibleViewport.width - popoverRect.width - minimalMargin);

        style = {
          top: `${top}px`,
          left: `${left}px`,
          visibility: "visible",
        };
      } else if (renderMode === "portal-scrolling") {
        // Portal with scrolling: use document coordinates (with page offset)
        top = triggerRect.top + top + initialPageOffset;
        left = triggerRect.left + left;

        style = {
          top: `${top}px`,
          left: `${left}px`,
          visibility: "visible",
        };
      } else if (left === alignedLeft) {
        if (finalPosition === positions["top left"] || finalPosition === positions["bottom left"]) {
          style = {
            top: `${top}px`,
            right: `${-xOffset}px`,
            visibility: "visible",
          };
        } else if (finalPosition === positions["top right"] || finalPosition === positions["bottom right"]) {
          style = {
            top: `${top}px`,
            left: `${xOffset}px`,
            visibility: "visible",
          };
        }
      }

      setPopoverStyle(style);
    };

    computePosition();
  }, [
    triggerRect,
    renderMode,
    popoverRef,
    popoverRect,
    position,
    offset,
    xOffset,
    yOffset,
    initialPageOffset,
    visibleViewport,
  ]);

  return { popoverAnimation, popoverStyle, popoverRef };
};

export default usePopoverPosition;
