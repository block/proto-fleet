import { ReactNode } from "react";
import ErrorRow from "./ErrorRow";
import { Alert } from "@/shared/assets/icons";
import { DialogIcon } from "@/shared/components/Dialog";
import Divider from "@/shared/components/Divider";

export interface StatusModalLayoutError {
  key: string;
  icon: ReactNode;
  title: string;
  subtitle?: string;
  onClick?: () => void;
}

export interface StatusModalLayoutProps {
  // Header content - rendered consistently
  icon: ReactNode; // The fully rendered icon (with wrapper if needed)
  title: string | ReactNode;
  subtitle?: string | ReactNode;

  // Secondary title - shown above errors list (e.g. when miner is asleep with errors)
  secondaryTitle?: string | ReactNode;
  secondarySubtitle?: string | ReactNode;

  // Errors section (optional)
  errors?: StatusModalLayoutError[];

  // Additional sections (for extensibility)
  children?: ReactNode;
}

/**
 * Shared layout component for status modals
 * Provides consistent structure for both MinerStatusModal and ComponentStatusModal
 */
const StatusModalLayout = ({
  icon,
  title,
  subtitle,
  secondaryTitle,
  secondarySubtitle,
  errors,
  children,
}: StatusModalLayoutProps) => {
  return (
    <div className="space-y-6">
      {/* Header section - always rendered consistently */}
      <div className="mt-6 flex flex-col gap-2">
        {icon}
        <div className="text-heading-300 text-text-primary">{title}</div>
        {subtitle ? <div className="text-300 text-text-primary-50">{subtitle}</div> : null}
      </div>

      {/* Divider and secondary title when miner is asleep with errors */}
      {secondaryTitle ? (
        <>
          <Divider />
          {/* Secondary section - identical structure to header section */}
          <div className="flex flex-col gap-2">
            <DialogIcon intent="critical">
              <Alert />
            </DialogIcon>
            <div className="text-heading-300 text-text-primary">{secondaryTitle}</div>
            {secondarySubtitle ? <div className="text-300 text-text-primary-50">{secondarySubtitle}</div> : null}
          </div>
        </>
      ) : null}

      {/* Error list section */}
      {errors && errors.length > 0 ? (
        <div>
          {/* Error rows */}
          <div className="flex flex-col">
            {errors.map((error, index) => (
              <ErrorRow
                key={error.key}
                icon={error.icon}
                title={error.title}
                subtitle={error.subtitle}
                onClick={error.onClick}
                divider={index !== errors.length - 1}
              />
            ))}
          </div>
        </div>
      ) : null}

      {/* Additional content sections */}
      {children}
    </div>
  );
};

export default StatusModalLayout;
