import { useLayoutEffect } from "react";
import type { Meta, StoryObj } from "@storybook/react";
import { action } from "storybook/actions";

import ReturnHashboardsToDefaultDialog from "./ReturnHashboardsToDefaultDialog";
import { useFleetStore } from "@/protoFleet/store";

const meta = {
  title: "Proto Fleet/Containers/Return hashboards",
  component: ReturnHashboardsToDefaultDialog,
  args: { hashboardIds: [1n], onClose: action("close"), onComplete: action("complete") },
  decorators: [
    (Story, context) => {
      useLayoutEffect(() => {
        const permissions = useFleetStore.getState().auth.permissions;
        useFleetStore.getState().auth.setPermissions(["miner:update_pools", "miner:start_mining", "miner:stop_mining"]);
        const originalFetch = window.fetch;
        let reviews = 0;
        window.fetch = async (input, init) => {
          const url = input instanceof Request ? input.url : String(input);
          if (!url.includes("/mininggroups.v1.MiningGroupService/")) return originalFetch(input, init);
          const blocked = context.parameters.pending || (context.parameters.transition && reviews < 2);
          let response: unknown = { operationId: "story-return" };
          if (url.endsWith("/ReviewReturnToDefault")) {
            reviews++;
            response = {
              containers: Array.from({ length: context.parameters.containerCount ?? 1 }, (_, containerIndex) => ({
                deviceIdentifier: `ct1-demo-${containerIndex + 1}`,
                boards: Array.from({ length: context.parameters.boardCount ?? 1 }, (_, index) => ({
                  hashboardId: String(containerIndex * (context.parameters.boardCount ?? 1) + index + 1),
                  name: `Hashboard CT1-${String(index + 1).padStart(4, "0")}`,
                })),
                defaultGroup: {
                  id: "default",
                  mining: true,
                  pools: [{ id: 1, url: "stratum+tcp://pool.example:3333", username: "host.worker", enabled: true }],
                },
                reviewToken: blocked ? "" : "a".repeat(64),
                blockedReason: blocked
                  ? "Wait for pool changes and ownership cleanup to finish. The boards remain recalled."
                  : "",
              })),
            };
          } else if (url.endsWith("/GetState")) response = { operationId: "story-return", status: "complete" };
          return new Response(JSON.stringify(response), { headers: { "Content-Type": "application/json" } });
        };
        return () => {
          window.fetch = originalFetch;
          useFleetStore.getState().auth.setPermissions(permissions);
        };
      }, [
        context.parameters.pending,
        context.parameters.transition,
        context.parameters.boardCount,
        context.parameters.containerCount,
      ]);
      return <Story />;
    },
  ],
} satisfies Meta<typeof ReturnHashboardsToDefaultDialog>;
export default meta;
type Story = StoryObj<typeof meta>;

export const Preparing: Story = { parameters: { pending: true } };
export const ReadyToMove: Story = {};
export const ManyHashboards: Story = {
  args: { hashboardIds: Array.from({ length: 288 }, (_, index) => BigInt(index + 1)) },
  parameters: { boardCount: 288 },
};
export const ManyContainers: Story = {
  args: { hashboardIds: Array.from({ length: 1500 }, (_, index) => BigInt(index + 1)) },
  parameters: { boardCount: 250, containerCount: 6 },
};
export const RecallToConfirmation: Story = { parameters: { transition: true } };
