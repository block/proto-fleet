import { useCallback, useEffect, useState } from "react";

import { MiningStatusMiningstatus } from "apiTypes";

import Callout, { intents } from "components/Callout";
import { WakingDialog, WarnWakeDialog } from "components/Power";

import { Power } from "icons";

import { isSleeping } from "./utility";

interface WakeCalloutProps {
  afterWake?: () => void;
  miningStatus?: MiningStatusMiningstatus;
  onWake: () => void;
}

const WakeCallout = ({ afterWake, miningStatus, onWake }: WakeCalloutProps) => {
  const [warnWake, setWarnWake] = useState(false);
  const [shouldWake, setShouldWake] = useState(false);

  const handleWakeConfirm = useCallback(() => {
    setWarnWake(false);
    onWake();
    setShouldWake(true);
  }, [onWake]);

  useEffect(() => {
    if (!isSleeping(miningStatus?.status)) {
      setShouldWake(false);
      afterWake?.();
    }
  }, [miningStatus, afterWake]);

  return (
    <>
      {isSleeping(miningStatus?.status) && (
        <div className="mb-10">
          <Callout
            buttonOnClick={() => setWarnWake(true)}
            buttonText="Wake up miner"
            intent={intents.information}
            prefixIcon={<Power />}
            subtitle="This miner is asleep and is not hashing."
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
