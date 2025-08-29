import { useMemo } from "react";
import clsx from "clsx";

import { UpdateStatus } from "@/protoOS/api/types";
import WidgetWrapper from "@/protoOS/components/PageHeader/WidgetWrapper";
import ProgressCircular from "@/shared/components/ProgressCircular";
import StatusCircle, {
  type StatusCircleProps,
  variants as variants,
} from "@/shared/components/StatusCircle";
import { statuses } from "@/shared/components/StatusCircle/constants";
import { statusLabelFromUpdateStatus } from "@/shared/utils/utility";

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
    return statusLabelFromUpdateStatus(updateStatus?.status);
  }, [updateStatus]);

  const isInProgress =
    updateStatus?.status === "downloading" ||
    updateStatus?.status === "installing";

  return (
    <WidgetWrapper
      onClick={loading ? undefined : onClick}
      className={clsx("text-text-primary", {
        "hover:cursor-progress": loading,
        hidden:
          !updateStatus ||
          updateStatus.status === "current" ||
          firmwareStatusMessage === undefined,
      })}
    >
      {isInProgress && updateStatus?.progress !== undefined ? (
        <div className="flex items-center gap-2 text-xs">
          <div className="flex items-center">
            <ProgressCircular
              indeterminate
              dataTestId="miner-status-spinner"
              size={12}
            />
          </div>
          {updateStatus.progress}%
        </div>
      ) : (
        <div className="flex items-center">
          <StatusCircle
            status={status}
            variant={variants.simple}
            width={"w-2"}
          />
        </div>
      )}
      {firmwareStatusMessage}
    </WidgetWrapper>
  );
};

export default FirmwareUpdateStatusWidget;
