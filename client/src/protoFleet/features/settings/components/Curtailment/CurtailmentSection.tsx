import { type ReactElement, useState } from "react";
import { Info } from "@/shared/assets/icons";
import { iconSizes } from "@/shared/assets/icons/constants";
import Button, { sizes, variants } from "@/shared/components/Button";
import Popover, { popoverSizes, usePopover } from "@/shared/components/Popover";
import { positions } from "@/shared/constants";
import { classNameToSelectors } from "@/shared/utils/cssUtils";

type InfoToggleContentProps = {
  ariaLabel: string;
  description: string;
  testId: string;
  triggerClassName: string;
};

export function InfoToggleContent({
  ariaLabel,
  description,
  testId,
  triggerClassName,
}: InfoToggleContentProps): ReactElement {
  const [isOpen, setIsOpen] = useState(false);
  const { triggerRef } = usePopover();
  const closeIgnoreSelectors = classNameToSelectors(triggerClassName);

  return (
    <div ref={triggerRef} className={`${triggerClassName} relative`}>
      <Button
        variant={variants.secondary}
        size={sizes.compact}
        ariaHasPopup
        ariaExpanded={isOpen}
        ariaLabel={ariaLabel}
        prefixIcon={<Info width={iconSizes.small} className="text-text-primary-70" />}
        onClick={() => setIsOpen((current) => !current)}
      />
      {isOpen ? (
        <Popover
          position={positions["bottom left"]}
          size={popoverSizes.normal}
          offset={8}
          className="!space-y-0"
          closePopover={() => setIsOpen(false)}
          closeIgnoreSelectors={closeIgnoreSelectors}
          testId={testId}
        >
          <p className="text-300 text-text-primary-70">{description}</p>
        </Popover>
      ) : null}
    </div>
  );
}

type SectionHeaderProps = {
  title: string;
  buttonText: string;
  onButtonClick?: () => void;
  infoToggle?: ReactElement;
  variant?: "settings" | "automations";
};

export function SectionHeader({
  title,
  buttonText,
  onButtonClick,
  infoToggle,
  variant = "automations",
}: SectionHeaderProps): ReactElement {
  return (
    <div
      className={variant === "settings" ? "curtailment-section-header" : "mb-4 flex items-center justify-between gap-4"}
    >
      <div className={variant === "settings" ? "curtailment-section-header__title" : "flex min-w-0 items-center gap-4"}>
        <h2
          className={
            variant === "settings"
              ? "curtailment-section-header__label"
              : "truncate text-heading-200 font-medium text-text-primary"
          }
        >
          {title}
        </h2>
      </div>
      <div className="flex shrink-0 items-center gap-2">
        {infoToggle}
        {onButtonClick ? (
          <Button
            variant={variants.secondary}
            size={sizes.compact}
            text={buttonText}
            onClick={onButtonClick}
            className={variant === "settings" ? "curtailment-settings__action-button" : undefined}
          />
        ) : null}
      </div>
    </div>
  );
}
