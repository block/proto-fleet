import { useMemo } from "react";
import clsx from "clsx";

import WidgetWrapper from "../WidgetWrapper";

import ProgressCircular from "@/shared/components/ProgressCircular";
import StatusCircle, {
  statuses,
  variants,
} from "@/shared/components/StatusCircle";

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
      <div className="flex flex-col justify-center">
        {loading ? (
          <ProgressCircular
            indeterminate
            dataTestId="mining-pool-spinner"
            size={12}
          />
        ) : isDisconnected ? (
          <StatusCircle
            status={statuses.error}
            variant={variants.simple}
            width="w-2"
          />
        ) : (
          <StatusCircle
            status={statuses.normal}
            variant={variants.simple}
            width="w-2"
          />
        )}
      </div>
      Mining Pool
    </WidgetWrapper>
  );
};

export default PoolWidget;
