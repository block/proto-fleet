import { ElementType, useEffect, useState } from "react";
import { MemoryRouter } from "react-router-dom";

import App from "./App";
import { MiningStatusMiningstatus } from "@/protoOS/api/types";

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
