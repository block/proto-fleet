import { ElementType, useEffect, useState } from "react";
import { MemoryRouter } from "react-router-dom";

import App from "./App";
import { MiningStatusMiningstatus } from "@/protoOS/api/types";
import { MinerStatusProvider } from "@/protoOS/contexts/MinerStatusContext";

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
    <MinerStatusProvider apiMiningStatus={miningStatus}>
      <App title="Page title" onWake={() => {}} pendingSystemInfo={false}>
        Page content
      </App>
    </MinerStatusProvider>
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
