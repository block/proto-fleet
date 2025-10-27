import { useState } from "react";
import FansDetectedDialog from "./FansDetectedDialog";
import { useCoolingStatus } from "@/protoOS/api";

import { WakingDialog } from "@/protoOS/components/Power";
import { useWakeMiner } from "@/protoOS/hooks/useWakeMiner";
import { useIsSleeping } from "@/protoOS/store";
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
  const { data: coolingStatus, setCooling } = useCoolingStatus({ poll: false });
  const isSleeping = useIsSleeping();
  const [showFansDetectedDialog, setShowFansDetectedDialog] = useState(false);
  const [isUpdatingCooling, setIsUpdatingCooling] = useState(false);

  const handleWake = () => {
    // Check if fans are running and cooling mode is immersion
    const hasFansRunning = coolingStatus?.fans?.some(
      (fan) => fan && (fan.rpm ?? 0) > 0,
    );
    const isImmersionMode = coolingStatus?.fan_mode === "Off";

    if (hasFansRunning && isImmersionMode) {
      setShowFansDetectedDialog(true);
    } else {
      setShowFansDetectedDialog(false);
      wakeMiner();
    }
  };

  const handleConfirmImmersion = async () => {
    setIsUpdatingCooling(true);
    // Add synthetic delay to show loading state
    await new Promise((resolve) => setTimeout(resolve, 500));
    setIsUpdatingCooling(false);
    handleWake();
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
      {isSleeping && (
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
        onRetry={handleConfirmImmersion}
        onCancel={handleSwitchToAirCooled}
        isLoading={isUpdatingCooling}
        show={showFansDetectedDialog}
      />
    </>
  );
};

export default WakeCallout;
