import { ElementType, useEffect, useState } from "react";
import { MemoryRouter } from "react-router-dom";

import { MiningStatusMiningstatus } from "apiTypes";

import App from "./App";

export const WarmingUpMiner = () => {
  const [miningStatus, setMiningStatus] = useState<MiningStatusMiningstatus>({
    status: "PoweringOn",
  });

  useEffect(() => {
    setTimeout(() => {
      setMiningStatus({ status: "Mining" });
    }, 5000);
  }, []);

  return (
    <App
      title="Page title"
      apiMiningStatus={miningStatus}
      onWake={() => {}}
      pendingSystemInfo={false}
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
