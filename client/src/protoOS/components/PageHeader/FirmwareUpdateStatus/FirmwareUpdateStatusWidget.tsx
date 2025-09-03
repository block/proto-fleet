import { useMemo } from "react";
import clsx from "clsx";

import { UpdateStatus } from "@/protoOS/api/types";
import WidgetWrapper from "@/protoOS/components/PageHeader/WidgetWrapper";
import { variants as buttonVariants } from "@/shared/components/Button";
import ProgressCircular from "@/shared/components/ProgressCircular";
import StatusCircle, { variants } from "@/shared/components/StatusCircle";
import { statuses } from "@/shared/components/StatusCircle/constants";
import { statusLabelFromUpdateStatus } from "@/shared/utils/utility";

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
    return statusLabelFromUpdateStatus(updateStatus?.status);
  }, [updateStatus]);

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
            status={statuses.pending}
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
