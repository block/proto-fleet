import { Fragment, type ReactNode, useCallback, useEffect, useState } from "react";

import { Ellipsis } from "@/shared/assets/icons";
import { iconSizes } from "@/shared/assets/icons/constants";
import ActionSheet from "@/shared/components/ActionSheet";
import Button, { type ButtonVariant, sizes, variants } from "@/shared/components/Button";
import Divider from "@/shared/components/Divider";
import Popover, { PopoverProvider, popoverSizes, usePopover } from "@/shared/components/Popover";
import Row from "@/shared/components/Row";
import { positions } from "@/shared/constants";
import { useClickOutside } from "@/shared/hooks/useClickOutside";
import { useWindowDimensions } from "@/shared/hooks/useWindowDimensions";

export interface RowAction {
  label: string;
  onClick: () => void;
  icon?: ReactNode;
  // Thick divider rendered below the row; suppressed on the last row.
  showGroupDivider?: boolean;
  hidden?: boolean;
  disabled?: boolean;
  testId?: string;
}

interface RowActionsMenuProps {
  actions: RowAction[];
  ariaLabel?: string;
  testIdPrefix?: string;
  // Falls back to `${testIdPrefix}-trigger` / `row-actions-menu-trigger`.
  triggerTestId?: string;
  disabled?: boolean;
  triggerLabel?: string;
  triggerClassName?: string;
  triggerVariant?: ButtonVariant;
  triggerSuffixIcon?: ReactNode;
  onOpenChange?: (open: boolean) => void;
  popoverTestId?: string;
}

const RowActionsMenu = ({
  actions,
  ariaLabel = "Row actions",
  testIdPrefix,
  triggerTestId,
  disabled,
  triggerLabel,
  triggerClassName,
  triggerVariant,
  triggerSuffixIcon,
  onOpenChange,
  popoverTestId,
}: RowActionsMenuProps) => (
  <PopoverProvider>
    <RowActionsMenuInner
      actions={actions}
      ariaLabel={ariaLabel}
      testIdPrefix={testIdPrefix}
      triggerTestId={triggerTestId}
      disabled={disabled}
      triggerLabel={triggerLabel}
      triggerClassName={triggerClassName}
      triggerVariant={triggerVariant}
      triggerSuffixIcon={triggerSuffixIcon}
      onOpenChange={onOpenChange}
      popoverTestId={popoverTestId}
    />
  </PopoverProvider>
);

const RowActionsMenuInner = ({
  actions,
  ariaLabel,
  testIdPrefix,
  triggerTestId,
  disabled,
  triggerLabel,
  triggerClassName,
  triggerVariant,
  triggerSuffixIcon,
  onOpenChange,
  popoverTestId: popoverTestIdProp,
}: Required<Pick<RowActionsMenuProps, "actions" | "ariaLabel">> &
  Pick<
    RowActionsMenuProps,
    | "testIdPrefix"
    | "triggerTestId"
    | "disabled"
    | "triggerLabel"
    | "triggerClassName"
    | "triggerVariant"
    | "triggerSuffixIcon"
    | "onOpenChange"
    | "popoverTestId"
  >) => {
  const { triggerRef, setPopoverRenderMode } = usePopover();
  const { isPhone } = useWindowDimensions();
  const [isOpen, setIsOpen] = useState(false);
  const resolvedTriggerTestId =
    triggerTestId ?? (testIdPrefix ? `${testIdPrefix}-trigger` : "row-actions-menu-trigger");
  const popoverTestId = popoverTestIdProp ?? (testIdPrefix ? `${testIdPrefix}-popover` : "row-actions-menu-popover");

  // Portal-fixed keeps the popover above the list's overflow scroll containers.
  useEffect(() => {
    setPopoverRenderMode("portal-fixed");
  }, [setPopoverRenderMode]);

  // Disabled hard-closes; re-enable doesn't resurrect — operator must reopen.
  const open = isOpen && !disabled;

  const setMenuOpen = useCallback(
    (nextOpen: boolean) => {
      setIsOpen(nextOpen);
      onOpenChange?.(nextOpen);
    },
    [onOpenChange],
  );

  const onClickOutside = useCallback(() => setMenuOpen(false), [setMenuOpen]);
  useClickOutside({
    ref: triggerRef,
    onClickOutside,
    ignoreSelectors: [".popover-content", `[data-testid="${popoverTestId}"]`],
  });

  const visibleActions = actions.filter((action) => !action.hidden);
  if (visibleActions.length === 0) return null;

  return (
    <div className="relative" ref={triggerRef}>
      <Button
        className={triggerClassName ?? "-my-[10px] !p-[14px]"}
        size={sizes.compact}
        variant={triggerVariant ?? variants.textOnly}
        prefixIcon={triggerLabel ? undefined : <Ellipsis width={iconSizes.small} className="text-text-primary-70" />}
        suffixIcon={triggerSuffixIcon}
        ariaLabel={ariaLabel}
        ariaHasPopup="menu"
        ariaExpanded={open}
        testId={resolvedTriggerTestId}
        disabled={disabled}
        onClick={() => setMenuOpen(!open)}
      >
        {triggerLabel}
      </Button>
      {open && isPhone ? (
        <ActionSheet
          items={visibleActions.map((action) => ({
            disabled: action.disabled,
            icon: action.icon,
            label: action.label,
            onClick: action.onClick,
            showGroupDivider: action.showGroupDivider,
            testId: action.testId,
          }))}
          onClose={() => setMenuOpen(false)}
          contentTestId={popoverTestId}
          testId={`${popoverTestId}-sheet`}
        />
      ) : null}
      {open && !isPhone ? (
        <Popover
          className="!space-y-0 !rounded-2xl px-0 pt-2 pb-1"
          position={positions["bottom right"]}
          size={popoverSizes.small}
          offset={8}
          testId={popoverTestId}
        >
          {visibleActions.map((action, index) => (
            <Fragment key={action.testId ?? `${action.label}-${index}`}>
              <div className="px-4">
                <Row
                  className="text-emphasis-300"
                  prefixIcon={action.icon}
                  testId={action.testId}
                  onClick={() => {
                    setMenuOpen(false);
                    action.onClick();
                  }}
                  disabled={action.disabled}
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
