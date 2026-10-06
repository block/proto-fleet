import clsx from "clsx";

import { sizes } from "@/shared/components/Button";
import { type ButtonProps } from "@/shared/components/ButtonGroup";
import ResponsiveActionGroup from "@/shared/components/ResponsiveActionGroup";
import { useWindowDimensions } from "@/shared/hooks/useWindowDimensions";

interface ModalHeaderActionsProps {
  buttons?: ButtonProps[];
  buttonSize?: keyof typeof sizes;
  className?: string;
  primaryTestIdSuffix?: string;
  renderWhen?: "phone" | "phone-tablet" | "always";
  triggerTestId?: string;
}

const ModalHeaderActions = ({
  buttons,
  buttonSize = sizes.base,
  className,
  primaryTestIdSuffix = "mobile",
  renderWhen = "phone",
  triggerTestId,
}: ModalHeaderActionsProps) => {
  const { isPhone, isTablet } = useWindowDimensions();
  const isCompactViewport = renderWhen === "phone-tablet" ? isPhone || isTablet : isPhone;
  const visibilityClassName =
    renderWhen === "phone-tablet" ? "laptop:hidden" : renderWhen === "phone" ? "tablet:hidden" : undefined;

  if (renderWhen !== "always" && !isCompactViewport) {
    return null;
  }

  return (
    <ResponsiveActionGroup
      buttons={buttons}
      buttonSize={buttonSize}
      className={clsx("ml-3 shrink-0", visibilityClassName, className)}
      primaryTestIdSuffix={primaryTestIdSuffix}
      sheetContentTestId="modal-overflow-sheet-content"
      sheetTestId="modal-overflow-sheet"
      triggerTestId={triggerTestId}
    />
  );
};

export default ModalHeaderActions;
export type { ModalHeaderActionsProps };
