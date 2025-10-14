import type { ComponentError, ComponentType } from "./types";
import {
  ControlBoard,
  Fan,
  Hashboard,
  LightningAlt,
} from "@/shared/assets/icons";
import { iconSizes } from "@/shared/assets/icons/constants";
import Row from "@/shared/components/Row";

interface ComponentErrorRowProps {
  error: ComponentError;
}

const getComponentIcon = (type: ComponentType) => {
  switch (type) {
    case "fan":
      return <Fan width={iconSizes.medium} />;
    case "hashboard":
      return <Hashboard width={iconSizes.medium} />;
    case "psu":
      return <LightningAlt width={iconSizes.medium} />;
    case "controlBoard":
      return <ControlBoard width={iconSizes.medium} />;
  }
};

const ComponentErrorRow = ({ error }: ComponentErrorRowProps) => {
  const formatTimestamp = (timestamp?: number) => {
    if (!timestamp) return "";
    const date = new Date(timestamp * 1000);
    return `on ${date.toLocaleDateString(undefined, {
      month: "numeric",
      day: "numeric",
      year: "2-digit",
    })} at ${date.toLocaleTimeString(undefined, {
      hour: "numeric",
      minute: "2-digit",
      hour12: true,
    })}`;
  };

  return (
    <Row
      prefixIcon={
        <div className="flex h-8 w-8 items-center justify-center rounded-lg bg-core-primary-5">
          {getComponentIcon(error.componentType)}
        </div>
      }
      divider={false}
    >
      <div className="text-emphasis-300 text-text-primary">
        {error.componentName}
      </div>
      <div className="text-200 text-text-primary-70">
        {error.message} {formatTimestamp(error.timestamp)}
      </div>
    </Row>
  );
};

export default ComponentErrorRow;
