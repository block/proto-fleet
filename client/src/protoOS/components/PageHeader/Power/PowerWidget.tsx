import { RefObject, useCallback, useEffect, useState } from "react";

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
  ExportingLogsDialog,
  RebootingDialog,
  WakingDialog,
  WarnRebootDialog,
  WarnSleepDialog,
  WarnWakeDialog,
} from "@/protoOS/components/Power";
import { useAccessToken, useAuthContext } from "@/protoOS/contexts/AuthContext";
import { Power } from "@/shared/assets/icons";
import { iconSizes } from "@/shared/assets/icons/constants";

import { usePopover } from "@/shared/components/Popover";
import { useClickOutside } from "@/shared/hooks/useClickOutside";

interface PowerWidgetProps {
  afterReboot?: () => void;
  afterSleep?: () => void;
  afterWake?: () => void;
  linkRef?: RefObject<HTMLAnchorElement>;
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
  afterWake,
  linkRef,
  miningStatus,
  onReboot,
  onSleep,
  onWake,
  rebootError,
  sleepError,
  wakeError,
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
  const [shouldExportLogs, setShouldExportLogs] = useState(false);
  const [warnSleep, setWarnSleep] = useState(false);
  const [shouldSleep, setShouldSleep] = useState(false);
  const [warnWake, setWarnWake] = useState(false);
  const [shouldWake, setShouldWake] = useState(false);
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
      } else if (pausedAction === actions.wake) {
        setWarnWake(true);
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

  const handleRebootConfirm = async () => {
    setWarnReboot(false);
    setShouldExportLogs(true);
    await onReboot();
    setShouldExportLogs(false);
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
    setPausedAction(actions.wake);
    checkAccess();
  };

  const handleWakeConfirm = () => {
    onWake();
    setWarnWake(false);
    setShouldWake(true);
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

  useEffect(() => {
    if (shouldWake) {
      if (wakeError) {
        setShouldWake(false);
        if (wakeError?.status === 401) {
          setHasAccess(false);
          setPausedAction(actions.wake);
        }
        // TODO: handle other errors
      } else if (isAwake(miningStatus.status)) {
        setShouldWake(false);
        afterWake?.();
      }
    }
  }, [afterWake, miningStatus.status, setHasAccess, shouldWake, wakeError]);

  return (
    <div className="relative" ref={WidgetRef}>
      <WidgetWrapper
        onClick={() => setIsOpen((prev) => !prev)}
        className="text-text-primary"
        isOpen={isOpen}
        testId="power-button"
      >
        <>
          <Power className="mr-1" width={iconSizes.xSmall} />
          Power
        </>
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
      <ExportingLogsDialog show={shouldExportLogs} linkRef={linkRef} />
      <RebootingDialog show={shouldReboot} />
      <WarnSleepDialog
        onClose={() => setWarnSleep(false)}
        onSubmit={handleSleepConfirm}
        show={warnSleep}
      />
      <EnteringSleepDialog show={shouldSleep} />
      <WarnWakeDialog
        onClose={() => setWarnWake(false)}
        onSubmit={handleWakeConfirm}
        show={warnWake}
      />
      <WakingDialog show={shouldWake} />
    </div>
  );
};

export default PowerWidget;
