import { useMemo } from "react";
import clsx from "clsx";

import WidgetWrapper from "../WidgetWrapper";
import { ConcentricCircles } from "@/shared/assets/icons";

import ProgressCircular from "@/shared/components/ProgressCircular";

interface PoolWidgetProps {
  loading: boolean;
  isConnected: boolean;
  isOpen: boolean;
  onTogglePopover: () => void;
}

const PoolWidget = ({
  loading,
  isConnected,
  isOpen,
  onTogglePopover,
}: PoolWidgetProps) => {
  const isDisconnected = useMemo(
    () => !isConnected && !loading,
    [isConnected, loading],
  );

  return (
    <WidgetWrapper
      onClick={loading ? undefined : onTogglePopover}
      className={clsx("text-text-primary", {
        "hover:cursor-progress": loading,
      })}
      isOpen={isOpen}
    >
      <>
        {loading ? (
          <ProgressCircular
            className="mr-1"
            indeterminate
            dataTestId="mining-pool-spinner"
            size={14}
          />
        ) : (
          <ConcentricCircles
            className={clsx("mr-1", {
              "text-intent-success-fill": isConnected || loading,
              "text-intent-critical-fill": isDisconnected,
            })}
          />
        )}
        Pool
      </>
    </WidgetWrapper>
  );
};

export default PoolWidget;
