import clsx from "clsx";
import { createPortal } from "react-dom";

import "./style.css";

import { minimalMargin, popoverSizes } from "./constants";
import { usePopover } from "./usePopover";
import { groupVariants } from "@/shared/components/ButtonGroup";
import PopoverContent from "@/shared/components/Popover/PopoverContent";
import { PopoverContentProps } from "@/shared/components/Popover/types";
import usePopoverPosition from "@/shared/components/Popover/usePopoverPosition";
import { Position } from "@/shared/constants";
import { useWindowDimensions } from "@/shared/hooks/useWindowDimensions";

type PopoverProps = PopoverContentProps & {
  position?: Position;
  offset?: number;
  xOffset?: number;
  yOffset?: number;
  /**
   * Anchor the popover to the trigger's position at the moment of mount and stop
   * tracking it afterwards. Renders portal-fixed so layout shifts (e.g. chips inserted
   * before the trigger button) don't drag the popover sideways while it's open.
   */
  freezePosition?: boolean;
  /**
   * Suppress the viewport-overflow-driven position flip. Use when the caller
   * is responsible for choosing the popover's position (e.g. Select picking
   * bottom/top based on forceBelow + available space) and a flip would
   * override that decision.
   */
  disableAutoFlip?: boolean;
  /**
   * Cap the popover's height to the visible viewport (portal-fixed only) so a
   * menu taller than the screen scrolls internally instead of overflowing the
   * tappable area. Tracks `visualViewport`, so it holds under pinch-zoom and
   * mobile browser-chrome collapse where a CSS `100vh` cap would not. Pair with
   * `overflow-y-auto` on the popover's className to make the overflow scroll.
   */
  constrainHeightToViewport?: boolean;
};

/**
 * Popover component to display a popover with optional title, subtitle, and buttons.
 * The popover is positioned relative to a trigger element and will adjust its position to avoid overflow.
 * To supply a trigger element, use the `usePopover` hook together with `PopoverProvider`.
 *
 * When supplying trigger element, you should also specify whether the trigger element has fixed position on the page.
 * The default value is false (element is not fixed).
 * Popover with fixed trigger element is rendered as child element of the trigger element.
 * Otherwise, popover is rendered as child element of the body.
 * This way we avoid usage of scroll listeners in both cases.
 *
 * @param {Object} props - The properties object.
 * @param {keyof typeof groupVariants} [props.buttonGroupVariant=groupVariants.fill] - The variant of the button group.
 * @param {ButtonProps[]} [props.buttons] - The buttons to display in the popover.
 * @param {ReactNode} [props.children] - The content to display inside the popover.
 * @param {string} [props.className] - Additional class names to apply to the popover.
 * @param {Position} [props.position] - The position of the popover relative to the trigger element.
 * @param {number} [props.offset=minimalMargin] - The offset of the popover from the trigger element.
 * @param {number} [props.xOffset=0] - Additional horizontal offset in pixels (positive moves right, negative moves left).
 * @param {number} [props.yOffset=0] - Additional vertical offset in pixels (positive moves down, negative moves up).
 * @param {keyof typeof popoverSizes} [props.size=popoverSizes.normal] - The size of the popover.
 * @param {string} [props.subtitle] - The subtitle of the popover.
 * @param {string} [props.testId] - The test ID for the popover.
 * @param {string} [props.title] - The title of the popover.
 * @param {string} [props.titleSize="text-heading-200"] - The size of the title text.
 * @returns {JSX.Element} The rendered popover component.
 */
const Popover = ({
  buttonGroupVariant = groupVariants.fill,
  buttons,
  children,
  className,
  position,
  offset = minimalMargin,
  xOffset = 0,
  yOffset = 0,
  size = popoverSizes.normal,
  subtitle,
  testId,
  title,
  titleSize = "text-heading-200",
  closePopover,
  closeIgnoreSelectors = [],
  freezePosition = false,
  disableAutoFlip = false,
  constrainHeightToViewport = false,
}: PopoverProps) => {
  const { triggerRef, renderMode: contextRenderMode } = usePopover();
  const { isPhone } = useWindowDimensions();
  const canDismissPopover = typeof closePopover === "function";
  // Frozen popovers must be portal'd to body so they're positioned relative to the
  // viewport rather than the trigger element's absolute container (which moves with it).
  const renderMode = freezePosition ? "portal-fixed" : contextRenderMode;
  const { popoverAnimation, popoverStyle, popoverRef } = usePopoverPosition(
    triggerRef,
    offset ?? minimalMargin,
    xOffset,
    yOffset,
    renderMode,
    position,
    freezePosition,
    disableAutoFlip,
    constrainHeightToViewport,
  );

  const content = (contentClassName?: string, contentTestId?: string) => (
    <PopoverContent
      buttonGroupVariant={buttonGroupVariant}
      buttons={buttons}
      children={children}
      className={clsx(className, contentClassName)}
      size={size}
      subtitle={subtitle}
      title={title}
      titleSize={titleSize}
      closePopover={closePopover}
      closeIgnoreSelectors={closeIgnoreSelectors}
      testId={contentTestId}
    />
  );

  if (isPhone) {
    return createPortal(
      <div
        className="fixed inset-0 z-60 flex items-end bg-grayscale-gray-5"
        data-testid={testId ? `${testId}-sheet` : "popover-sheet"}
        onMouseDown={canDismissPopover ? (event) => event.stopPropagation() : undefined}
        onTouchStart={canDismissPopover ? (event) => event.stopPropagation() : undefined}
        onClick={closePopover}
      >
        <div
          className="w-full rounded-t-2xl bg-surface-elevated-base px-0 pt-2 pb-[max(env(safe-area-inset-bottom),16px)]"
          onMouseDown={(event) => event.stopPropagation()}
          onTouchStart={(event) => event.stopPropagation()}
          onClick={(event) => event.stopPropagation()}
        >
          {content(
            "!max-h-[calc(100dvh-theme(spacing.10))] !w-full overflow-y-auto overscroll-contain !rounded-t-2xl !rounded-b-none !bg-surface-elevated-base !p-6 !shadow-none !backdrop-blur-none",
            testId,
          )}
        </div>
      </div>,
      document.body,
    );
  }

  const popoverElement = (
    <div
      ref={popoverRef}
      className={clsx(
        "z-50 backdrop-blur-[7px]",
        renderMode === "portal-fixed" ? "fixed" : "absolute",
        // The viewport cap (`maxHeight`) is applied to this positioned wrapper via
        // `popoverStyle`, so the wrapper must also be the scroller — otherwise the
        // unconstrained inner content paints past the cap and clips off-screen.
        // As the scroller, its `overflow-y-auto` clips the inner PopoverContent's
        // `shadow-200`, so re-raise the elevation shadow here on the wrapper — its
        // own box-shadow paints outside the scroll box and isn't clipped (#727 regression).
        // Constrained popovers are scrollable menus and use the `rounded-2xl` menu
        // shape (the only caller, RowActionsMenu, overrides the inner content to match);
        // keep the wrapper radius aligned so the wrapper's shadow and backdrop-blur trace
        // the same corners as the visible surface instead of the wider `rounded-3xl`.
        constrainHeightToViewport ? "overflow-y-auto overscroll-contain rounded-2xl shadow-200" : "rounded-3xl",
        popoverAnimation,
      )}
      style={popoverStyle}
      data-testid={testId}
    >
      {content()}
    </div>
  );

  if (renderMode === "inline") {
    return popoverElement;
  }
  return createPortal(popoverElement, document.body);
};

export default Popover;
