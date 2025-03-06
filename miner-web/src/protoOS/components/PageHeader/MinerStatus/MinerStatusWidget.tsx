import { useMemo } from "react";
import clsx from "clsx";

import WidgetWrapper from "../WidgetWrapper";
import { ErrorListResponse } from "@/protoOS/api/types";
import {
  isAsicError,
  isAsicWarning,
  isFanError,
  isFanWarning,
  isHashboardError,
  isHashboardWarning,
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
  const hashboardStatus = useMemo<StatusCircleProps["status"]>(() => {
    if (errors.some(isHashboardError)) {
      return "error";
    } else if (errors.some(isHashboardWarning)) {
      return "warning";
    }

    return "normal";
  }, [errors]);

  const asicStatus = useMemo<StatusCircleProps["status"]>(() => {
    if (errors.some(isAsicError)) {
      return "error";
    } else if (errors.some(isAsicWarning)) {
      return "warning";
    }

    return "normal";
  }, [errors]);

  const fanStatus = useMemo<StatusCircleProps["status"]>(() => {
    if (errors.some(isFanError)) {
      return "error";
    } else if (errors.some(isFanWarning)) {
      return "warning";
    }

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
            <StatusCircle status={hashboardStatus} />
            <StatusCircle status={asicStatus} />
            <StatusCircle status={fanStatus} />
          </>
        )}
        Miner status
      </>
    </WidgetWrapper>
  );
};

export default MinerStatusWidget;
