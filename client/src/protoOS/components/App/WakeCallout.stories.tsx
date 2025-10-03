import { ElementType, useState } from "react";
import { MemoryRouter } from "react-router-dom";

import App from "./App";
import { MiningStatusMiningstatus } from "@/protoOS/api/generatedApi";
import { MinerStatusProvider } from "@/protoOS/contexts/MinerStatusContext";
import { SystemContextProvider } from "@/protoOS/contexts/SystemContext";

export const WakeUpMiner = () => {
  const [miningStatus, setMiningStatus] = useState<MiningStatusMiningstatus>({
    status: "Stopped",
  });

  const handleWake = () => {
    setTimeout(() => {
      setMiningStatus({ status: "Mining" });
    }, 2000);
  };

  return (
    <SystemContextProvider poll={false}>
      <MinerStatusProvider apiMiningStatus={miningStatus}>
        <App title="Page title" onWake={handleWake} pendingSystemInfo={false}>
          Page content
        </App>
      </MinerStatusProvider>
    </SystemContextProvider>
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
