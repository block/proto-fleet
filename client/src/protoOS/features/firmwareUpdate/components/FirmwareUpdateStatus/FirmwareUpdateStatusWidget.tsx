import { useMemo } from "react";
import clsx from "clsx";

import { UpdateStatus } from "@/protoOS/api/types";
import WidgetWrapper from "@/protoOS/components/PageHeader/WidgetWrapper";
import { statusLabelFromUpdateStatus } from "@/protoOS/features/firmwareUpdate/utility";
import { variants as buttonVariants } from "@/shared/components/Button";
import ProgressCircular from "@/shared/components/ProgressCircular";
import StatusCircle, {
  type StatusCircleProps,
  variants,
} from "@/shared/components/StatusCircle";
import { statuses } from "@/shared/components/StatusCircle/constants";

interface FirmwareUpdateStatusWidgetProps {
  updateStatus?: UpdateStatus;
  loading?: boolean;
  installing?: boolean;
  onClick: () => void;
}

const FirmwareUpdateStatusWidget = ({
  updateStatus,
  installing,
  loading = false,
  onClick,
}: FirmwareUpdateStatusWidgetProps) => {
  const firmwareStatusMessage = useMemo(() => {
    return statusLabelFromUpdateStatus(updateStatus);
  }, [updateStatus]);

  const status: StatusCircleProps["status"] = useMemo(() => {
    switch (updateStatus?.status) {
      case "error":
        return statuses.error;
      case "success":
        return statuses.normal;
      default:
        return statuses.pending;
    }
  }, [updateStatus?.status]);

  return (
    <WidgetWrapper
      onClick={loading ? undefined : onClick}
      className={clsx({
        "hover:cursor-progress": loading,
        hidden:
          !updateStatus ||
          updateStatus.status === "current" ||
          firmwareStatusMessage === undefined,
      })}
      variant={
        updateStatus?.status === "installed"
          ? buttonVariants.primary
          : undefined
      }
    >
      {installing ? (
        <div className="flex items-center gap-2 text-xs">
          <div className="flex items-center">
            <ProgressCircular
              indeterminate
              dataTestId="miner-status-spinner"
              size={12}
            />
          </div>
          {updateStatus?.progress && <>{updateStatus.progress}%</>}
        </div>
      ) : updateStatus?.status !== "installed" ? (
        <div className="flex items-center">
          <StatusCircle
            removeMargin={true}
            status={status}
            variant={variants.simple}
            width={"w-2"}
          />
        </div>
      ) : null}
      {firmwareStatusMessage}
    </WidgetWrapper>
  );
};

export default FirmwareUpdateStatusWidget;
