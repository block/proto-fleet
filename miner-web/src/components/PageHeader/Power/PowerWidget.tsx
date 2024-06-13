import { useCallback, useEffect, useRef, useState } from "react";

import { MiningStatusMiningstatus } from "apiTypes";

import { useClickOutside } from "common/hooks/useClickOutside";

import {
  EnteringSleepDialog,
  RebootingDialog,
  WakingDialog,
  WarnRebootDialog,
  WarnSleepDialog,
  WarnWakeDialog,
} from "components/Power";

import { Power } from "icons";
import { iconSizes } from "icons/constants";

import WidgetWrapper from "../WidgetWrapper";
import PowerPopover from "./PowerPopover";

interface PowerWidgetProps {
  afterReboot?: () => void;
  afterSleep?: () => void;
  afterWake?: () => void;
  miningStatus: MiningStatusMiningstatus;
  onReboot: () => void;
  onSleep: () => void;
  onWake: () => void;
  shouldShowPopover?: boolean;
}

const PowerWidget = ({
  afterReboot,
  afterSleep,
  afterWake,
  miningStatus,
  onReboot,
  onSleep,
  onWake,
  shouldShowPopover,
}: PowerWidgetProps) => {
  const WidgetRef = useRef<HTMLDivElement>(null);
  const [isOpen, setIsOpen] = useState(shouldShowPopover);
  const [warnReboot, setWarnReboot] = useState(false);
  const [shouldReboot, setShouldReboot] = useState(false);
  const [warnSleep, setWarnSleep] = useState(false);
  const [shouldSleep, setShouldSleep] = useState(false);
  const [warnWake, setWarnWake] = useState(false);
  const [shouldWake, setShouldWake] = useState(false);

  const onClickOutside = useCallback(() => {
    setIsOpen(false);
  }, []);

  useClickOutside({ ref: WidgetRef, onClickOutside });

  const handleRebootButton = () => {
    setIsOpen(false);
    setWarnReboot(true);
  };

  const handleRebootConfirm = () => {
    onReboot();
    setWarnReboot(false);
    setShouldReboot(true);
  };

  const handleSleepButton = () => {
    setIsOpen(false);
    setWarnSleep(true);
  };

  const handleSleepConfirm = () => {
    onSleep();
    setWarnSleep(false);
    setShouldSleep(true);
  };

  const handleWakeButton = () => {
    setIsOpen(false);
    setWarnWake(true);
  };

  const handleWakeConfirm = () => {
    onWake();
    setWarnWake(false);
    setShouldWake(true);
  };

  useEffect(() => {
    if (shouldReboot && miningStatus.status === "Running") {
      setShouldReboot(false);
      afterReboot?.();
    }
    if (shouldSleep && miningStatus.status === "Stopped") {
      setShouldSleep(false);
      afterSleep?.();
    }
    if (shouldWake && miningStatus.status === "Running") {
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
    <div className="relative" ref={WidgetRef} data-testid="power-widget">
      <WidgetWrapper
        onClick={() => setIsOpen((prev) => !prev)}
        className="text-text-primary/90"
        isOpen={isOpen}
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
