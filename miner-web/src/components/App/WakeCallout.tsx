import { useCallback, useEffect, useState } from "react";

import { ErrorProps } from "apiResponseTypes";
import { MiningStatusMiningstatus } from "apiTypes";

import { useAccessToken } from "common/hooks/useAccessToken";
import { useAuthContext } from "common/hooks/useAuthContext";

import Callout, { intents } from "components/Callout";
import { WakingDialog, WarnWakeDialog } from "components/Power";

import { Power } from "icons";

import { isSleeping } from "./utility";

interface WakeCalloutProps {
  afterWake?: () => void;
  miningStatus?: MiningStatusMiningstatus;
  onWake?: () => void;
  wakeError?: ErrorProps;
}

const WakeCallout = ({
  afterWake,
  miningStatus,
  onWake,
  wakeError,
}: WakeCalloutProps) => {
  const [warnWake, setWarnWake] = useState(false);
  const [shouldWake, setShouldWake] = useState(false);
  const [pausedAction, setPausedAction] = useState(false);
  const { dismissedLoginModal, setDismissedLoginModal } = useAuthContext();
  const { checkAccess, hasAccess, setHasAccess } = useAccessToken(
    pausedAction && !dismissedLoginModal
  );

  useEffect(() => {
    if (hasAccess && pausedAction) {
      setPausedAction(false);
      setWarnWake(true);
    }
  }, [hasAccess, pausedAction]);

  useEffect(() => {
    if (shouldWake && wakeError?.status === 401) {
      setHasAccess(false);
      setShouldWake(false);
      setPausedAction(true);
    }
  }, [shouldWake, wakeError, setHasAccess]);

  useEffect(() => {
    if (dismissedLoginModal) {
      setPausedAction(false);
      setDismissedLoginModal(false);
    }
  }, [dismissedLoginModal, setDismissedLoginModal]);

  const handleWakeConfirm = useCallback(() => {
    setWarnWake(false);
    onWake?.();
    setShouldWake(true);
  }, [onWake]);

  useEffect(() => {
    if (!isSleeping(miningStatus?.status)) {
      setShouldWake(false);
      afterWake?.();
    }
  }, [miningStatus, afterWake]);

  const handleWakeClick = useCallback(() => {
    setPausedAction(true);
    checkAccess();
  }, [checkAccess]);

  return (
    <>
      {isSleeping(miningStatus?.status) && (
        <div className="mb-10">
          <Callout
            buttonOnClick={handleWakeClick}
            buttonText="Wake up miner"
            intent={intents.information}
            prefixIcon={<Power />}
            title="This miner is asleep and is not hashing."
          />
        </div>
      )}
      <WarnWakeDialog
        onClose={() => setWarnWake(false)}
        onSubmit={handleWakeConfirm}
        show={warnWake}
      />
      <WakingDialog show={shouldWake} />
    </>
  );
};

export default WakeCallout;
