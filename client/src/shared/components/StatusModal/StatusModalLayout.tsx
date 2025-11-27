import { ReactNode } from "react";
import ErrorRow from "./ErrorRow";

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

  // Errors section (optional)
  errors?: StatusModalLayoutError[];

  // Additional sections (for extensibility)
  children?: ReactNode;
}

/**
 * Shared layout component for status modals
 * Provides consistent structure for both MinerStatusModal and ComponentStatusModal
 */
const StatusModalLayout = ({ icon, title, subtitle, errors, children }: StatusModalLayoutProps) => {
  return (
    <div className="space-y-6">
      {/* Header section - always rendered consistently */}
      <div className="mt-6 flex flex-col gap-2">
        {icon}
        <div className="text-heading-300 text-text-primary">{title}</div>
        {subtitle && <div className="text-300 text-text-primary-50">{subtitle}</div>}
      </div>

      {/* Error list section */}
      {errors && errors.length > 0 && (
        <div>
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
      )}

      {/* Additional content sections */}
      {children}
    </div>
  );
};

export default StatusModalLayout;
