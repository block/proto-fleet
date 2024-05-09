import { useState } from "react";
import clsx from "clsx";

import { useWindowDimensions } from "common/hooks/useWindowDimensions";

import { AlertCompact } from "icons";

import WidgetWrapper from "../WidgetWrapper";
import WarningModal from "./WarningModal";

interface WarningProps {
  errorCount: number;
  errorType: "fan" | "asic";
  state: "critical" | "warning";
}

const Warning = ({ errorCount, errorType, state }: WarningProps) => {
  const { isPhone } = useWindowDimensions();
  const [showModal, setShowModal] = useState(false);

  const isWarning = state === "warning";
  const isCritical = state === "critical";

  const plural = errorCount > 1 ? "s" : "";
  const severity = isWarning ? `warning${plural}` : `error${plural}`;
  const label = errorType === "fan" ? "fan" : "ASIC";

  return (
    <>
      <WidgetWrapper
        className={clsx({
          "text-text-critical": isCritical,
          "text-text-warning": isWarning,
        })}
        onClick={() => setShowModal(true)}
      >
        <AlertCompact className="mr-1" />
        {isPhone ? label : `${errorCount} ${label} ${severity}`}
      </WidgetWrapper>
      {showModal && (
        <WarningModal onDismiss={() => setShowModal(false)} type={errorType} />
      )}
    </>
  );
};

export default Warning;
