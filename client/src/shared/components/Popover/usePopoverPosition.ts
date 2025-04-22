import {
  CSSProperties,
  MutableRefObject,
  useEffect,
  useLayoutEffect,
  useState,
} from "react";
import { minimalMargin } from "@/shared/components/Popover/constants";
import { Position, positions } from "@/shared/constants";
import useMeasure, { UseMeasureRect } from "@/shared/hooks/useMeasure";
import { useWindowDimensions } from "@/shared/hooks/useWindowDimensions";

const computeBasePosition = (
  triggerRect: UseMeasureRect,
  popoverRect: UseMeasureRect,
  offset: number,
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

  return { top, left };
};

const usePopoverPosition = (
  triggerRef: MutableRefObject<HTMLDivElement | null>,
  offset: number,
  isTriggerFixed: boolean,
  position?: Position,
) => {
  const { width: viewportWidth, height: viewportHeight } =
    useWindowDimensions();

  const [popoverAnimation, setPopoverAnimation] = useState("");
  const [popoverStyle, setPopoverStyle] = useState({
    visibility: "hidden",
  } as CSSProperties);

  const [popoverRef, , popoverRect] = useMeasure<HTMLDivElement>();
  const [triggerRect, setTriggerRect] = useState<UseMeasureRect | null>(null);
  const [initialPageOffset, setInitialPageOffset] = useState<number>(0);

  useEffect(() => {
    if (triggerRef.current) {
      const { x, y, width, height, top, left, bottom, right } =
        triggerRef.current.getBoundingClientRect();
      setTriggerRect({ x, y, width, height, top, left, bottom, right });
      setInitialPageOffset(window.scrollY);
    }
  }, [triggerRef, viewportWidth, viewportHeight]);

  const flipPosition = (position?: Position): Position | undefined => {
    if (!position) {
      return;
    }

    const TOP = "top";
    const BOTTOM = "bottom";

    if (position.startsWith(TOP))
      return position.replace(TOP, BOTTOM) as Position;
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

      let { top, left } = computeBasePosition(
        triggerRect,
        popoverRect,
        offset,
        finalPosition,
      );

      // handle overflow on top
      // top position on page is less than some margin
      if (top + triggerRect.top < minimalMargin) {
        // flip position from top to bottom
        finalPosition = flipPosition(finalPosition);
        ({ top, left } = computeBasePosition(
          triggerRect,
          popoverRect,
          offset,
          finalPosition,
        ));
      }

      // handle overflow on bottom
      // top position on page + height of popover is greater than viewport height minus some margin
      if (
        top + triggerRect.bottom + popoverRect.height >
        viewportHeight - minimalMargin
      ) {
        // flip position from bottom to top
        finalPosition = flipPosition(finalPosition);
        ({ top, left } = computeBasePosition(
          triggerRect,
          popoverRect,
          offset,
          finalPosition,
        ));
      }

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
      if (
        left + triggerRect.left + popoverRect.width >
        viewportWidth - minimalMargin
      ) {
        // width of popover exceeding trigger on the right
        const rightTriggerOverflow =
          popoverRect.width - triggerRect.width + left;
        // how much of popover is visible on the right side of the trigger
        const notOverflowing =
          viewportWidth - triggerRect.width - triggerRect.left;
        // subtract notOverflowing - how much is not overflowing on the right
        left -= rightTriggerOverflow - notOverflowing + minimalMargin;
      }

      setPopoverAnimation(
        finalPosition?.includes("bottom")
          ? "animate-slide-down-popover"
          : "animate-slide-up-popover",
      );

      // when trigger is fixed on page, popover will be child element of trigger element
      // this way we do not need to recompute position on scroll
      if (!isTriggerFixed) {
        // otherwise popover will be teleported to document body, set absolute document position
        // we assume both popover and trigger element will move in sync on scroll,
        // therefore there is no need to recompute position on scroll
        top = triggerRect.top + top + initialPageOffset;
        left = triggerRect.left + left;
      }
      setPopoverStyle({
        top: `${top}px`,
        left: `${left}px`,
        visibility: "visible",
      });
    };

    computePosition();
  }, [
    triggerRect,
    isTriggerFixed,
    popoverRef,
    popoverRect,
    position,
    offset,
    initialPageOffset,
    viewportWidth,
    viewportHeight,
  ]);

  return { popoverAnimation, popoverStyle, popoverRef };
};

export default usePopoverPosition;
