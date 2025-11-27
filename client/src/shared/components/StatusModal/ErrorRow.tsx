import { ReactNode } from "react";
import { ChevronDown } from "@/shared/assets/icons";
import { iconSizes } from "@/shared/assets/icons/constants";
import Row from "@/shared/components/Row";

export interface ErrorRowProps {
  icon: ReactNode;
  title: string;
  subtitle?: string;
  onClick?: () => void;
  divider?: boolean;
}

/**
 * Generic error row component used in status modals
 * Handles consistent layout for both MinerStatusModal and ComponentStatusModal
 */
const ErrorRow = ({ icon, title, subtitle, onClick, divider = true }: ErrorRowProps) => {
  const content = (
    <Row prefixIcon={icon} className="flex items-center justify-between text-emphasis-300" compact divider={divider}>
      <div className="min-w-0 flex-1 py-2 pr-2">
        <div className="text-emphasis-300 text-text-primary">{title}</div>
        {subtitle && <div className="mt-0.5 text-200 text-text-primary-70">{subtitle}</div>}
      </div>

      {onClick && (
        <div className="ml-2 shrink-0">
          <ChevronDown width={iconSizes.small} className="rotate-270 text-text-primary-70" />
        </div>
      )}
    </Row>
  );

  // Wrap in button if clickable
  if (onClick) {
    return (
      <button type="button" onClick={onClick} className="w-full text-left">
        {content}
      </button>
    );
  }

  return content;
};

export default ErrorRow;
