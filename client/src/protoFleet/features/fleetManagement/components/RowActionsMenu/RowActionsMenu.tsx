import { Fragment, type ReactNode, useCallback, useEffect, useState } from "react";

import { Ellipsis } from "@/shared/assets/icons";
import { iconSizes } from "@/shared/assets/icons/constants";
import Button, { sizes, variants } from "@/shared/components/Button";
import Divider from "@/shared/components/Divider";
import Popover, { PopoverProvider, popoverSizes, usePopover } from "@/shared/components/Popover";
import Row from "@/shared/components/Row";
import { positions } from "@/shared/constants";
import { useClickOutside } from "@/shared/hooks/useClickOutside";

export interface RowAction {
  label: string;
  onClick: () => void;
  icon?: ReactNode;
  // Renders a thick group divider below this action. The divider is
  // suppressed when this action is the last visible one so menus don't
  // end on a trailing separator.
  showGroupDivider?: boolean;
  // When true, the action is skipped from the rendered list. Useful for
  // permission-gated entries that should not surface at all.
  hidden?: boolean;
  testId?: string;
}

interface RowActionsMenuProps {
  actions: RowAction[];
  ariaLabel?: string;
  // Bakes into the popover testId (`<prefix>-popover`) and acts as the
  // default trigger testId (`<prefix>-trigger`). Individual action
  // testIds come from the action entries themselves.
  testIdPrefix?: string;
  // Override the trigger testId — used by `SingleMinerActionsMenu`,
  // which historically exposed `single-miner-actions-menu-button` as
  // the trigger handle. Falls back to `${testIdPrefix}-trigger` /
  // `row-actions-menu-trigger`.
  triggerTestId?: string;
  // Disables the ellipsis trigger. Used by `FleetGroupActionsMenu`
  // while the lazy device-id fetch is in flight so a second click
  // doesn't double-trigger.
  disabled?: boolean;
}

// Ellipsis-trigger row actions menu shared by the fleet-management
// row menus (`SingleMinerActionsMenu`, `FleetGroupActionsMenu`, plus
// the Sites / Buildings / Racks list rows). Owns the popover shell:
// trigger button, portal-fixed popover, click-outside, row rendering
// with optional thick dividers. Action state (confirmation dialogs,
// modal stacks, batch wiring) stays in the caller.
const RowActionsMenu = ({
  actions,
  ariaLabel = "Row actions",
  testIdPrefix,
  triggerTestId,
  disabled,
}: RowActionsMenuProps) => (
  <PopoverProvider>
    <RowActionsMenuInner
      actions={actions}
      ariaLabel={ariaLabel}
      testIdPrefix={testIdPrefix}
      triggerTestId={triggerTestId}
      disabled={disabled}
    />
  </PopoverProvider>
);

const RowActionsMenuInner = ({
  actions,
  ariaLabel,
  testIdPrefix,
  triggerTestId,
  disabled,
}: Required<Pick<RowActionsMenuProps, "actions" | "ariaLabel">> &
  Pick<RowActionsMenuProps, "testIdPrefix" | "triggerTestId" | "disabled">) => {
  const { triggerRef, setPopoverRenderMode } = usePopover();
  const [isOpen, setIsOpen] = useState(false);

  // Portal-fixed keeps the popover above the list's overflow scroll
  // containers and out of the row click area.
  useEffect(() => {
    setPopoverRenderMode("portal-fixed");
  }, [setPopoverRenderMode]);

  // Treat disabled as a hard-close. Derived so a re-enable doesn't
  // resurrect a previously open popover; the operator clicks the
  // ellipsis again to reopen.
  const open = isOpen && !disabled;

  const onClickOutside = useCallback(() => setIsOpen(false), []);
  useClickOutside({
    ref: triggerRef,
    onClickOutside,
    ignoreSelectors: [".popover-content"],
  });

  const visibleActions = actions.filter((action) => !action.hidden);
  if (visibleActions.length === 0) return null;

  const resolvedTriggerTestId =
    triggerTestId ?? (testIdPrefix ? `${testIdPrefix}-trigger` : "row-actions-menu-trigger");
  const popoverTestId = testIdPrefix ? `${testIdPrefix}-popover` : "row-actions-menu-popover";

  return (
    <div className="relative" ref={triggerRef}>
      <Button
        className="-my-[10px] !p-[14px]"
        size={sizes.compact}
        variant={variants.textOnly}
        prefixIcon={<Ellipsis width={iconSizes.small} className="text-text-primary-70" />}
        ariaLabel={ariaLabel}
        testId={resolvedTriggerTestId}
        disabled={disabled}
        onClick={() => setIsOpen((prev) => !prev)}
      />
      {open ? (
        <Popover
          className="!space-y-0 !rounded-2xl px-0 pt-2 pb-1"
          position={positions["bottom right"]}
          size={popoverSizes.small}
          offset={8}
          testId={popoverTestId}
        >
          {visibleActions.map((action, index) => (
            <Fragment key={action.testId ?? action.label}>
              <div className="px-4">
                <Row
                  className="text-emphasis-300"
                  prefixIcon={action.icon}
                  testId={action.testId}
                  onClick={() => {
                    setIsOpen(false);
                    action.onClick();
                  }}
                  compact
                  divider={false}
                >
                  {action.label}
                </Row>
              </div>
              {action.showGroupDivider && index < visibleActions.length - 1 ? <Divider dividerStyle="thick" /> : null}
            </Fragment>
          ))}
        </Popover>
      ) : null}
    </div>
  );
};

export default RowActionsMenu;
