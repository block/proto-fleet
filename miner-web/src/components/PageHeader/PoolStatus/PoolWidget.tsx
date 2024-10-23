import { useMemo } from "react";
import clsx from "clsx";

import Spinner from "components/Spinner";

import { ConcentricCircles } from "icons";

import WidgetWrapper from "../WidgetWrapper";

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
    [isConnected, loading]
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
          <Spinner
            className="mr-1"
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
        Mining pool
      </>
    </WidgetWrapper>
  );
};

export default PoolWidget;
