import { useMemo } from "react";
import clsx from "clsx";

import type { FirmwareUpdateInfo } from "@/protoOS/store/contract";
import WidgetWrapper from "@/protoOS/components/PageHeader/WidgetWrapper";
import { variants as buttonVariants } from "@/shared/components/Button";
import ProgressCircular from "@/shared/components/ProgressCircular";
import StatusCircle, { type StatusCircleProps, variants } from "@/shared/components/StatusCircle";
import { statuses } from "@/shared/components/StatusCircle/constants";

interface FirmwareUpdateStatusWidgetProps {
  updateStatus?: FirmwareUpdateInfo;
  loading?: boolean;
  installing?: boolean;
  statusMessage?: string;
  onClick: () => void;
}

const FirmwareUpdateStatusWidget = ({
  updateStatus,
  installing,
  statusMessage,
  loading = false,
  onClick,
}: FirmwareUpdateStatusWidgetProps) => {
  const status: StatusCircleProps["status"] = useMemo(() => {
    switch (updateStatus?.status) {
      case "failed":
        return statuses.error;
      case "complete":
        return statuses.normal;
      default:
        return statuses.pending;
    }
  }, [updateStatus?.status]);

  return (
    <WidgetWrapper
      onClick={loading ? undefined : onClick}
      testId="firmware-status-widget"
      className={clsx({
        "hover:cursor-progress": loading,
      })}
      variant={updateStatus?.status === "rebootRequired" ? buttonVariants.primary : undefined}
    >
      {installing ? (
        <div className="flex items-center gap-2 text-xs">
          <div className="flex items-center">
            <ProgressCircular indeterminate dataTestId="miner-status-spinner" size={12} />
          </div>
          {updateStatus?.progressPercent != null ? <>{updateStatus.progressPercent}%</> : null}
        </div>
      ) : updateStatus?.status !== "rebootRequired" ? (
        <div className="flex items-center">
          <StatusCircle removeMargin={true} status={status} variant={variants.simple} width={"w-2"} />
        </div>
      ) : null}
      {statusMessage}
    </WidgetWrapper>
  );
};

export default FirmwareUpdateStatusWidget;
