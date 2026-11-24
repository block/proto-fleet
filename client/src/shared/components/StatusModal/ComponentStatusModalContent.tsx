import { ReactNode, useMemo } from "react";
import clsx from "clsx";
import ComponentMetadata from "./ComponentMetadata";
import StatusModalIcon from "./StatusModalIcon";
import StatusModalLayout, { type StatusModalLayoutError } from "./StatusModalLayout";
import type { ComponentStatusModalProps } from "./types";
import { formatReportedTimestamp } from "./utils";
import { Alert, ControlBoard, Fan, Hashboard, Immersion, LightningAlt } from "@/shared/assets/icons";
import { iconSizes } from "@/shared/assets/icons/constants";

const LabeledValue = ({ label, value }: { label: string; value: ReactNode }) => {
  return (
    <div className="flex flex-col" data-testid="status-modal-metric">
      <div className="text-heading-200 text-text-primary" data-testid="status-modal-metric-value">
        {value}
      </div>
      <div className="text-300 text-text-primary-50" data-testid="status-modal-metric-label">
        {label}
      </div>
    </div>
  );
};

const componentIconClassName = "text-inherit";

const renderComponentIcon = (componentType: ComponentStatusModalProps["componentType"]) => {
  switch (componentType) {
    case "fan":
      return <Fan width={iconSizes.xLarge} className={componentIconClassName} />;
    case "hashboard":
      return <Hashboard width={iconSizes.xLarge} className={componentIconClassName} />;
    case "psu":
      return <LightningAlt width={iconSizes.xLarge} className={componentIconClassName} />;
    case "controlBoard":
      return <ControlBoard width={iconSizes.xLarge} className={componentIconClassName} />;
    case "tank":
      return <Immersion width={iconSizes.xLarge} className={componentIconClassName} />;
    default:
      return <Alert width={iconSizes.xLarge} className={componentIconClassName} />;
  }
};

const ComponentStatusModalContent = ({
  summary,
  componentType,
  health,
  errors,
  metrics,
  metadata,
  notice,
}: ComponentStatusModalProps) => {
  const hasErrors = errors.length > 0;
  const hasSingleError = errors.length === 1;

  const icon = useMemo(() => {
    const iconElement = renderComponentIcon(componentType);

    if (hasErrors || health === "fault") {
      return <StatusModalIcon intent="critical">{iconElement}</StatusModalIcon>;
    }

    return <StatusModalIcon intent={health === "degraded" ? "warning" : "info"}>{iconElement}</StatusModalIcon>;
  }, [componentType, hasErrors, health]);

  // For single error: use error message as title, timestamp as subtitle, skip error rows
  // For multiple errors or no errors: use summary as title, show error rows
  const title = hasSingleError ? errors[0].message : summary;
  const subtitle = hasSingleError ? formatReportedTimestamp(errors[0].timestamp) : undefined;

  // Transform errors into layout format (skip for single error case)
  const layoutErrors: StatusModalLayoutError[] = useMemo(
    () =>
      hasSingleError
        ? []
        : errors.map((error, index) => ({
            key: `error-${index}-${error.timestamp || index}`,
            icon: <Alert />,
            title: error.message,
            subtitle: formatReportedTimestamp(error.timestamp),
            onClick: error.onClick,
          })),
    [errors, hasSingleError],
  );

  return (
    <StatusModalLayout icon={icon} title={title} subtitle={subtitle} errors={layoutErrors}>
      {notice ? (
        <p role="status" className="text-300 text-text-primary-70">
          {notice}
        </p>
      ) : null}
      {/* Performance metrics grid */}
      {metrics && metrics.length > 0 ? (
        <div
          className={clsx("grid gap-x-4 gap-y-6", metrics.length % 3 === 0 ? "grid-cols-3" : "grid-cols-2")}
          data-testid="status-modal-metrics"
        >
          {metrics.map((metric, index) => (
            <LabeledValue key={`${metric.label}-${index}`} label={metric.label} value={metric.value} />
          ))}
        </div>
      ) : null}

      {/* Metadata section */}
      {metadata ? <ComponentMetadata metadata={metadata} /> : null}
    </StatusModalLayout>
  );
};

export default ComponentStatusModalContent;
