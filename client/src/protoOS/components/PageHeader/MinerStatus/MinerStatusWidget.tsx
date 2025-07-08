import { useMemo } from "react";
import clsx from "clsx";

import WidgetWrapper from "../WidgetWrapper";
import {
  ErrorListResponse,
  MiningStatusMiningstatus,
  NotificationError,
} from "@/protoOS/api/types";
import { isSleeping } from "@/protoOS/components/App/utility";
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
import { statuses } from "@/shared/components/StatusCircle/constants";
import { createOrPredicate } from "@/shared/utils/predicate";

interface MinerStatusWidgetProps {
  errors?: ErrorListResponse;
  miningStatus?: MiningStatusMiningstatus;
  loading?: boolean;
  onClick: () => void;
}

const MinerStatusWidget = ({
  errors = [],
  miningStatus,
  loading = false,
  onClick,
}: MinerStatusWidgetProps) => {
  const status = useMemo<StatusCircleProps["status"]>(() => {
    if (isSleeping(miningStatus?.status)) {
      return statuses.sleeping;
    }
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
      return statuses.error;
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
      return statuses.warning;
    return statuses.normal;
  }, [errors, miningStatus]);

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
