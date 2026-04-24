import { useCallback, useEffect, useState } from "react";

import WidgetWrapper from "../WidgetWrapper";
import PowerPopover from "./PowerPopover";
import { ErrorProps } from "@/protoOS/api/apiResponseTypes";
import { EnteringSleepDialog, RebootingDialog, WarnRebootDialog, WarnSleepDialog } from "@/protoOS/components/Power";
import { useAccessToken } from "@/protoOS/store";
import {
  AUTH_ACTIONS,
  useDismissedLoginModal,
  useIsAwake,
  useIsSleeping,
  usePausedAuthAction,
  useSetDismissedLoginModal,
  useSetPausedAuthAction,
} from "@/protoOS/store";
import { Power } from "@/shared/assets/icons";
import { iconSizes } from "@/shared/assets/icons/constants";

import { useResponsivePopover } from "@/shared/components/Popover";
import { useClickOutside } from "@/shared/hooks/useClickOutside";

interface PowerWidgetProps {
  afterReboot?: () => void;
  afterSleep?: () => void;
  afterWake?: () => void;
  onReboot: () => void;
  onSleep: () => void;
  onWake: () => void;
  rebootError?: ErrorProps;
  sleepError?: ErrorProps;
  shouldShowPopover?: boolean;
  wakeError?: ErrorProps;
}

const PowerWidget = ({
  afterReboot,
  afterSleep,
  onReboot,
  onSleep,
  onWake,
  rebootError,
  sleepError,
  shouldShowPopover,
}: PowerWidgetProps) => {
  const { triggerRef: WidgetRef } = useResponsivePopover();
  const isAwake = useIsAwake();
  const isSleeping = useIsSleeping();

  const [isOpen, setIsOpen] = useState(shouldShowPopover);
  const [warnReboot, setWarnReboot] = useState(false);
  const [shouldReboot, setShouldReboot] = useState(false);
  const [warnSleep, setWarnSleep] = useState(false);
  const [shouldSleep, setShouldSleep] = useState(false);
  const dismissedLoginModal = useDismissedLoginModal();
  const setDismissedLoginModal = useSetDismissedLoginModal();
  const pausedAuthAction = usePausedAuthAction();
  const setPausedAuthAction = useSetPausedAuthAction();

  const { checkAccess, hasAccess, setHasAccess } = useAccessToken(!!pausedAuthAction && !dismissedLoginModal);

  const onClickOutside = useCallback(() => {
    setIsOpen(false);
  }, []);

  useClickOutside({
    ref: WidgetRef,
    onClickOutside,
    ignoreSelectors: [".popover-content"],
  });

  // Open the confirmation dialog once auth is available for a paused action
  const [prevHasAccess, setPrevHasAccess] = useState(hasAccess);
  const [prevPausedAuthAction, setPrevPausedAuthAction] = useState(pausedAuthAction);
  if (prevHasAccess !== hasAccess || prevPausedAuthAction !== pausedAuthAction) {
    setPrevHasAccess(hasAccess);
    setPrevPausedAuthAction(pausedAuthAction);
    if (hasAccess && pausedAuthAction) {
      if (pausedAuthAction === AUTH_ACTIONS.reboot) {
        setWarnReboot(true);
      } else if (pausedAuthAction === AUTH_ACTIONS.sleep) {
        setWarnSleep(true);
      }
    }
  }

  // Clear the auth-store flag after the warn dialog has surfaced (external store
  // mutation must not happen in render; subscribers should only see updates post-commit)
  useEffect(() => {
    if (hasAccess && pausedAuthAction) {
      setPausedAuthAction(null);
    }
  }, [hasAccess, pausedAuthAction, setPausedAuthAction]);

  useEffect(() => {
    if (dismissedLoginModal) {
      setPausedAuthAction(null);
      setDismissedLoginModal(false);
    }
  }, [dismissedLoginModal, setDismissedLoginModal, setPausedAuthAction]);

  const handleRebootButton = () => {
    setIsOpen(false);
    setPausedAuthAction(AUTH_ACTIONS.reboot);
    checkAccess();
  };

  const handleRebootConfirm = () => {
    setWarnReboot(false);
    onReboot();
    setShouldReboot(true);
  };

  const handleSleepButton = () => {
    setIsOpen(false);
    setPausedAuthAction(AUTH_ACTIONS.sleep);
    checkAccess();
  };

  const handleSleepConfirm = () => {
    onSleep();
    setWarnSleep(false);
    setShouldSleep(true);
  };

  const handleWakeButton = () => {
    setIsOpen(false);
    onWake();
  };

  useEffect(() => {
    if (shouldReboot) {
      if (rebootError) {
        // eslint-disable-next-line react-hooks/set-state-in-effect -- clear pending reboot flag once the reboot request settles
        setShouldReboot(false);
        if (rebootError?.status === 401) {
          setHasAccess(false);
          setPausedAuthAction(AUTH_ACTIONS.reboot);
        }
        // TODO: handle other errors
      } else if (isAwake) {
        setShouldReboot(false);
        afterReboot?.();
      }
    }
  }, [shouldReboot, isAwake, afterReboot, rebootError, setHasAccess, setPausedAuthAction]);

  useEffect(() => {
    if (shouldSleep) {
      if (sleepError) {
        // eslint-disable-next-line react-hooks/set-state-in-effect -- clear pending sleep flag once the sleep request settles
        setShouldSleep(false);
        if (sleepError?.status === 401) {
          setHasAccess(false);
          setPausedAuthAction(AUTH_ACTIONS.sleep);
        }
        // TODO: handle other errors
      } else if (isSleeping) {
        setShouldSleep(false);
        afterSleep?.();
      }
    }
  }, [afterSleep, isSleeping, setHasAccess, shouldSleep, sleepError, setPausedAuthAction]);

  return (
    <div className="relative" ref={WidgetRef} data-testid="power-widget">
      <WidgetWrapper
        onClick={() => setIsOpen((prev) => !prev)}
        className="w-[28px] p-0 text-text-primary"
        isOpen={isOpen}
        testId="power-button"
      >
        <Power width={iconSizes.small} className="m-1 -translate-y-0.25" />
      </WidgetWrapper>
      {isOpen ? (
        <PowerPopover onReboot={handleRebootButton} onSleep={handleSleepButton} onWake={handleWakeButton} />
      ) : null}
      <WarnRebootDialog open={warnReboot} onClose={() => setWarnReboot(false)} onSubmit={handleRebootConfirm} />
      <RebootingDialog open={shouldReboot} />
      <WarnSleepDialog open={warnSleep} onClose={() => setWarnSleep(false)} onSubmit={handleSleepConfirm} />
      <EnteringSleepDialog open={shouldSleep} />
    </div>
  );
};

export default PowerWidget;
