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
  // Bakes into the trigger + popover testIds so multiple rows on the
  // same page stay individually addressable (e.g. `<prefix>-trigger`,
  // `<prefix>-popover`). Individual action testIds come from the action
  // entries themselves.
  testIdPrefix?: string;
}

// Ellipsis-trigger row actions menu. Visual + interaction parity with
// `SingleMinerActionsMenu` (Ellipsis button, portal-fixed popover,
// `Row` items with optional thick dividers) but stripped of the
// miner-specific batch/auth/confirmation machinery so non-fleet
// surfaces can reuse the same affordance.
const RowActionsMenu = ({ actions, ariaLabel = "Row actions", testIdPrefix }: RowActionsMenuProps) => (
  <PopoverProvider>
    <RowActionsMenuInner actions={actions} ariaLabel={ariaLabel} testIdPrefix={testIdPrefix} />
  </PopoverProvider>
);

const RowActionsMenuInner = ({
  actions,
  ariaLabel,
  testIdPrefix,
}: Required<Pick<RowActionsMenuProps, "actions" | "ariaLabel">> & { testIdPrefix?: string }) => {
  const { triggerRef, setPopoverRenderMode } = usePopover();
  const [isOpen, setIsOpen] = useState(false);

  // Portal-fixed mirrors SingleMinerActionsMenu — keeps the popover above
  // the list's overflow scroll containers and out of the row click area.
  useEffect(() => {
    setPopoverRenderMode("portal-fixed");
  }, [setPopoverRenderMode]);

  const onClickOutside = useCallback(() => setIsOpen(false), []);
  useClickOutside({
    ref: triggerRef,
    onClickOutside,
    ignoreSelectors: [".popover-content"],
  });

  const visibleActions = actions.filter((action) => !action.hidden);
  if (visibleActions.length === 0) return null;

  const triggerTestId = testIdPrefix ? `${testIdPrefix}-trigger` : "row-actions-menu-trigger";
  const popoverTestId = testIdPrefix ? `${testIdPrefix}-popover` : "row-actions-menu-popover";

  return (
    <div className="relative" ref={triggerRef}>
      <Button
        className="-my-[10px] !p-[14px]"
        size={sizes.compact}
        variant={variants.textOnly}
        prefixIcon={<Ellipsis width={iconSizes.small} className="text-text-primary-70" />}
        ariaLabel={ariaLabel}
        testId={triggerTestId}
        onClick={() => setIsOpen((prev) => !prev)}
      />
      {isOpen ? (
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
