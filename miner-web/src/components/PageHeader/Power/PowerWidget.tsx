import { RefObject, useCallback, useEffect, useRef, useState } from "react";

import { ErrorProps } from "apiResponseTypes";
import { MiningStatusMiningstatus } from "apiTypes";

import { useAccessToken } from "common/hooks/useAccessToken";
import { useAuthContext } from "common/hooks/useAuthContext";
import { useClickOutside } from "common/hooks/useClickOutside";

import { isAwake, isSleeping, isWarmingUp } from "components/App/utility";
import {
  EnteringSleepDialog,
  ExportingLogsDialog,
  RebootingDialog,
  WakingDialog,
  WarnRebootDialog,
  WarnSleepDialog,
  WarnWakeDialog,
} from "components/Power";

import { Power } from "icons";
import { iconSizes } from "icons/constants";

import WidgetWrapper from "../WidgetWrapper";
import { actions } from "./constants";
import PowerPopover from "./PowerPopover";

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
  const WidgetRef = useRef<HTMLDivElement>(null);
  const [isOpen, setIsOpen] = useState(shouldShowPopover);
  const [warnReboot, setWarnReboot] = useState(false);
  const [shouldReboot, setShouldReboot] = useState(
    miningStatus.reboot_uptime_s ? isWarmingUp(miningStatus) : false
  );
  const [shouldExportLogs, setShouldExportLogs] = useState(false);
  const [warnSleep, setWarnSleep] = useState(false);
  const [shouldSleep, setShouldSleep] = useState(false);
  const [warnWake, setWarnWake] = useState(false);
  const [shouldWake, setShouldWake] = useState(false);
  const [pausedAction, setPausedAction] = useState<keyof typeof actions | null>(
    null
  );
  const { dismissedLoginModal, setDismissedLoginModal } = useAuthContext();
  const { checkAccess, hasAccess, setHasAccess } = useAccessToken(
    !!pausedAction && !dismissedLoginModal
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
    if (shouldWake && wakeError?.status === 401) {
      setHasAccess(false);
      setShouldWake(false);
      setPausedAction(actions.wake);
    }
    if (shouldSleep && sleepError?.status === 401) {
      setHasAccess(false);
      setShouldSleep(false);
      setPausedAction(actions.sleep);
    }
    if (shouldReboot && rebootError?.status === 401) {
      setHasAccess(false);
      setShouldReboot(false);
      setPausedAction(actions.reboot);
    }
  }, [
    shouldWake,
    wakeError,
    shouldSleep,
    sleepError,
    shouldReboot,
    rebootError,
    setHasAccess,
  ]);

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
    if (shouldReboot && isAwake(miningStatus.status)) {
      setShouldReboot(false);
      afterReboot?.();
    }
    if (shouldSleep && isSleeping(miningStatus.status)) {
      setShouldSleep(false);
      afterSleep?.();
    }
    if (shouldWake && isAwake(miningStatus.status)) {
      setShouldWake(false);
      afterWake?.();
    }
  }, [
    shouldReboot,
    shouldSleep,
    shouldWake,
    miningStatus,
    afterReboot,
    afterSleep,
    afterWake,
  ]);

  return (
    <div className="relative" ref={WidgetRef}>
      <WidgetWrapper
        onClick={() => setIsOpen((prev) => !prev)}
        className="text-text-primary/90"
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
