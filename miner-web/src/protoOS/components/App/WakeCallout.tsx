import { useCallback, useEffect, useState } from "react";

import { isSleeping } from "./utility";
import { ErrorProps } from "@/protoOS/api/apiResponseTypes";
import { MiningStatusMiningstatus } from "@/protoOS/api/types";

import { WakingDialog, WarnWakeDialog } from "@/protoOS/components/Power";
import { useAccessToken, useAuthContext } from "@/protoOS/contexts/AuthContext";
import { Power } from "@/shared/assets/icons";
import Callout, { intents } from "@/shared/components/Callout";

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
    pausedAction && !dismissedLoginModal,
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
