import { ElementType, useEffect } from "react";
import { MemoryRouter } from "react-router-dom";

import App from "./App";
import { useSetMiningStatus } from "@/protoOS/store";

export const WakeUpMiner = () => {
  const setMiningStatus = useSetMiningStatus();

  useEffect(() => {
    setMiningStatus({ status: "Stopped" });
  }, [setMiningStatus]);

  return <App title="Page title">Page content</App>;
};

export default {
  title: "ProtoOS/Wake Up Miner",
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
