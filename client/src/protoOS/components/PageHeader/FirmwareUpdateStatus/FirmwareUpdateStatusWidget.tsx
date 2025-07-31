import { useMemo } from "react";
import clsx from "clsx";

import { UpdateStatus } from "@/protoOS/api/types";
import WidgetWrapper from "@/protoOS/components/PageHeader/WidgetWrapper";
import StatusCircle, {
  type StatusCircleProps,
} from "@/shared/components/StatusCircle";
import { statuses } from "@/shared/components/StatusCircle/constants";

interface FirmwareUpdateStatusWidgetProps {
  updateStatus?: UpdateStatus;
  loading?: boolean;
  onClick: () => void;
}

const FirmwareUpdateStatusWidget = ({
  updateStatus,
  loading = false,
  onClick,
}: FirmwareUpdateStatusWidgetProps) => {
  const status = useMemo<StatusCircleProps["status"]>(() => {
    if (!updateStatus?.status) {
      return statuses.normal;
    }

    switch (updateStatus.status) {
      case "error":
        return statuses.error;
      case "downloading":
      case "installing":
      case "checking":
        return statuses.pending;
      case "available":
        return statuses.pending;
      case "current":
      case "success":
      case "installed":
        return statuses.normal;
      case "downloaded":
      case "confirming":
        return statuses.warning;
      default:
        return statuses.error;
    }
  }, [updateStatus]);

  const firmwareStatusMessage = useMemo(() => {
    if (!updateStatus) return "Firmware up to date";
    switch (updateStatus.status) {
      case "available":
        return "Update available";
      case "downloading":
        return "Downloading";
      case "downloaded":
        return "Ready to install";
      case "installing":
        return "Installing";
      case "installed":
        return "Reboot required";
      case "success":
        return "Update complete";
      case "current":
        return "Firmware up to date";
      case "checking":
        return "Checking";
      case "confirming":
        return "Confirming";
      case "error":
        return "Update failed";
      default:
        return "Firmware status unknown";
    }
  }, [updateStatus]);

  const isInProgress =
    updateStatus?.status === "downloading" ||
    updateStatus?.status === "installing";

  return (
    <WidgetWrapper
      onClick={loading ? undefined : onClick}
      className={clsx("text-text-primary", {
        "hover:cursor-progress": loading,
        hidden: updateStatus?.status === "current",
      })}
    >
      <StatusCircle status={status} />
      {isInProgress && updateStatus?.progress !== undefined && (
        <span className="text-xs">{updateStatus.progress}% </span>
      )}
      {firmwareStatusMessage}
    </WidgetWrapper>
  );
};

export default FirmwareUpdateStatusWidget;
