import { useLayoutEffect } from "react";
import { BulkAction } from "./types";
import Divider from "@/shared/components/Divider";
import Popover, { popoverSizes, usePopover } from "@/shared/components/Popover";
import { minimalMargin } from "@/shared/components/Popover/constants";
import Row from "@/shared/components/Row";
import { type Position, positions } from "@/shared/constants";
import { useWindowDimensions } from "@/shared/hooks/useWindowDimensions";

// Spacing between the trigger and the popover. Passed as the `offset` prop below
// too, so the cap math and the rendered offset can't drift apart.
const POPOVER_OFFSET = 20;
// The open animation settles at translateY(-minimalMargin) (`slideUpPopover`,
// forwards), a permanent upward shift on top of the layout position.
const ANIMATION_TRANSLATE = minimalMargin;
// Total upward shift the popover applies on top of `-popoverHeight`: the layout
// offset correction (`POPOVER_OFFSET` beyond `minimalMargin`) plus the animation.
const UPWARD_SHIFT = POPOVER_OFFSET - minimalMargin + ANIMATION_TRANSLATE;
// Floor so an extremely short viewport still shows a usable, scrollable menu.
const MIN_MENU_HEIGHT = 88;
const MENU_MAX_HEIGHT_VAR = "--bulk-actions-menu-max-h";

// The menu is anchored above the bottom-fixed action bar and opens upward, so a
// tall list would otherwise run off the top of the viewport (#727). We cap its
// height so its top edge stops `minimalMargin` below the viewport top and the
// overflow scrolls internally. Derived from the trigger's measured position and
// the popover's own offset math so it's exact regardless of bar height:
//   topInViewport = trigger.top - popoverHeight - UPWARD_SHIFT + yOffset
// setting that to `minimalMargin` gives the max height used in the effect.

interface BulkActionsPopoverProps<ActionType> {
  actions: BulkAction<ActionType>[];
  beforeEach: (requiresConfirmation: boolean) => void;
  testId: string;
  position?: Position;
  className?: string;
  closePopover?: () => void;
}

interface ActionItemProps<ActionType> {
  action: BulkAction<ActionType>;
  onAction: (action: BulkAction<ActionType>) => void;
}

const ActionItem = <ActionType,>({ action, onAction }: ActionItemProps<ActionType>) => {
  const isDisabled = action.disabled === true;
  return (
    <>
      <div className="px-4" title={isDisabled ? action.disabledReason : undefined}>
        <Row
          className={isDisabled ? "cursor-not-allowed text-emphasis-300 opacity-50" : "text-emphasis-300"}
          prefixIcon={action.icon}
          testId={action.action + "-popover-button"}
          onClick={() => onAction(action)}
          disabled={isDisabled}
          compact
          divider={false}
        >
          {action.title}
        </Row>
      </div>
      {action.showGroupDivider ? <Divider dividerStyle="thick" /> : null}
    </>
  );
};

const BulkActionsPopover = <ActionType,>({
  actions,
  beforeEach,
  testId,
  position = positions["top left"],
  className,
  closePopover,
}: BulkActionsPopoverProps<ActionType>) => {
  const { isPhone, isTablet } = useWindowDimensions();
  const { triggerRef } = usePopover();
  const yOffset = isPhone || isTablet ? -32 : 0;

  // Keep the max-height in sync with the trigger's viewport position (phones use a
  // full-height bottom sheet instead, so skip them). The value is published as a CSS
  // variable on the trigger container; the inline popover is a DOM descendant, so it
  // inherits it via `max-h-[var(...)]` below.
  useLayoutEffect(() => {
    const trigger = triggerRef.current;
    // Only the default (upward, bottom-bar-anchored) menu needs the cap; callers
    // that pass their own className position the menu themselves.
    if (isPhone || !trigger || className) return;

    const update = () => {
      const triggerTop = trigger.getBoundingClientRect().top;
      const available = Math.max(triggerTop - UPWARD_SHIFT + yOffset - minimalMargin, MIN_MENU_HEIGHT);
      trigger.style.setProperty(MENU_MAX_HEIGHT_VAR, `${available}px`);
    };

    update();
    window.addEventListener("resize", update);
    window.visualViewport?.addEventListener("resize", update);
    return () => {
      window.removeEventListener("resize", update);
      window.visualViewport?.removeEventListener("resize", update);
      trigger.style.removeProperty(MENU_MAX_HEIGHT_VAR);
    };
  }, [triggerRef, isPhone, yOffset, className]);

  const onAction = (action: BulkAction<ActionType>) => {
    beforeEach(action.requiresConfirmation);
    action.actionHandler();
  };
  return (
    <Popover
      className={
        className ??
        // Cap the menu height (see MENU_MAX_HEIGHT_VAR above) and scroll internally so
        // it can't run off the top when opening upward from the bottom-anchored action
        // bar on short viewports (#727).
        "-mr-3 max-h-[var(--bulk-actions-menu-max-h,80vh)] !space-y-0 overflow-y-auto overscroll-contain !rounded-2xl px-0 pt-2 pb-1 phone:w-[calc(100vw-theme(spacing.4))]"
      }
      position={position}
      size={popoverSizes.small}
      offset={POPOVER_OFFSET}
      yOffset={isPhone || isTablet ? -32 : 0}
      testId={testId}
      closePopover={closePopover}
    >
      {actions.map((action) => (
        <ActionItem key={action.title} action={action} onAction={onAction} />
      ))}
    </Popover>
  );
};

export default BulkActionsPopover;
