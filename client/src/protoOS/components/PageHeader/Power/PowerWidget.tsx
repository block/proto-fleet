import { useCallback, useEffect, useState } from "react";

import WidgetWrapper from "../WidgetWrapper";
import { actions } from "./constants";
import PowerPopover from "./PowerPopover";
import { ErrorProps } from "@/protoOS/api/apiResponseTypes";
import { MiningStatusMiningstatus } from "@/protoOS/api/types";

import {
  isAwake,
  isSleeping,
  isWarmingUp,
} from "@/protoOS/components/App/utility";
import {
  EnteringSleepDialog,
  RebootingDialog,
  WarnRebootDialog,
  WarnSleepDialog,
} from "@/protoOS/components/Power";
import {
  useAccessToken,
  useAuthContext,
} from "@/protoOS/features/auth/contexts/AuthContext";
import { Power } from "@/shared/assets/icons";
import { iconSizes } from "@/shared/assets/icons/constants";

import { usePopover } from "@/shared/components/Popover";
import { useClickOutside } from "@/shared/hooks/useClickOutside";

interface PowerWidgetProps {
  afterReboot?: () => void;
  afterSleep?: () => void;
  afterWake?: () => void;
  miningStatus: MiningStatusMiningstatus;
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
  miningStatus,
  onReboot,
  onSleep,
  onWake,
  rebootError,
  sleepError,
  shouldShowPopover,
}: PowerWidgetProps) => {
  const { triggerRef: WidgetRef, setIsTriggerFixed } = usePopover();
  useEffect(() => {
    setIsTriggerFixed(true);
  }, [setIsTriggerFixed]);

  const [isOpen, setIsOpen] = useState(shouldShowPopover);
  const [warnReboot, setWarnReboot] = useState(false);
  const [shouldReboot, setShouldReboot] = useState(
    miningStatus.reboot_uptime_s ? isWarmingUp(miningStatus) : false,
  );
  const [warnSleep, setWarnSleep] = useState(false);
  const [shouldSleep, setShouldSleep] = useState(false);
  const [pausedAction, setPausedAction] = useState<keyof typeof actions | null>(
    null,
  );
  const { dismissedLoginModal, setDismissedLoginModal } = useAuthContext();
  const { checkAccess, hasAccess, setHasAccess } = useAccessToken(
    !!pausedAction && !dismissedLoginModal,
  );

  const onClickOutside = useCallback(() => {
    setIsOpen(false);
  }, []);

  useClickOutside({ ref: WidgetRef, onClickOutside });

  useEffect(() => {
    if (hasAccess && pausedAction) {
      if (pausedAction === actions.reboot) {
        setWarnReboot(true);
      } else if (pausedAction === actions.sleep) {
        setWarnSleep(true);
      }
      setPausedAction(null);
    }
  }, [hasAccess, pausedAction]);

  useEffect(() => {
    if (dismissedLoginModal) {
      setPausedAction(null);
      setDismissedLoginModal(false);
    }
  }, [dismissedLoginModal, setDismissedLoginModal]);

  const handleRebootButton = () => {
    setIsOpen(false);
    setPausedAction(actions.reboot);
    checkAccess();
  };

  const handleRebootConfirm = () => {
    setWarnReboot(false);
    onReboot();
    setShouldReboot(true);
  };

  const handleSleepButton = () => {
    setIsOpen(false);
    setPausedAction(actions.sleep);
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
        setShouldReboot(false);
        if (rebootError?.status === 401) {
          setHasAccess(false);
          setPausedAction(actions.reboot);
        }
        // TODO: handle other errors
      } else if (isAwake(miningStatus.status)) {
        setShouldReboot(false);
        afterReboot?.();
      }
    }
  }, [shouldReboot, miningStatus, afterReboot, rebootError, setHasAccess]);

  useEffect(() => {
    if (shouldSleep) {
      if (sleepError) {
        setShouldSleep(false);
        if (sleepError?.status === 401) {
          setHasAccess(false);
          setPausedAction(actions.sleep);
        }
        // TODO: handle other errors
      } else if (isSleeping(miningStatus.status)) {
        setShouldSleep(false);
        afterSleep?.();
      }
    }
  }, [afterSleep, miningStatus, setHasAccess, shouldSleep, sleepError]);

  return (
    <div className="relative" ref={WidgetRef}>
      <WidgetWrapper
        onClick={() => setIsOpen((prev) => !prev)}
        className="w-[28px] p-0 text-text-primary"
        isOpen={isOpen}
        testId="power-button"
      >
        <Power width={iconSizes.small} className="m-1 -translate-y-0.25" />
      </WidgetWrapper>
      {isOpen && (
        <PowerPopover
          miningStatus={miningStatus}
          onReboot={handleRebootButton}
          onSleep={handleSleepButton}
          onWake={handleWakeButton}
        />
      )}
      <WarnRebootDialog
        onClose={() => setWarnReboot(false)}
        onSubmit={handleRebootConfirm}
        show={warnReboot}
      />
      <RebootingDialog show={shouldReboot} />
      <WarnSleepDialog
        onClose={() => setWarnSleep(false)}
        onSubmit={handleSleepConfirm}
        show={warnSleep}
      />
      <EnteringSleepDialog show={shouldSleep} />
    </div>
  );
};

export default PowerWidget;
