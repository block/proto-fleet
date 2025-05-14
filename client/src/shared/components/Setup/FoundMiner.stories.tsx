import { action } from "@storybook/addon-actions";
import FoundMinerComnponent from "./FoundMiner";

export const FoundMiner = () => {
  const miner = {
    macAddress: "0d:04:8a:54:fa:00",
    serialNumber: "0123456789",
  };

  return (
    <div>
      <FoundMinerComnponent
        miner={miner}
        handleContinueSetup={action("continue setup")}
      />
    </div>
  );
};

export default {
  title: "Components (Shared)/Setup/Found Miner",
};
