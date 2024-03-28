import { useState } from "react";
import clsx from "clsx";
import WarningModal from "./WarningModal";

interface WarningProps {
  label: "Fans" | "ASIC";
  messages: string[];
  state: "critical" | "warning";
}

const Warning = ({ label, messages, state }: WarningProps) => {
  const [showModal, setShowModal] = useState(false);

  const isWarning = state === "warning";
  const isCritical = state === "critical";

  return (
    <>
      <button
        className={clsx("text-heading-50 rounded flex items-center", {
          "bg-intent-critical-fill/10 text-intent-critical-text": isCritical,
          "bg-intent-warning-fill/10 text-intent-warning-text": isWarning,
        })}
        onClick={() => setShowModal(true)}
      >
        <div
          className={clsx("px-2 py-1 rounded-s", {
            "bg-intent-critical-fill/20": isCritical,
            "bg-intent-warning-fill/20": isWarning,
          })}
        >
          {label}
        </div>
        <div className="flex items-center px-2 py-1 space-x-1">
          <svg width="6" height="6" viewBox="0 0 6 6">
            <circle cx="3" cy="3" r="3" fill="currentColor" />
          </svg>
          <div>
            {messages.length === 1
              ? messages[0]
              : isWarning
                ? `${messages.length} warnings`
                : `${messages.length} errors`}
          </div>
        </div>
      </button>
      {showModal && (
        <WarningModal
          onDismiss={() => setShowModal(false)}
          type={label === "Fans" ? "fan" : "asic"}
        />
      )}
    </>
  );
};

export default Warning;
