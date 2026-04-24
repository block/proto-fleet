import { useState } from "react";
import FansDetectedDialog from "./FansDetectedDialog";
import { useCoolingStatus } from "@/protoOS/api/hooks/useCoolingStatus";

import { WakingDialog } from "@/protoOS/components/Power";
import { useWakeMiner } from "@/protoOS/hooks/useWakeMiner";
import { useCoolingMode, useFansTelemetry, useIsSleeping } from "@/protoOS/store";
import { areFansDetectedInImmersionMode } from "@/protoOS/store/utils/coolingUtils";
import { Power } from "@/shared/assets/icons";
import Callout, { intents } from "@/shared/components/Callout";

interface WakeCalloutProps {
  afterWake?: () => void;
  onWake?: () => void;
}

const WakeCallout = ({ afterWake, onWake }: WakeCalloutProps) => {
  const { wakeMiner, shouldWake } = useWakeMiner({
    afterWake,
    onSuccess: onWake,
  });
  const [isUpdatingCooling, setIsUpdatingCooling] = useState(false);
  const { setCooling } = useCoolingStatus();
  const coolingMode = useCoolingMode();
  const fans = useFansTelemetry();
  const isSleeping = useIsSleeping();
  const [showFansDetectedDialog, setShowFansDetectedDialog] = useState(false);

  // Show dialog on wake completion (isSleeping true→false) when fans are detected in immersion mode
  const [prevIsSleeping, setPrevIsSleeping] = useState(isSleeping);
  if (prevIsSleeping !== isSleeping) {
    setPrevIsSleeping(isSleeping);
    if (prevIsSleeping && !isSleeping && areFansDetectedInImmersionMode(fans, coolingMode)) {
      setShowFansDetectedDialog(true);
    }
  }

  const handleContinue = () => {
    setShowFansDetectedDialog(false);
  };

  const handleSwitchToAirCooled = () => {
    setIsUpdatingCooling(true);
    setCooling({
      mode: "Auto",
      onSuccess: () => {
        setIsUpdatingCooling(false);
        setShowFansDetectedDialog(false);
      },
      onError: () => {
        setIsUpdatingCooling(false);
      },
    });
  };

  return (
    <>
      {isSleeping && (
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
      <WakingDialog open={shouldWake} />

      <FansDetectedDialog
        open={showFansDetectedDialog}
        onContinue={handleContinue}
        onSwitchToAirCooled={handleSwitchToAirCooled}
        isLoading={isUpdatingCooling}
      />
    </>
  );
};

export default WakeCallout;
