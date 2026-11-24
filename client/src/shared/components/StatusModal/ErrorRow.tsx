import { ReactNode } from "react";
import clsx from "clsx";
import { ChevronDown } from "@/shared/assets/icons";
import { iconSizes } from "@/shared/assets/icons/constants";
import { DialogIcon } from "@/shared/components/Dialog";
import Row from "@/shared/components/Row";

export interface ErrorRowProps {
  icon: ReactNode;
  title: string;
  subtitle?: string;
  onClick?: () => void;
  action?: ReactNode;
  divider?: boolean;
}

/**
 * Generic error row component used in status modals
 * Handles consistent layout for both MinerStatusModal and ComponentStatusModal
 */
const ErrorRow = ({ icon, title, subtitle, onClick, action, divider = true }: ErrorRowProps) => {
  const content = (
    <Row
      prefixIcon={
        <div className="shrink-0 text-text-primary-70">
          <DialogIcon>{icon}</DialogIcon>
        </div>
      }
      className={clsx("flex items-center justify-between text-emphasis-300", action && "flex-wrap gap-x-3 gap-y-2")}
      compact
      divider={divider}
    >
      <div className={clsx("min-w-0 flex-1 py-2 pr-2", action && "basis-40")}>
        <div className="text-emphasis-300 text-text-primary">{title}</div>
        {subtitle ? <div className="mt-0.5 text-200 text-text-primary-70">{subtitle}</div> : null}
      </div>

      {action ? <div className="ml-auto shrink-0">{action}</div> : null}

      {onClick ? (
        <div className="ml-2 shrink-0">
          <ChevronDown width={iconSizes.small} className="rotate-270 text-text-primary-70" />
        </div>
      ) : null}
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
