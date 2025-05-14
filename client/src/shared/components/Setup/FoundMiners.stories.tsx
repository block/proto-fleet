import { useState } from "react";
import { action } from "@storybook/addon-actions";
import FoundMinersComnponent from "./FoundMiners";

type FoundMinerProps = {
  minersCount: number;
};

export const FoundMiner = ({ minersCount }: FoundMinerProps) => {
  const [miners] = useState([
    ...Array.from({ length: 1000 }, (_, i) => ({
      macAddress: `0d:04:8a:54:fa:${(i + 10).toString(16).padStart(2, "0")}`,
      deviceIdentifier: `5440...88${(i + 10).toString().padStart(2, "0")}`,
      model: `Miner Model`,
      selected: true,
    })),
  ]);

  return (
    <div>
      <FoundMinersComnponent
        miners={miners.slice(0, minersCount)}
        handleContinueSetup={action("continue setup")}
        handleRestartSearch={action("restart search")}
      />
    </div>
  );
};

export default {
  title: "Components (Shared)/Setup/Found Miners",
  args: {
    minersCount: 1,
  },
  argTypes: {
    minersCount: {
      control: {
        type: "range",
        min: 1,
        max: 1000,
        step: 1,
      },
    },
  },
};
