import { isSleeping } from "./utility";
import { MiningStatusMiningstatus } from "@/protoOS/api/types";

import { WakingDialog } from "@/protoOS/components/Power";
import { useWakeMiner } from "@/protoOS/hooks/useWakeMiner";
import { Power } from "@/shared/assets/icons";
import Callout, { intents } from "@/shared/components/Callout";

interface WakeCalloutProps {
  afterWake?: () => void;
  miningStatus?: MiningStatusMiningstatus;
  onWake?: () => void;
}

const WakeCallout = ({ afterWake, miningStatus, onWake }: WakeCalloutProps) => {
  const { wakeMiner, shouldWake } = useWakeMiner({
    afterWake,
    miningStatus,
    onSuccess: onWake,
  });

  return (
    <>
      {isSleeping(miningStatus?.status) && (
        <div className="mb-10">
          <Callout
            buttonOnClick={wakeMiner}
            buttonText="Wake up miner"
            intent={intents.information}
            prefixIcon={<Power />}
            title="This miner is asleep and is not hashing."
          />
        </div>
      )}
      <WakingDialog show={shouldWake} />
    </>
  );
};

export default WakeCallout;
