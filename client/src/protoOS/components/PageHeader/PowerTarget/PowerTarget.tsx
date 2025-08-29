import { useCallback, useEffect, useMemo, useState } from "react";

import { DEFAULT_POWER_TARGET } from "./constants";
import PowerTargetPopover from "./PowerTargetPopover";
import { useMiningTarget } from "@/protoOS/api";
import WidgetWrapper from "@/protoOS/components/PageHeader/WidgetWrapper";
import { usePopover } from "@/shared/components/Popover";
import ProgressCircular from "@/shared/components/ProgressCircular";
import { useClickOutside } from "@/shared/hooks/useClickOutside";

const PowerTarget = () => {
  const { miningTarget, bounds, pending } = useMiningTarget();
  const [showPopover, setShowPopover] = useState<boolean>(false);
  const { triggerRef: widgetRef, setIsTriggerFixed } = usePopover();

  const isMax = useMemo(() => {
    return bounds?.max && miningTarget === bounds?.max * 1000;
  }, [miningTarget, bounds?.max]);

  const isMin = useMemo(() => {
    return bounds?.min && miningTarget === bounds?.min * 1000;
  }, [miningTarget, bounds?.min]);

  const chipText = useMemo(() => {
    if (pending || miningTarget === undefined) {
      return "Power target";
    }

    let targetType;
    if (isMax) {
      targetType = "max";
    } else if (isMin) {
      targetType = "min";
    } else if (miningTarget === DEFAULT_POWER_TARGET) {
      targetType = "default";
    } else {
      targetType = "custom";
    }

    return `${miningTarget / 1000} kW ${targetType} target`;
  }, [isMax, isMin, miningTarget, pending]);

  useEffect(() => {
    setIsTriggerFixed(true);
  }, [setIsTriggerFixed]);

  const onClickOutside = useCallback(() => {
    setShowPopover(false);
  }, []);

  useClickOutside({ ref: widgetRef, onClickOutside });

  return (
    <div ref={widgetRef} className="relative">
      <WidgetWrapper
        onClick={() => {
          setShowPopover(true);
        }}
      >
        <>
          {pending && (
            <ProgressCircular
              className="mr-1"
              indeterminate
              dataTestId="mining-pool-spinner"
              size={14}
            />
          )}
          {chipText}
        </>
      </WidgetWrapper>
      {showPopover && (
        <PowerTargetPopover onDismiss={() => setShowPopover(false)} />
      )}
    </div>
  );
};

export default PowerTarget;
