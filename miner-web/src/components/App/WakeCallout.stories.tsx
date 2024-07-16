import { ElementType, useState } from "react";
import { MemoryRouter } from "react-router-dom";

import { MiningStatusMiningstatus } from "apiTypes";

import App from "./App";

export const WakeUpMiner = () => {
  const [miningStatus, setMiningStatus] = useState<MiningStatusMiningstatus>({ status: "Stopped"});

  const handleWake = () => {
    setTimeout(() => {
      setMiningStatus({ status: "Running" });
    }, 2000);
  };

  return (
    <App
      title="Page title"
      apiMiningStatus={miningStatus}
      onWake={handleWake}
    >
      Page content
    </App>
  );
};

export default {
  title: "Pages/App/Wake Up Miner",
  decorators: [
    (Story: ElementType) => (
      <MemoryRouter>
        <Story />
      </MemoryRouter>
    ),
  ],
};
