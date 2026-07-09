import { useCallback, useState } from "react";
import clsx from "clsx";

import { Ellipsis } from "@/shared/assets/icons";
import ActionSheet, { type ActionSheetItem } from "@/shared/components/ActionSheet";
import { type ButtonProps as BaseButtonProps, sizes, variants } from "@/shared/components/Button";
import ButtonGroup, {
  type ButtonProps as ButtonGroupButtonProps,
  groupVariants,
} from "@/shared/components/ButtonGroup";

export type ResponsiveActionButton = ButtonGroupButtonProps &
  Pick<BaseButtonProps, "ariaExpanded" | "ariaHasPopup" | "ariaLabel"> & {
    actionSheetLabel?: string;
  };

type PrimaryButtonStrategy = "primary-or-last" | "last";

interface ResponsiveActionGroupProps {
  buttons?: ResponsiveActionButton[];
  buttonGroupClassName?: string;
  buttonSize?: keyof typeof sizes;
  className?: string;
  primaryButtonStrategy?: PrimaryButtonStrategy;
  primaryTestIdSuffix?: string;
  sheetContentTestId?: string;
  sheetTestId?: string;
  triggerAriaLabel?: string;
  triggerTestId?: string;
}

const isDangerVariant = (variant: string) => variant === variants.danger || variant === variants.secondaryDanger;

const getActionSheetLabel = (button: ResponsiveActionButton) =>
  button.actionSheetLabel ?? button.text ?? button.ariaLabel;

const OverflowActionSheet = ({
  overflowButtons,
  onClose,
  sheetContentTestId = "responsive-action-sheet-content",
  sheetTestId = "responsive-action-sheet",
}: {
  overflowButtons: ResponsiveActionButton[];
  onClose: () => void;
  sheetContentTestId?: string;
  sheetTestId?: string;
}) => {
  const actionSheetItems: ActionSheetItem[] = overflowButtons.map((button) => ({
    danger: isDangerVariant(button.variant),
    disabled: button.disabled,
    label: getActionSheetLabel(button),
    loading: button.loading,
    onClick: button.onClick,
    testId: button.testId ? `${button.testId}-overflow-item` : undefined,
  }));

  return (
    <ActionSheet items={actionSheetItems} onClose={onClose} contentTestId={sheetContentTestId} testId={sheetTestId} />
  );
};

const splitButtons = (buttons: ResponsiveActionButton[], primaryButtonStrategy: PrimaryButtonStrategy) => {
  if (buttons.length === 0) {
    return { primaryButton: undefined, overflowButtons: [] };
  }

  if (buttons.length === 1 || primaryButtonStrategy === "last") {
    return {
      primaryButton: buttons[buttons.length - 1],
      overflowButtons: buttons.slice(0, -1),
    };
  }

  let primaryIndex = -1;
  for (let i = buttons.length - 1; i >= 0; i--) {
    if (buttons[i].variant === variants.primary) {
      primaryIndex = i;
      break;
    }
  }

  if (primaryIndex === -1) {
    return {
      primaryButton: buttons[buttons.length - 1],
      overflowButtons: buttons.slice(0, -1),
    };
  }

  return {
    primaryButton: buttons[primaryIndex],
    overflowButtons: buttons.filter((_, index) => index !== primaryIndex),
  };
};

const ResponsiveActionGroup = ({
  buttons,
  buttonGroupClassName,
  buttonSize = sizes.base,
  className,
  primaryButtonStrategy = "primary-or-last",
  primaryTestIdSuffix = "mobile",
  sheetContentTestId,
  sheetTestId,
  triggerAriaLabel = "More actions",
  triggerTestId = "overflow-menu-trigger",
}: ResponsiveActionGroupProps) => {
  const [showOverflowSheet, setShowOverflowSheet] = useState(false);
  const closeSheet = useCallback(() => setShowOverflowSheet(false), []);

  const { primaryButton, overflowButtons } = splitButtons(buttons ?? [], primaryButtonStrategy);

  const compactButtons: ResponsiveActionButton[] = [];
  const overflowTriggerClassName = buttonSize === sizes.compact ? "!h-8 !w-8 !px-0 !py-0" : undefined;

  if (overflowButtons.length > 0) {
    compactButtons.push({
      variant: variants.secondary,
      onClick: () => setShowOverflowSheet(true),
      prefixIcon: <Ellipsis />,
      testId: triggerTestId,
      className: overflowTriggerClassName,
      ariaHasPopup: "dialog",
      ariaLabel: triggerAriaLabel,
    });
  }

  if (primaryButton) {
    compactButtons.push({
      ...primaryButton,
      testId: primaryButton.testId ? `${primaryButton.testId}-${primaryTestIdSuffix}` : undefined,
    });
  }

  if (compactButtons.length === 0) {
    return null;
  }

  return (
    <>
      <div className={className}>
        <ButtonGroup
          buttons={compactButtons}
          className={clsx("phone:flex-nowrap phone:space-y-0", buttonGroupClassName)}
          variant={groupVariants.rightAligned}
          size={buttonSize}
        />
      </div>
      {showOverflowSheet ? (
        <OverflowActionSheet
          overflowButtons={overflowButtons}
          onClose={closeSheet}
          sheetContentTestId={sheetContentTestId}
          sheetTestId={sheetTestId}
        />
      ) : null}
    </>
  );
};

export default ResponsiveActionGroup;
