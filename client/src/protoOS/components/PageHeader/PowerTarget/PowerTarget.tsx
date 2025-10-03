import { useCallback, useEffect, useMemo, useState } from "react";

import PowerTargetPopover from "./PowerTargetPopover";
import { useMiningTarget } from "@/protoOS/api";
import { MiningTarget } from "@/protoOS/api/generatedApi";
import WidgetWrapper from "@/protoOS/components/PageHeader/WidgetWrapper";
import {
  AUTH_ACTIONS,
  useAccessToken,
  useAuthContext,
} from "@/protoOS/features/auth/contexts/AuthContext";
import { usePopover } from "@/shared/components/Popover";
import ProgressCircular from "@/shared/components/ProgressCircular";
import { useClickOutside } from "@/shared/hooks/useClickOutside";

const PowerTarget = () => {
  const {
    miningTarget,
    defaultTarget,
    bounds,
    pending,
    updateMiningTarget,
    setPending,
  } = useMiningTarget();
  const [showPopover, setShowPopover] = useState<boolean>(false);
  const { triggerRef: widgetRef, setIsTriggerFixed } = usePopover();
  const {
    dismissedLoginModal,
    setDismissedLoginModal,
    pausedAuthAction,
    setPausedAuthAction,
  } = useAuthContext();
  const [lastMiningTarget, setLastMiningTarget] = useState<MiningTarget | null>(
    null,
  );

  const { hasAccess } = useAccessToken(
    !!pausedAuthAction && !dismissedLoginModal,
  );

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

  useEffect(() => {
    if (
      hasAccess &&
      pausedAuthAction === AUTH_ACTIONS.miningTarget &&
      lastMiningTarget
    ) {
      updateMiningTarget(lastMiningTarget);
      setPausedAuthAction(null);
      setLastMiningTarget(null);
    }
  }, [
    hasAccess,
    pausedAuthAction,
    setPausedAuthAction,
    updateMiningTarget,
    lastMiningTarget,
  ]);

  useEffect(() => {
    if (dismissedLoginModal) {
      setPending(false);
      setPausedAuthAction(null);
      setDismissedLoginModal(false);
      setLastMiningTarget(null);
    }
  }, [
    dismissedLoginModal,
    setDismissedLoginModal,
    setPausedAuthAction,
    setPending,
  ]);

  useEffect(() => {
    return () => {
      setLastMiningTarget(null);
    };
  }, []);

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
        <PowerTargetPopover
          onDismiss={() => setShowPopover(false)}
          onUpdateStart={(miningTarget) => setLastMiningTarget(miningTarget)}
        />
      )}
    </div>
  );
};

export default PowerTarget;
