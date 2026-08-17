import { ElementType, useEffect } from "react";
import { MemoryRouter } from "react-router-dom";

import App from "./App";
import { useSetMiningStatus } from "@/protoOS/store";
import { ROOT_MODULE_ID } from "@/protoOS/store/contract";

export const WarmingUpMiner = () => {
  const setMiningStatus = useSetMiningStatus();

  useEffect(() => {
    setMiningStatus({
      owner: { kind: "module", id: ROOT_MODULE_ID },
      resource: { status: "ready", updatedAtMs: Date.now() },
      miningStatus: "poweringOn",
    });

    setTimeout(() => {
      setMiningStatus({
        owner: { kind: "module", id: ROOT_MODULE_ID },
        resource: { status: "ready", updatedAtMs: Date.now() },
        miningStatus: "mining",
      });
    }, 5000);
  }, [setMiningStatus]);

  return <App title="Page title">Page content</App>;
};

export default {
  title: "Proto OS/Warming Up Miner",
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
