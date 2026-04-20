import { ElementType, useEffect } from "react";
import { MemoryRouter } from "react-router-dom";

import App from "./App";
import { useSetMiningStatus } from "@/protoOS/store";

export const WarmingUpMiner = () => {
  const setMiningStatus = useSetMiningStatus();

  useEffect(() => {
    setMiningStatus({ status: "PoweringOn" });

    setTimeout(() => {
      setMiningStatus({ status: "Mining" });
    }, 5000);
  }, [setMiningStatus]);

  return <App title="Page title">Page content</App>;
};

export default {
  title: "ProtoOS/Warming Up Miner",
  parameters: {
    withRouter: false,
  },
  decorators: [
    (Story: ElementType) => (
      <MemoryRouter>
        <Story />
      </MemoryRouter>
    ),
  ],
};
