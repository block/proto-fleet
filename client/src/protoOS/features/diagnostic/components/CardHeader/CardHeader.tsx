import { ReactNode } from "react";
import { InfoInverted } from "@/shared/assets/icons";
import { iconSizes } from "@/shared/assets/icons/constants";

interface CardHeaderProps {
  title: string;
  statusIcon?: ReactNode | null;
  componentIcon?: ReactNode | null;
  onInfoIconClick?: () => void;
  actions?: ReactNode;
}

function CardHeader({
  title,
  statusIcon = null,
  componentIcon = null,
  actions = null,
  onInfoIconClick,
}: CardHeaderProps) {
  return (
    <div className="flex flex-row items-center gap-6">
      <div className="flex basis-full items-center gap-2 truncate" title={title}>
        <div className="shrink-0">{statusIcon}</div>
        <span className="text-emphasis-300 text-text-primary">{title}</span>
      </div>
      <div className="flex items-center gap-3">
        {componentIcon}
        {onInfoIconClick && (
          <button
            className="rounded-full border-0 bg-core-primary-5 p-1.5"
            onClick={onInfoIconClick}
            aria-label="More info"
          >
            <InfoInverted className="text-text-primary-70" width={iconSizes.small} />
          </button>
        )}
        {actions}
      </div>
    </div>
  );
}

export default CardHeader;
