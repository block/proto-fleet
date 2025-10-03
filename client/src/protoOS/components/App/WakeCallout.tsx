import { useState } from "react";
import FansDetectedDialog from "./FansDetectedDialog";
import { isSleeping } from "./utility";
import { useCoolingStatus } from "@/protoOS/api";
import { MiningStatusMiningstatus } from "@/protoOS/api/generatedApi";

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
  const { data: coolingStatus, setCooling } = useCoolingStatus({ poll: false });
  const [showFansDetectedDialog, setShowFansDetectedDialog] = useState(false);
  const [isUpdatingCooling, setIsUpdatingCooling] = useState(false);

  const handleWake = () => {
    // Check if fans are running and cooling mode is immersion
    const hasFansRunning = coolingStatus?.fans?.some(
      (fan) => (fan.rpm ?? 0) > 0,
    );
    const isImmersionMode = coolingStatus?.fan_mode === "Off";

    if (hasFansRunning && isImmersionMode) {
      setShowFansDetectedDialog(true);
    } else {
      wakeMiner();
    }
  };

  const handleConfirmImmersion = () => {
    setShowFansDetectedDialog(false);
    wakeMiner();
  };

  const handleSwitchToAirCooled = () => {
    setIsUpdatingCooling(true);
    setCooling({
      mode: "Auto",
      onSuccess: () => {
        setIsUpdatingCooling(false);
        setShowFansDetectedDialog(false);
        wakeMiner();
      },
      onError: () => {
        setIsUpdatingCooling(false);
      },
    });
  };

  return (
    <>
      {isSleeping(miningStatus?.status) && (
        <div className="mb-10">
          <Callout
            buttonOnClick={handleWake}
            buttonText="Wake up miner"
            intent={intents.information}
            prefixIcon={<Power />}
            title="This miner is asleep and is not hashing."
          />
        </div>
      )}
      <WakingDialog show={shouldWake} />

      <FansDetectedDialog
        onConfirmImmersion={handleConfirmImmersion}
        onSwitchToAirCooled={handleSwitchToAirCooled}
        isLoading={isUpdatingCooling}
        show={showFansDetectedDialog}
      />
    </>
  );
};

export default WakeCallout;
