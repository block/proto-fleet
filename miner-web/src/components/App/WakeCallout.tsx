import { useCallback, useEffect, useState } from "react";

import { MiningStatusMiningstatus } from "apiTypes";

import Callout, { intents } from "components/Callout";
import { WakingDialog, WarnWakeDialog } from "components/Power";

import { Power } from "icons";

interface AppProps {
  afterWake?: () => void;
  miningStatus?: MiningStatusMiningstatus;
  onWake: () => void;
}

const App = ({
  afterWake,
  miningStatus,
  onWake,
}: AppProps) => {
  const [warnWake, setWarnWake] = useState(false);
  const [shouldWake, setShouldWake] = useState(false);

  const handleWakeConfirm = useCallback(() => {
    setWarnWake(false);
    onWake();
    setShouldWake(true);
  }, [onWake]);

  useEffect(() => {
    if (miningStatus?.status === "Running") {
      setShouldWake(false);
      afterWake?.();
    }
  }, [miningStatus, afterWake]);

  return (
    <>
      {miningStatus?.status === "Stopped" && (
        <div className="mb-10">
          <Callout
            buttonOnClick={() => setWarnWake(true)}
            buttonText="Wake miner"
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

export default App;
