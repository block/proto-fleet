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
  renderWhen?: "phone" | "always";
}

const ModalHeaderActions = ({
  buttons,
  buttonSize = sizes.base,
  className,
  primaryTestIdSuffix = "mobile",
  renderWhen = "phone",
}: ModalHeaderActionsProps) => {
  const { isPhone } = useWindowDimensions();

  if (renderWhen === "phone" && !isPhone) {
    return null;
  }

  return (
    <ResponsiveActionGroup
      buttons={buttons}
      buttonSize={buttonSize}
      className={clsx("ml-3 shrink-0 tablet:hidden", className)}
      primaryTestIdSuffix={primaryTestIdSuffix}
      sheetContentTestId="modal-overflow-sheet-content"
      sheetTestId="modal-overflow-sheet"
    />
  );
};

export default ModalHeaderActions;
export type { ModalHeaderActionsProps };
