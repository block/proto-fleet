import { ElementType, useEffect } from "react";
import { MemoryRouter } from "react-router-dom";

import App from "./App";
import { SystemContextProvider } from "@/protoOS/contexts/SystemContext";
import { useSetMiningStatus } from "@/protoOS/store";

export const WakeUpMiner = () => {
  const setMiningStatus = useSetMiningStatus();

  useEffect(() => {
    setMiningStatus({ status: "Stopped" });
  }, [setMiningStatus]);

  const handleWake = () => {
    setTimeout(() => {
      setMiningStatus({ status: "Mining" });
    }, 2000);
  };

  return (
    <SystemContextProvider poll={false}>
      <App title="Page title" onWake={handleWake} pendingSystemInfo={false}>
        Page content
      </App>
    </SystemContextProvider>
  );
};

export default {
  title: "ProtoOS/Wake Up Miner",
  decorators: [
    (Story: ElementType) => (
      <MemoryRouter>
        <Story />
      </MemoryRouter>
    ),
  ],
};
