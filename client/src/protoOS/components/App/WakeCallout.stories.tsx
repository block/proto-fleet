import { ElementType, useEffect } from "react";
import { MemoryRouter } from "react-router-dom";

import App from "./App";
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
    <App title="Page title" onWake={handleWake}>
      Page content
    </App>
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
