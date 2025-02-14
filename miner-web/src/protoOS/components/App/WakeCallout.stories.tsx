import { ElementType, useState } from "react";
import { MemoryRouter } from "react-router-dom";

import App from "./App";
import { MiningStatusMiningstatus } from "@/protoOS/api/types";


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
    <App
      title="Page title"
      apiMiningStatus={miningStatus}
      onWake={handleWake}
      pendingSystemInfo={false}
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
