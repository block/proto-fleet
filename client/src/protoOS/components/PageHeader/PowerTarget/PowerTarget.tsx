import { useCallback, useEffect, useMemo, useState } from "react";

import PowerTargetPopover from "./PowerTargetPopover";
import { MiningTarget } from "@/protoOS/api/generatedApi";
import { useMiningTarget } from "@/protoOS/api/hooks/useMiningTarget";
import WidgetWrapper from "@/protoOS/components/PageHeader/WidgetWrapper";
import { useAccessToken } from "@/protoOS/store";
import {
  AUTH_ACTIONS,
  useDismissedLoginModal,
  usePausedAuthAction,
  useSetDismissedLoginModal,
  useSetPausedAuthAction,
} from "@/protoOS/store";
import { useResponsivePopover } from "@/shared/components/Popover";
import ProgressCircular from "@/shared/components/ProgressCircular";
import { useClickOutside } from "@/shared/hooks/useClickOutside";

const PowerTarget = () => {
  const { miningTarget, defaultTarget, bounds, pending, updateMiningTarget, setPending } = useMiningTarget();
  const [showPopover, setShowPopover] = useState<boolean>(false);
  const { triggerRef: widgetRef } = useResponsivePopover();
  const dismissedLoginModal = useDismissedLoginModal();
  const setDismissedLoginModal = useSetDismissedLoginModal();
  const pausedAuthAction = usePausedAuthAction();
  const setPausedAuthAction = useSetPausedAuthAction();
  const [lastMiningTarget, setLastMiningTarget] = useState<MiningTarget | null>(null);

  const { hasAccess, checkAccess } = useAccessToken(!!pausedAuthAction && !dismissedLoginModal);

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
    let targetValue = `${miningTarget / 1000} kW`;
    if (isMax) {
      targetType = `${targetValue} max target`;
    } else if (isMin) {
      targetType = `${targetValue} min target`;
    } else if (miningTarget === defaultTarget) {
      targetType = `${targetValue} default target`;
    } else {
      targetType = `${targetValue} custom target`;
    }

    return targetType;
  }, [isMax, isMin, miningTarget, pending, defaultTarget]);

  const handleUpdateStart = useCallback(
    (miningTarget: MiningTarget) => {
      setLastMiningTarget(miningTarget);
      setPausedAuthAction(AUTH_ACTIONS.miningTarget);
      checkAccess();
    },
    [setPausedAuthAction, checkAccess],
  );

  useEffect(() => {
    if (hasAccess && pausedAuthAction === AUTH_ACTIONS.miningTarget && lastMiningTarget) {
      updateMiningTarget(lastMiningTarget);
      setPausedAuthAction(null);
      // eslint-disable-next-line react-hooks/set-state-in-effect -- resume paused mining-target update once auth is available
      setLastMiningTarget(null);
    }
  }, [hasAccess, pausedAuthAction, setPausedAuthAction, updateMiningTarget, lastMiningTarget]);

  // Abandon paused mining-target update when user dismisses login modal.
  // Local state clears in render-phase; external store writes happen in the effect below
  // (mutating a shared store during render can notify other subscribers mid-reconciliation).
  const [prevDismissedLoginModal, setPrevDismissedLoginModal] = useState(dismissedLoginModal);
  if (prevDismissedLoginModal !== dismissedLoginModal) {
    setPrevDismissedLoginModal(dismissedLoginModal);
    if (dismissedLoginModal) {
      setLastMiningTarget(null);
    }
  }

  useEffect(() => {
    if (dismissedLoginModal) {
      setPending(false);
      setPausedAuthAction(null);
      setDismissedLoginModal(false);
    }
  }, [dismissedLoginModal, setPending, setPausedAuthAction, setDismissedLoginModal]);

  useEffect(() => {
    return () => {
      setLastMiningTarget(null);
    };
  }, []);

  const onClickOutside = useCallback(() => {
    setShowPopover(false);
  }, []);

  useClickOutside({
    ref: widgetRef,
    onClickOutside,
    ignoreSelectors: [".popover-content"],
  });

  return (
    <div ref={widgetRef} className="relative">
      <WidgetWrapper
        onClick={() => {
          setShowPopover(true);
        }}
      >
        <div className="flex items-center">
          {pending ? (
            <ProgressCircular className="mr-1" indeterminate dataTestId="mining-pool-spinner" size={12} />
          ) : null}
          {chipText}
        </div>
      </WidgetWrapper>
      {showPopover ? (
        <PowerTargetPopover onDismiss={() => setShowPopover(false)} onUpdateStart={handleUpdateStart} />
      ) : null}
    </div>
  );
};

export default PowerTarget;
