import { ReactNode } from "react";
import { InfoInverted } from "@/shared/assets/icons";
import { iconSizes } from "@/shared/assets/icons/constants";

interface HardwareCardHeaderProps {
  title: string;
  titleAs?: "span" | "h3";
  infoAriaLabel?: string;
  statusIcon?: ReactNode | null;
  componentIcon?: ReactNode | null;
  onInfoIconClick?: () => void;
  actions?: ReactNode;
}

function HardwareCardHeader({
  title,
  titleAs: Title = "span",
  infoAriaLabel = "More info",
  statusIcon = null,
  componentIcon = null,
  actions = null,
  onInfoIconClick,
}: HardwareCardHeaderProps) {
  return (
    <div className="flex flex-row items-center gap-3 @xs:gap-6">
      <div className="flex min-w-0 basis-full items-center gap-2" title={title}>
        {statusIcon ? <div className="shrink-0">{statusIcon}</div> : null}
        <Title className="truncate text-emphasis-300 text-text-primary">{title}</Title>
      </div>
      <div className="flex shrink-0 items-center gap-2 @xs:gap-3">
        {componentIcon}
        {onInfoIconClick ? (
          <button
            type="button"
            className="rounded-full border-0 bg-core-primary-5 p-1.5 focus-visible:outline-2 focus-visible:outline-core-accent-fill"
            onClick={onInfoIconClick}
            aria-label={infoAriaLabel}
            aria-haspopup="dialog"
          >
            <InfoInverted className="text-text-primary-70" width={iconSizes.small} />
          </button>
        ) : null}
        {actions}
      </div>
    </div>
  );
}

export default HardwareCardHeader;
