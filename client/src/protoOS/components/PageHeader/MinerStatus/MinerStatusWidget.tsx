import { useMemo } from "react";
import clsx from "clsx";

import WidgetWrapper from "../WidgetWrapper";
import { ErrorListResponse, NotificationError } from "@/protoOS/api/types";
import {
  isAsicError,
  isAsicWarning,
  isControlBoardError,
  isControlBoardWarning,
  isFanError,
  isFanWarning,
  isHashboardError,
  isHashboardWarning,
  isPSUError,
  isPSUWarning,
} from "@/protoOS/components/MinerStatusModal/utility";
import ProgressCircular from "@/shared/components/ProgressCircular";
import StatusCircle, {
  type StatusCircleProps,
} from "@/shared/components/StatusCircle/";
import { createOrPredicate } from "@/shared/utils/predicate";

interface MinerStatusWidgetProps {
  errors?: ErrorListResponse;
  loading?: boolean;
  onClick: () => void;
}

const MinerStatusWidget = ({
  errors = [],
  loading = false,
  onClick,
}: MinerStatusWidgetProps) => {
  const status = useMemo<StatusCircleProps["status"]>(() => {
    if (
      errors.some(
        createOrPredicate<NotificationError>(
          isFanError,
          isControlBoardError,
          isHashboardError,
          isAsicError,
          isPSUError,
        ),
      )
    )
      return "error";
    if (
      errors.some(
        createOrPredicate<NotificationError>(
          isFanWarning,
          isControlBoardWarning,
          isHashboardWarning,
          isAsicWarning,
          isPSUWarning,
        ),
      )
    )
      return "warning";
    return "normal";
  }, [errors]);

  return (
    <WidgetWrapper
      onClick={loading ? undefined : onClick}
      className={clsx("text-text-primary", {
        "hover:cursor-progress": loading,
      })}
    >
      <>
        {loading ? (
          [...Array(3)].map((_, index) => (
            <ProgressCircular
              className="mr-1"
              indeterminate
              dataTestId="miner-status-spinner"
              size={14}
              key={index}
            />
          ))
        ) : (
          <>
            <StatusCircle status={status} />
          </>
        )}
        Status
      </>
    </WidgetWrapper>
  );
};

export default MinerStatusWidget;
