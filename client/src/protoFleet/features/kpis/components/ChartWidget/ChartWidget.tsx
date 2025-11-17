import { ReactNode } from "react";

type ChartWidgetProps = {
  label: string;
  value: string | number;
  units?: string;
  children: ReactNode;
  className?: string;
};

const ChartWidget = ({
  label,
  value,
  units,
  children,
  className = "",
}: ChartWidgetProps) => {
  return (
    <div className={`rounded-xl bg-surface-base p-10 ${className}`}>
      <div className="mb-6">
        <div className="text-heading-50 text-text-primary-70">{label}</div>
        <div className="text-heading-300 text-text-primary">
          {value}
          {units && (
            <span className={units === "%" ? "text-heading-200" : "ml-1"}>
              {units}
            </span>
          )}
        </div>
      </div>
      <div className="flex">{children}</div>
    </div>
  );
};

export default ChartWidget;
