import { ReactNode } from "react";
import clsx from "clsx";
import ComponentErrorRow from "./ComponentErrorRow";
import ComponentMetadata from "./ComponentMetadata";
import type { ComponentStatusModalProps, ComponentType } from "./types";
import {
  ArrowRight,
  ControlBoard,
  Fan,
  Hashboard,
  LightningAlt,
} from "@/shared/assets/icons";
import { iconSizes } from "@/shared/assets/icons/constants";
import { variants } from "@/shared/components/Button";
import Header from "@/shared/components/Header";
import Modal from "@/shared/components/Modal";

const getComponentTitle = (type: ComponentType): string => {
  switch (type) {
    case "fan":
      return "Fan status";
    case "hashboard":
      return "Hashboard status";
    case "psu":
      return "PSU status";
    case "controlBoard":
      return "Control board status";
  }
};

const LabeledValue = ({
  label,
  value,
}: {
  label: string;
  value: ReactNode;
}) => (
  <div className="flex flex-col">
    <div className="text-heading-200 text-text-primary">{value}</div>
    <div className="text-300 text-text-primary-50">{label}</div>
  </div>
);

const ComponentStatusModal = ({
  summary,
  componentType,
  issues,
  metrics,
  metadata,
  onDismiss,
  navigateBack,
}: ComponentStatusModalProps) => {
  const buttons = [
    {
      text: "Done",
      variant: variants.primary,
      onClick: onDismiss,
    },
  ];

  const hasErrors = issues.length > 0;
  const iconBackgroundClass = hasErrors
    ? "bg-intent-critical-fill"
    : "bg-core-primary-5";
  const iconColorClass = hasErrors ? "text-white" : "text-core-primary";

  const getIcon = () => {
    switch (componentType) {
      case "fan":
        return <Fan width={iconSizes.medium} className={iconColorClass} />;
      case "hashboard":
        return (
          <Hashboard width={iconSizes.medium} className={iconColorClass} />
        );
      case "psu":
        return (
          <LightningAlt width={iconSizes.medium} className={iconColorClass} />
        );
      case "controlBoard":
        return (
          <ControlBoard width={iconSizes.medium} className={iconColorClass} />
        );
    }
  };

  return (
    <Modal
      buttons={buttons}
      title={getComponentTitle(componentType)}
      onDismiss={onDismiss}
      icon={navigateBack ? <ArrowRight className="rotate-180" /> : undefined}
      onIconClick={navigateBack}
    >
      <div className="flex flex-col gap-y-5 pt-5">
        {/* Header with icon and summary */}
        <Header
          icon={
            <div
              className={`flex h-8 w-8 items-center justify-center rounded-lg ${iconBackgroundClass}`}
            >
              {getIcon()}
            </div>
          }
          title={summary}
          titleSize="text-heading-200"
        />

        {/* Error list */}
        {issues.length > 0 && (
          <div className="-mx-6 px-6">
            <div className="flex flex-col">
              {issues.map((error, index) => (
                <ComponentErrorRow
                  key={error.id}
                  error={error}
                  divider={index !== issues.length - 1}
                />
              ))}
            </div>
          </div>
        )}

        {/* Performance metrics grid */}
        {metrics && metrics.length > 0 && (
          <div
            className={clsx(
              "grid gap-x-4 gap-y-6",
              metrics.length % 3 === 0 ? "grid-cols-3" : "grid-cols-2",
            )}
          >
            {metrics.map((metric, index) => (
              <LabeledValue
                key={`${metric.label}-${index}`}
                label={metric.label}
                value={metric.value}
              />
            ))}
          </div>
        )}

        {/* Metadata section */}
        {metadata && <ComponentMetadata metadata={metadata} />}
      </div>
    </Modal>
  );
};

export default ComponentStatusModal;
