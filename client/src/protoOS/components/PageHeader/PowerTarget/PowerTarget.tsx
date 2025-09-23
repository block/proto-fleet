import { useCallback, useEffect, useMemo, useState } from "react";

import PowerTargetPopover from "./PowerTargetPopover";
import { useMiningTarget } from "@/protoOS/api";
import WidgetWrapper from "@/protoOS/components/PageHeader/WidgetWrapper";
import { usePopover } from "@/shared/components/Popover";
import ProgressCircular from "@/shared/components/ProgressCircular";
import { useClickOutside } from "@/shared/hooks/useClickOutside";

const PowerTarget = () => {
  const { miningTarget, defaultTarget, bounds, pending } = useMiningTarget();
  const [showPopover, setShowPopover] = useState<boolean>(false);
  const { triggerRef: widgetRef, setIsTriggerFixed } = usePopover();

  const isMax = useMemo(() => {
    return bounds?.max && miningTarget === bounds?.max;
  }, [miningTarget, bounds?.max]);

  const isMin = useMemo(() => {
    return bounds?.min && miningTarget === bounds?.min;
  }, [miningTarget, bounds?.min]);

  const chipText = useMemo(() => {
    if (pending || miningTarget === undefined) {
      return "Power target";
    }

    let targetType;
    if (isMax) {
      targetType = "Max";
    } else if (isMin) {
      targetType = "Min";
    } else if (miningTarget === defaultTarget) {
      targetType = "Default";
    } else {
      targetType = `${miningTarget / 1000} kW`;
    }

    return `${targetType} power target`;
  }, [isMax, isMin, miningTarget, pending, defaultTarget]);

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
        <div className="flex items-center">
          {pending && (
            <ProgressCircular
              className="mr-1"
              indeterminate
              dataTestId="mining-pool-spinner"
              size={12}
            />
          )}
          {chipText}
        </div>
      </WidgetWrapper>
      {showPopover && (
        <PowerTargetPopover onDismiss={() => setShowPopover(false)} />
      )}
    </div>
  );
};

export default PowerTarget;
