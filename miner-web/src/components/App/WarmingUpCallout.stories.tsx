import { ElementType, useEffect, useState } from "react";
import { MemoryRouter } from "react-router-dom";

import { MiningStatusMiningstatus } from "apiTypes";

import App from "./App";

export const WarmingUpMiner = () => {
  const [miningStatus, setMiningStatus] = useState<MiningStatusMiningstatus>({ status: "Stopped"});

  useEffect(() => {
    setTimeout(() => {
      setMiningStatus({ status: "Running" });
    }, 5000);
  }, []);

  return (
    <App
      title="Page title"
      apiMiningStatus={miningStatus}
      isOnboarding={miningStatus.status === "Stopped"}
      onWake={() => {}}
    >
      Page content
    </App>
  );
};

export default {
  title: "Pages/App/Warming Up Miner",
  decorators: [
    (Story: ElementType) => (
      <MemoryRouter>
        <Story />
      </MemoryRouter>
    ),
  ],
};
