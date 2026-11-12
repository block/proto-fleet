import { useLayoutEffect } from "react";
import type { Meta, StoryObj } from "@storybook/react-vite";

import { WorkerNameDialog } from "./WorkerNameDialog";
import { useFleetStore } from "@/protoFleet/store";

const meta = {
  title: "Proto Fleet/Pool Configurations/Worker names",
  component: WorkerNameDialog,
  args: { hashboardIds: [17n, 18n], onDismiss: () => undefined },
  decorators: [
    (Story) => {
      useLayoutEffect(() => {
        const previous = useFleetStore.getState().auth.permissions;
        useFleetStore
          .getState()
          .auth.setPermissions([
            "miner:read",
            "miner:update_worker_names",
            "pool:read",
            "miner:update_pools",
            "miner:start_mining",
            "miner:stop_mining",
          ]);
        const previousFetch = window.fetch;
        window.fetch = async (input, init) => {
          const url = input instanceof Request ? input.url : String(input);
          if (url.endsWith("/VerifyCredentials"))
            return new Response("{}", { headers: { "Content-Type": "application/json" } });
          if (url.endsWith("/ReviewWorkerNames")) {
            const body = await new Request(input, init).json();
            const changes = [17, 18].map((id, i) => ({
              deviceIdentifier: "CT1",
              hashboardId: String(id),
              displayName: `Module 1 · Hashboard ${i + 1}`,
              before: "",
              after:
                body.workerName?.mode === "WORKER_NAME_MODE_CUSTOM"
                  ? body.workerName.name
                  : body.workerName?.mode === "WORKER_NAME_MODE_MAC_ADDRESS"
                    ? `02:00:00:00:00:0${i + 1}`
                    : "",
            }));
            return new Response(
              JSON.stringify({
                reviews: [
                  {
                    reviewId: "story-review",
                    configurationId: "story-config",
                    configurationName: "Production",
                    changes,
                  },
                ],
              }),
              { headers: { "Content-Type": "application/json" } },
            );
          }
          return previousFetch(input, init);
        };
        return () => {
          window.fetch = previousFetch;
          useFleetStore.getState().auth.setPermissions(previous);
        };
      }, []);
      return <Story />;
    },
  ],
} satisfies Meta<typeof WorkerNameDialog>;
export default meta;
type Story = StoryObj<typeof meta>;
export const Hashboards: Story = {};

export const MACAddress: Story = {
  args: {
    hashboardIds: [17n],
    hashboards: [{ hashboardId: 17n, workerName: "02:00:00:00:00:01", workerNameMode: "mac_address" }],
  },
};
export const Custom: Story = {
  args: { hashboardIds: [17n], hashboards: [{ hashboardId: 17n, workerName: "rack-one", workerNameMode: "custom" }] },
};
export const NoWorkerName: Story = {
  args: { hashboardIds: [17n], hashboards: [{ hashboardId: 17n, workerName: "", workerNameMode: "none" }] },
};
export const Module: Story = { args: { moduleIds: [9n], hashboardIds: undefined } };
export const Mixed: Story = {
  args: {
    hashboards: [
      { hashboardId: 17n, workerName: "rack-one", workerNameMode: "custom" },
      { hashboardId: 18n, workerName: "rack-two", workerNameMode: "custom" },
    ],
  },
};
