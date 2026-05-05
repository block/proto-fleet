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
}: PopoverProps) => {
  const { triggerRef, renderMode: contextRenderMode } = usePopover();
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
  );

  const popoverElement = (
    <div
      ref={popoverRef}
      className={clsx(
        "z-50 rounded-3xl backdrop-blur-[7px]",
        renderMode === "portal-fixed" ? "fixed" : "absolute",
        popoverAnimation,
      )}
      style={popoverStyle}
      data-testid={testId}
    >
      <PopoverContent
        buttonGroupVariant={buttonGroupVariant}
        buttons={buttons}
        children={children}
        className={className}
        size={size}
        subtitle={subtitle}
        title={title}
        titleSize={titleSize}
        closePopover={closePopover}
        closeIgnoreSelectors={closeIgnoreSelectors}
      />
    </div>
  );

  if (renderMode === "inline") {
    return popoverElement;
  }
  return createPortal(popoverElement, document.body);
};

export default Popover;
