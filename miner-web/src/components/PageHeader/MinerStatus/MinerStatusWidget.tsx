import { useMemo } from "react";
import clsx from "clsx";

import { ErrorListResponse } from "apiTypes";

import StatusCircle from "components/MinerStatusModal/StatusCircle";
import {
  isAsicError,
  isAsicWarning,
  isFanError,
  isFanWarning,
  isHashboardError,
  isHashboardWarning,
} from "components/MinerStatusModal/utility";
import Spinner from "components/Spinner";

import WidgetWrapper from "../WidgetWrapper";

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
  const showHashboardError = useMemo(
    () => errors.some(isHashboardError),
    [errors]
  );
  // if there are errors, we don't need to check for warnings
  const showHashboardWarning = useMemo(
    () => !showHashboardError && errors.some(isHashboardWarning),
    [errors, showHashboardError]
  );

  const showAsicError = useMemo(() => errors.some(isAsicError), [errors]);
  // if there are errors, we don't need to check for warnings
  const showAsicWarning = useMemo(
    () => !showAsicError && errors.some(isAsicWarning),
    [errors, showAsicError]
  );

  const showFanError = useMemo(() => errors.some(isFanError), [errors]);
  // if there are errors, we don't need to check for warnings
  const showFanWarning = useMemo(
    () => !showFanError && errors.some(isFanWarning),
    [errors, showFanError]
  );

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
            <StatusCircle
              isError={showHashboardError}
              isWarning={showHashboardWarning}
            />
            <StatusCircle isError={showAsicError} isWarning={showAsicWarning} />
            <StatusCircle isError={showFanError} isWarning={showFanWarning} />
          </>
        )}
        Miner status
      </>
    </WidgetWrapper>
  );
};

export default MinerStatusWidget;
