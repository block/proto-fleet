import { useState } from "react";
import { action } from "@storybook/addon-actions";
import { FoundMiners as FoundMinersComponent } from ".";

type FoundMinersProps = {
  minersCount: number;
};

export const FoundMiners = ({ minersCount }: FoundMinersProps) => {
  const [miners] = useState([
    ...Array.from({ length: 1000 }, (_, i) => ({
      macAddress: `0d:04:8a:54:fa:${(i + 10).toString(16).padStart(2, "0")}`,
      serialNumber: `5440...88${(i + 10).toString().padStart(2, "0")}`,
    })),
  ]);

  return (
    <div>
      <FoundMinersComponent
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
