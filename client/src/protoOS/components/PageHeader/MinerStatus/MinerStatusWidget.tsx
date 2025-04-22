import { useMemo } from "react";
import clsx from "clsx";

import WidgetWrapper from "../WidgetWrapper";
import { ErrorListResponse } from "@/protoOS/api/types";
import {
  isControlBoardError,
  isControlBoardWarning,
  isFanError,
  isFanWarning,
  isHashboardError,
  isHashboardWarning,
  isPSUError,
  isPSUWarning,
} from "@/protoOS/components/MinerStatusModal/utility";
import Spinner from "@/shared/components/Spinner";
import StatusCircle, {
  type StatusCircleProps,
} from "@/shared/components/StatusCircle/";

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
        isFanError || isControlBoardError || isHashboardError || isPSUError,
      )
    )
      return "error";
    if (
      errors.some(
        isFanWarning ||
          isControlBoardWarning ||
          isHashboardWarning ||
          isPSUWarning,
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
            <Spinner
              className="mr-1"
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
        Miner status
      </>
    </WidgetWrapper>
  );
};

export default MinerStatusWidget;
