import { useCallback, useEffect, useMemo, useState } from "react";
import clsx from "clsx";

import WidgetWrapper from "../WidgetWrapper";
import PowerTargetPopover from "./PowerTargetPopover";
import { useMiningTarget } from "@/protoOS/api";
import { Lightning } from "@/shared/assets/icons";
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

  const chipText = useMemo(() => {
    if (pending || miningTarget === undefined) {
      return "Power target";
    }

    if (isMax) {
      return "Max power target";
    }

    return `${miningTarget / 1000} kW fixed target`;
  }, [isMax, miningTarget, pending]);

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
        contrast={!isMax}
        onClick={() => {
          setShowPopover(true);
        }}
      >
        <>
          {pending ? (
            <ProgressCircular
              className="mr-1"
              indeterminate
              dataTestId="mining-pool-spinner"
              size={14}
            />
          ) : (
            <Lightning
              className={clsx("mr-1", {
                "text-text-contrast-70": !isMax,
                "text-text-primary-50": isMax,
              })}
              width="w-3"
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
