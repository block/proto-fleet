import { ReactNode, useMemo } from "react";
import clsx from "clsx";
import ComponentMetadata from "./ComponentMetadata";
import StatusModalLayout, { type StatusModalLayoutError } from "./StatusModalLayout";
import type { ComponentStatusModalProps } from "./types";
import { Alert, ControlBoard, Fan, Hashboard, LightningAlt } from "@/shared/assets/icons";
import { iconSizes } from "@/shared/assets/icons/constants";
import { formatTimestamp } from "@/shared/utils/formatTimestamp";

const LabeledValue = ({ label, value }: { label: string; value: ReactNode }) => (
  <div className="flex flex-col">
    <div className="text-heading-200 text-text-primary">{value}</div>
    <div className="text-300 text-text-primary-50">{label}</div>
  </div>
);

const ComponentStatusModalContent = ({
  summary,
  componentType,
  errors,
  metrics,
  metadata,
}: ComponentStatusModalProps) => {
  const hasErrors = errors.length > 0;

  // Create icon with proper sizing and colors to match MinerStatus
  const icon = useMemo(() => {
    const iconClass = hasErrors ? "text-text-critical" : "text-core-primary-20";
    switch (componentType) {
      case "fan":
        return <Fan width={iconSizes.xLarge} className={iconClass} />;
      case "hashboard":
        return <Hashboard width={iconSizes.xLarge} className={iconClass} />;
      case "psu":
        return <LightningAlt width={iconSizes.xLarge} className={iconClass} />;
      case "controlBoard":
        return <ControlBoard width={iconSizes.xLarge} className={iconClass} />;
    }
  }, [componentType, hasErrors]);

  // Transform errors into layout format
  const layoutErrors: StatusModalLayoutError[] = useMemo(
    () =>
      errors.map((error, index) => ({
        key: `error-${index}-${error.timestamp || index}`,
        icon: (
          <div className="flex h-6 w-6 items-center justify-center rounded bg-core-primary-5">
            <Alert className="text-text-critical" width={iconSizes.small} />
          </div>
        ),
        // For ComponentStatus, show message as the title
        title: error.message,
        subtitle: error.timestamp ? formatTimestamp(error.timestamp) : undefined,
        onClick: error.onClick,
      })),
    [errors],
  );

  return (
    <StatusModalLayout icon={icon} title={summary} errors={layoutErrors}>
      {/* Performance metrics grid */}
      {metrics && metrics.length > 0 && (
        <div className={clsx("grid gap-x-4 gap-y-6", metrics.length % 3 === 0 ? "grid-cols-3" : "grid-cols-2")}>
          {metrics.map((metric, index) => (
            <LabeledValue key={`${metric.label}-${index}`} label={metric.label} value={metric.value} />
          ))}
        </div>
      )}

      {/* Metadata section */}
      {metadata && <ComponentMetadata metadata={metadata} />}
    </StatusModalLayout>
  );
};

export default ComponentStatusModalContent;
